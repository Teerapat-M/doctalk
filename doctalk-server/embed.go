package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type geminiEmbedder struct {
	apiKey     string
	model      string
	baseURL    string
	http       *http.Client
	batchSize  int
	maxRetries int
	sleep      func(context.Context, time.Duration) error
	mu         sync.Mutex
}

func newGeminiEmbedder(cfg config) *geminiEmbedder {
	return &geminiEmbedder{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiEmbeddingModel,
		baseURL:    "https://generativelanguage.googleapis.com",
		http:       &http.Client{Timeout: 60 * time.Second},
		batchSize:  cfg.GeminiEmbedBatch,
		maxRetries: 6,
		sleep:      defaultSleep,
	}
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (g *geminiEmbedder) endpoint() string {
	model := g.model
	if model == "" {
		model = defaultEmbedModel
	}
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return strings.TrimRight(g.baseURL, "/") + "/v1beta/" + model + ":batchEmbedContents"
}

func (g *geminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	batch := g.batchSize
	if batch < 1 {
		batch = defaultEmbedBatch
	}

	// ponytail: process-wide lock so concurrent uploads don't stampede free-tier RPM.
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batch {
		end := i + batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := g.embedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

type embedRequestBody struct {
	Requests []embedRequest `json:"requests"`
}

type embedRequest struct {
	Model                string       `json:"model"`
	Content              embedContent `json:"content"`
	TaskType             string       `json:"taskType"`
	OutputDimensionality int          `json:"outputDimensionality"`
}

type embedContent struct {
	Parts []embedPart `json:"parts"`
}

type embedPart struct {
	Text string `json:"text"`
}

type embedResponseBody struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}

func (g *geminiEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	model := g.model
	if model == "" {
		model = defaultEmbedModel
	}
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	reqs := make([]embedRequest, len(texts))
	for i, t := range texts {
		reqs[i] = embedRequest{
			Model: model,
			Content: embedContent{
				Parts: []embedPart{{Text: t}},
			},
			TaskType:             "RETRIEVAL_DOCUMENT",
			OutputDimensionality: EmbeddingDim,
		}
	}
	payload, err := json.Marshal(embedRequestBody{Requests: reqs})
	if err != nil {
		return nil, err
	}

	var last error
	delay := 500 * time.Millisecond
	attempts := g.maxRetries
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		vecs, retryAfter, err := g.doEmbed(ctx, payload)
		if err == nil {
			return vecs, nil
		}
		last = err
		if retryAfter < 0 {
			return nil, err
		}
		wait := delay
		if retryAfter > 0 {
			wait = retryAfter
		}
		if err := g.sleep(ctx, wait); err != nil {
			return nil, err
		}
		if delay < 16*time.Second {
			delay *= 2
		}
	}
	return nil, fmt.Errorf("gemini embedding: retries exhausted: %w", last)
}

// doEmbed returns retryAfter >= 0 when the caller should retry (429/503).
// retryAfter < 0 means a non-retryable error. The API key is never included
// in returned errors.
func (g *geminiEmbedder) doEmbed(ctx context.Context, payload []byte) ([][]float32, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	res, err := g.http.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("gemini embedding: request failed")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		return nil, parseRetryAfter(res.Header.Get("Retry-After")), fmt.Errorf("gemini embedding: HTTP %d", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, -1, fmt.Errorf("gemini embedding: HTTP %d", res.StatusCode)
	}

	var parsed embedResponseBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, -1, fmt.Errorf("gemini embedding: bad response")
	}
	if len(parsed.Embeddings) == 0 {
		return nil, -1, fmt.Errorf("gemini embedding: empty response")
	}
	out := make([][]float32, len(parsed.Embeddings))
	for i, e := range parsed.Embeddings {
		if len(e.Values) != EmbeddingDim {
			return nil, -1, fmt.Errorf("gemini embedding: got %d dims, want %d", len(e.Values), EmbeddingDim)
		}
		v := make([]float32, len(e.Values))
		for j, x := range e.Values {
			v[j] = float32(x)
		}
		l2normalize(v)
		out[i] = v
	}
	return out, -1, nil
}

func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func l2normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := math.Sqrt(sum)
	if n == 0 {
		return
	}
	inv := float32(1 / n)
	for i := range v {
		v[i] *= inv
	}
}

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
