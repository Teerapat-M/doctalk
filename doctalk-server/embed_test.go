package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestL2NormalizeAndVectorLiteral(t *testing.T) {
	v := []float32{3, 4, 0}
	l2normalize(v)
	if v[0] != 0.6 || v[1] != 0.8 {
		t.Fatalf("got %v", v)
	}
	lit := vectorLiteral([]float32{1, 2})
	if lit != "[1,2]" {
		t.Fatalf("literal = %q", lit)
	}
}

func TestGeminiEmbedBatchAnd429Retry(t *testing.T) {
	const secret = "test-gemini-key-not-real"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.RawQuery != "" {
			t.Errorf("API key must not appear in the URL, got %q", r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != secret {
			t.Errorf("missing API key header")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), secret) {
			t.Errorf("API key leaked into body")
		}
		if hits.Load() == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var req embedRequestBody
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("json: %v", err)
		}
		n := len(req.Requests)
		emb := make([]any, n)
		for i := 0; i < n; i++ {
			vals := make([]float64, EmbeddingDim)
			vals[0] = 1
			emb[i] = map[string]any{"values": vals}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": emb})
	}))
	defer srv.Close()

	g := &geminiEmbedder{
		apiKey:     secret,
		model:      defaultEmbedModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		batchSize:  2,
		maxRetries: 4,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	texts := []string{"a", "b", "c"}
	vecs, err := g.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 3 {
		t.Fatalf("len=%d", len(vecs))
	}
	if len(vecs[0]) != EmbeddingDim {
		t.Fatalf("dim=%d", len(vecs[0]))
	}
	// 429 on first batch, then success; second batch once. At least 3 calls.
	if hits.Load() < 3 {
		t.Fatalf("hits=%d, want batched + retry", hits.Load())
	}
}

func TestGeminiEmbedGivesUpOn429AndRedactsKey(t *testing.T) {
	const secret = "super-secret-key-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"` + secret + `"}`))
	}))
	defer srv.Close()

	g := &geminiEmbedder{
		apiKey:     secret,
		model:      defaultEmbedModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		batchSize:  8,
		maxRetries: 2,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	_, err := g.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := redactErr(err, secret)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("raw error leaked key: %v", err)
	}
	if strings.Contains(msg, secret) {
		t.Fatalf("redacted error leaked key: %s", msg)
	}
}

func TestRedactErr(t *testing.T) {
	err := errString("boom secret-token boom")
	got := redactErr(err, "secret-token")
	if strings.Contains(got, "secret-token") {
		t.Fatalf("got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
