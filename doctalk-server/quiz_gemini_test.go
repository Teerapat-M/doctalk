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

func TestGeminiQuizJSONSchemaAndRetry(t *testing.T) {
	const secret = "quiz-gemini-secret"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.RawQuery != "" {
			t.Errorf("API key must not appear in the URL, got %q", r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != secret {
			t.Errorf("missing API key header")
		}
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path=%s", r.URL.Path)
		}
		if strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Errorf("quiz must not stream, path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), secret) {
			t.Errorf("API key leaked into body")
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("json: %v", err)
		}
		gen, _ := payload["generationConfig"].(map[string]any)
		if gen["responseMimeType"] != "application/json" {
			t.Errorf("mime=%v", gen["responseMimeType"])
		}
		schema, _ := json.Marshal(gen["responseSchema"])
		if !strings.Contains(string(schema), "source_index") || !strings.Contains(string(schema), "questions") {
			t.Errorf("schema=%s", schema)
		}
		if hits.Load() == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"slow down"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": `{"questions":[{"prompt":"Q?","options":{"A":"a","B":"b","C":"c","D":"d"},"correct":"B","explanation":"b","source_index":1}]}`}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	g := &geminiQuiz{
		apiKey:     secret,
		model:      "quiz-model",
		baseURL:    srv.URL,
		http:       srv.Client(),
		maxRetries: 4,
		budget:     time.Second,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	raw, err := g.GenerateJSON(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"correct":"B"`) {
		t.Fatalf("body=%s", raw)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits=%d", hits.Load())
	}
	if !strings.Contains(g.endpoint(), "quiz-model:generateContent") {
		t.Fatalf("endpoint=%s", g.endpoint())
	}
}

func TestGeminiQuizFallbackAfter503(t *testing.T) {
	var primary, fallback atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "quiz-primary") {
			primary.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"status":"UNAVAILABLE","message":"high demand"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "quiz-fallback") {
			fallback.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []any{
					map[string]any{
						"content": map[string]any{
							"parts": []any{map[string]any{"text": `{"questions":[]}`}},
						},
					},
				},
			})
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer srv.Close()

	g := &geminiQuiz{
		apiKey:     "k",
		model:      "quiz-primary",
		fallback:   "quiz-fallback",
		baseURL:    srv.URL,
		http:       srv.Client(),
		maxRetries: 2,
		budget:     time.Second,
		sleep:      func(context.Context, time.Duration) error { return nil },
	}
	raw, err := g.GenerateJSON(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"questions"`) {
		t.Fatalf("body=%s", raw)
	}
	if primary.Load() < 1 || fallback.Load() != 1 {
		t.Fatalf("primary=%d fallback=%d", primary.Load(), fallback.Load())
	}
}

func TestNewGeminiQuizModelDefaultsToChat(t *testing.T) {
	g := newGeminiQuiz(config{GeminiAPIKey: "k", GeminiChatModel: "chat-m", GeminiQuizModel: ""}, nil)
	if g.model != "chat-m" {
		t.Fatalf("model=%q", g.model)
	}
	g = newGeminiQuiz(config{GeminiAPIKey: "k", GeminiChatModel: "chat-m", GeminiQuizModel: "quiz-m"}, nil)
	if g.model != "quiz-m" {
		t.Fatalf("model=%q", g.model)
	}
}

func TestGeminiQuizRedactsKey(t *testing.T) {
	const secret = "leaky-quiz-key"
	g := &geminiQuiz{apiKey: secret}
	err := g.errf("boom %s", secret)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("leaked: %v", err)
	}
}
