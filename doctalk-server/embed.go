package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

type geminiEmbedder struct {
	apiKey     string
	model      string
	baseURL    string
	http       *http.Client
	batchSize  int
	maxRetries int
	budget     time.Duration
	sleep      func(context.Context, time.Duration) error
	limiter    *slotLimiter
}

func newGeminiEmbedder(cfg config, lim *slotLimiter) *geminiEmbedder {
	base := cfg.GeminiBaseURL
	if base == "" {
		base = defaultGeminiBaseURL
	}
	budget := cfg.GeminiRetryBudget
	if budget <= 0 {
		budget = defaultGeminiRetryBudget
	}
	return &geminiEmbedder{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiEmbeddingModel,
		baseURL:    strings.TrimRight(base, "/"),
		http:       &http.Client{Timeout: 60 * time.Second},
		batchSize:  cfg.GeminiEmbedBatch,
		maxRetries: defaultGeminiAttempts,
		budget:     budget,
		sleep:      defaultSleep,
		limiter:    lim,
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

	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batch {
		end := i + batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := g.embedBatch(ctx, texts[i:end], "RETRIEVAL_DOCUMENT")
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (g *geminiEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, g.errf("gemini embedding: empty query")
	}
	vecs, err := g.embedBatch(ctx, []string{text}, "RETRIEVAL_QUERY")
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, g.errf("gemini embedding: empty query response")
	}
	return vecs[0], nil
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

func (g *geminiEmbedder) embedBatch(ctx context.Context, texts []string, taskType string) ([][]float32, error) {
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
			TaskType:             taskType,
			OutputDimensionality: EmbeddingDim,
		}
	}
	payload, err := json.Marshal(embedRequestBody{Requests: reqs})
	if err != nil {
		return nil, err
	}

	var vecs [][]float32
	policy := geminiRetry{
		attempts: g.maxRetries,
		budget:   g.budget,
		sleep:    g.sleep,
		limiter:  g.limiter,
	}
	err = policy.do(ctx, func() (time.Duration, error) {
		var retryAfter time.Duration
		var callErr error
		vecs, retryAfter, callErr = g.doEmbed(ctx, payload)
		return retryAfter, callErr
	})
	if err != nil {
		return nil, err
	}
	return vecs, nil
}

// doEmbed returns retryAfter >= 0 when the caller should retry (429/503).
// retryAfter < 0 means a non-retryable error. The API key is never included
// in returned errors.
func (g *geminiEmbedder) doEmbed(ctx context.Context, payload []byte) ([][]float32, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, -1, g.errf("gemini embedding: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	res, err := g.http.Do(req)
	if err != nil {
		return nil, -1, g.errf("gemini embedding: request failed")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		return nil, parseRetryAfter(res.Header.Get("Retry-After")), g.httpErr(res.StatusCode, body)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, -1, g.httpErr(res.StatusCode, body)
	}

	var parsed embedResponseBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, -1, g.errf("gemini embedding: bad response")
	}
	if len(parsed.Embeddings) == 0 {
		return nil, -1, g.errf("gemini embedding: empty response")
	}
	out := make([][]float32, len(parsed.Embeddings))
	for i, e := range parsed.Embeddings {
		if len(e.Values) != EmbeddingDim {
			return nil, -1, g.errf("gemini embedding: got %d dims, want %d", len(e.Values), EmbeddingDim)
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

func (g *geminiEmbedder) errf(format string, args ...any) error {
	return errors.New(redactSecrets(fmt.Sprintf(format, args...), g.apiKey))
}

func (g *geminiEmbedder) httpErr(status int, body []byte) error {
	reason := parseGeminiError(body)
	if reason == "" {
		return g.errf("gemini embedding: HTTP %d", status)
	}
	return g.errf("gemini embedding: HTTP %d: %s", status, reason)
}

func parseGeminiError(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		s := strings.Join(strings.Fields(string(body)), " ")
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	msg := strings.TrimSpace(parsed.Error.Message)
	st := strings.TrimSpace(parsed.Error.Status)
	switch {
	case st != "" && msg != "":
		return st + ": " + msg
	case msg != "":
		return msg
	case st != "":
		return st
	default:
		return ""
	}
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

func parseVectorLiteral(s string) []float32 {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]{}")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := strconv.ParseFloat(p, 32)
		if err != nil {
			return nil
		}
		out = append(out, float32(f))
	}
	return out
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
