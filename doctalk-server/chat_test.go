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

func TestParseGeminiSSEDeltas(t *testing.T) {
	body := "event: ignored\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]}}]}\n\n" +
		"data: [DONE]\n\n"
	var got strings.Builder
	if err := parseGeminiSSE(context.Background(), strings.NewReader(body), func(s string) error {
		got.WriteString(s)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.String() != "Hello" {
		t.Fatalf("got %q", got.String())
	}
}

func TestGeminiChatStreamsAndRetries429(t *testing.T) {
	const secret = "chat-secret-key-not-real"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("key") != "" || strings.Contains(r.URL.RawQuery, secret) {
			t.Errorf("API key must not appear in the URL, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("missing alt=sse, got %q", r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != secret {
			t.Errorf("missing API key header")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), secret) {
			t.Errorf("API key leaked into body")
		}
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("path=%s", r.URL.Path)
		}
		if hits.Load() == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"slow down"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"yes\"}]}}]}\n\n"))
	}))
	defer srv.Close()

	g := &geminiChat{
		apiKey:     secret,
		model:      defaultChatModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		maxRetries: 4,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	var got strings.Builder
	err := g.StreamAnswer(context.Background(), "What is this?", []retrievedChunk{{
		ID: "c1", DocumentID: "d1", PageNumber: 2, Content: "pgvector stores 768-d embeddings",
	}}, func(s string) error {
		got.WriteString(s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "yes" {
		t.Fatalf("got %q", got.String())
	}
	if hits.Load() < 2 {
		t.Fatalf("hits=%d, want 429 then success", hits.Load())
	}
}

func TestGeminiChatRedactsKeyOnError(t *testing.T) {
	const secret = "super-secret-chat-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"status":"INVALID_ARGUMENT","message":"bad https://x.test/v?key=` + secret + `"}}`))
	}))
	defer srv.Close()

	g := &geminiChat{
		apiKey:     secret,
		model:      defaultChatModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		maxRetries: 1,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	err := g.StreamAnswer(context.Background(), "hi", nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Fatalf("key leaked: %s", msg)
	}
	if !strings.Contains(msg, "HTTP 400") || !strings.Contains(msg, "INVALID_ARGUMENT") {
		t.Fatalf("lost status/message: %s", msg)
	}
}

func TestGeminiChatCancelsUpstream(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		if fl != nil {
			fl.Flush()
		}
		close(started)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"x\"}]}}]}\n\n"))
				if fl != nil {
					fl.Flush()
				}
			}
		}
	}))
	defer srv.Close()

	g := &geminiChat{
		apiKey:     "k",
		model:      defaultChatModel,
		baseURL:    srv.URL,
		http:       srv.Client(),
		maxRetries: 1,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	err := g.StreamAnswer(ctx, "hi", nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected cancel")
	}
	if ctx.Err() == nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildAskPromptIsGrounded(t *testing.T) {
	sys, user := buildAskPrompt("ภาษาไทยคืออะไร", []retrievedChunk{{
		DocumentID: "doc-1", PageNumber: 3, Content: "ไทยเป็นภาษา",
	}})
	for _, needle := range []string{"ONLY", "not found", "Thai or English"} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("system prompt missing %q: %s", needle, sys)
		}
	}
	if !strings.Contains(user, "ไทยเป็นภาษา") || !strings.Contains(user, "ภาษาไทยคืออะไร") {
		t.Fatalf("user prompt = %s", user)
	}
	if !strings.Contains(user, "page 3") {
		t.Fatalf("missing page: %s", user)
	}
}

func TestGeminiChatRequestOmitsKeyQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(r.URL.Query())
		if strings.Contains(string(raw), "key") && r.URL.Query().Get("key") != "" {
			t.Errorf("key query present")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n"))
	}))
	defer srv.Close()
	g := newGeminiChat(config{GeminiAPIKey: "abc", GeminiBaseURL: srv.URL, GeminiChatModel: "gemini-3.8-flash"})
	g.http = srv.Client()
	g.maxRetries = 1
	if err := g.StreamAnswer(context.Background(), "q", nil, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
