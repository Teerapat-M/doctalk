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
	parsed := parseVectorLiteral("[1,2]")
	if len(parsed) != 2 || parsed[0] != 1 || parsed[1] != 2 {
		t.Fatalf("parse %v", parsed)
	}
	if parseVectorLiteral("") != nil {
		t.Fatal("empty literal")
	}
}

func TestGeminiEmbedQueryUsesRetrievalQuery(t *testing.T) {
	const secret = "query-embed-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("API key must not appear in the URL, got %q", r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != secret {
			t.Errorf("missing API key header")
		}
		body, _ := io.ReadAll(r.Body)
		var req embedRequestBody
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("json: %v", err)
		}
		if len(req.Requests) != 1 || req.Requests[0].TaskType != "RETRIEVAL_QUERY" {
			t.Errorf("taskType=%v", req.Requests)
		}
		if req.Requests[0].OutputDimensionality != EmbeddingDim {
			t.Errorf("dims=%d", req.Requests[0].OutputDimensionality)
		}
		vals := make([]float64, EmbeddingDim)
		vals[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": []any{map[string]any{"values": vals}}})
	}))
	defer srv.Close()

	g := &geminiEmbedder{
		apiKey:     secret,
		model:      defaultEmbedModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		batchSize:  8,
		maxRetries: 1,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	vec, err := g.EmbedQuery(context.Background(), "What is pgvector?")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != EmbeddingDim {
		t.Fatalf("dim=%d", len(vec))
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

func TestRedactSecretsStripsKeyAndQuery(t *testing.T) {
	const key = "sk-live-abc123"
	in := "HTTP 400: INVALID_ARGUMENT: bad key sk-live-abc123 see https://generativelanguage.googleapis.com/v1beta/models/x:embedContent?key=sk-live-abc123&alt=json and extra https://x.test/v?foo=1&key=other-secret-value#frag"
	got := redactSecrets(in, key)
	if strings.Contains(got, key) {
		t.Fatalf("configured key leaked: %q", got)
	}
	if strings.Contains(got, "other-secret-value") {
		t.Fatalf("?key= query value leaked: %q", got)
	}
	if !strings.Contains(got, "?key=[redacted]") {
		t.Fatalf("expected ?key=[redacted] in %q", got)
	}
	if !strings.Contains(got, "&key=[redacted]") {
		t.Fatalf("expected &key=[redacted] in %q", got)
	}
	if !strings.Contains(got, "INVALID_ARGUMENT") || !strings.Contains(got, "HTTP 400") {
		t.Fatalf("lost status/message: %q", got)
	}
}

func TestGeminiEmbedIncludesStatusAndMessage(t *testing.T) {
	const secret = "cfg-secret-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"API key not valid. docs at https://ai.google.dev/?key=` + secret + `","status":"INVALID_ARGUMENT"}}`))
	}))
	defer srv.Close()

	g := &geminiEmbedder{
		apiKey:     secret,
		model:      defaultEmbedModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		batchSize:  8,
		maxRetries: 1,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	_, err := g.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 400") {
		t.Fatalf("missing status code: %s", msg)
	}
	if !strings.Contains(msg, "INVALID_ARGUMENT") {
		t.Fatalf("missing Gemini status: %s", msg)
	}
	if !strings.Contains(msg, "API key not valid") {
		t.Fatalf("missing Gemini message: %s", msg)
	}
	if strings.Contains(msg, secret) {
		t.Fatalf("key leaked: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "?key="+secret) {
		t.Fatalf("key query leaked: %s", msg)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
