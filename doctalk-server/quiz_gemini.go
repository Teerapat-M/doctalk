package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type geminiQuiz struct {
	apiKey     string
	model      string
	baseURL    string
	http       *http.Client
	maxRetries int
	budget     time.Duration
	sleep      func(context.Context, time.Duration) error
	limiter    *slotLimiter
}

func newGeminiQuiz(cfg config, lim *slotLimiter) *geminiQuiz {
	base := cfg.GeminiBaseURL
	if base == "" {
		base = defaultGeminiBaseURL
	}
	model := cfg.GeminiQuizModel
	if model == "" {
		model = cfg.GeminiChatModel
	}
	if model == "" {
		model = defaultChatModel
	}
	budget := cfg.GeminiRetryBudget
	if budget <= 0 {
		budget = defaultGeminiRetryBudget
	}
	return &geminiQuiz{
		apiKey:     cfg.GeminiAPIKey,
		model:      model,
		baseURL:    strings.TrimRight(base, "/"),
		http:       &http.Client{Timeout: 90 * time.Second},
		maxRetries: defaultGeminiAttempts,
		budget:     budget,
		sleep:      defaultSleep,
		limiter:    lim,
	}
}

func (g *geminiQuiz) endpoint() string {
	model := g.model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return strings.TrimRight(g.baseURL, "/") + "/v1beta/" + model + ":generateContent"
}

func quizResponseSchema() map[string]any {
	option := map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"A": map[string]any{"type": "STRING"},
			"B": map[string]any{"type": "STRING"},
			"C": map[string]any{"type": "STRING"},
			"D": map[string]any{"type": "STRING"},
		},
		"required": []string{"A", "B", "C", "D"},
	}
	question := map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"prompt":       map[string]any{"type": "STRING"},
			"options":      option,
			"correct":      map[string]any{"type": "STRING", "enum": []string{"A", "B", "C", "D"}},
			"explanation":  map[string]any{"type": "STRING"},
			"source_index": map[string]any{"type": "INTEGER"},
		},
		"required": []string{"prompt", "options", "correct", "explanation", "source_index"},
	}
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":  "ARRAY",
				"items": question,
			},
		},
		"required": []string{"questions"},
	}
}

func (g *geminiQuiz) GenerateJSON(ctx context.Context, system, user string) ([]byte, error) {
	return g.GenerateTypedJSON(ctx, system, user, quizResponseSchema())
}

func (g *geminiQuiz) GenerateTypedJSON(ctx context.Context, system, user string, schema map[string]any) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": system}},
		},
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": user}}},
		},
		"generationConfig": map[string]any{
			"temperature":      0.4,
			"maxOutputTokens":  4096,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
		},
	})
	if err != nil {
		return nil, err
	}

	var out []byte
	policy := geminiRetry{
		attempts: g.maxRetries,
		budget:   g.budget,
		sleep:    g.sleep,
		limiter:  g.limiter,
	}
	err = policy.do(ctx, func() (time.Duration, error) {
		body, retryAfter, err := g.doGenerate(ctx, payload)
		if err == nil {
			out = body
		}
		return retryAfter, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (g *geminiQuiz) doGenerate(ctx context.Context, payload []byte) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, -1, g.errf("gemini quiz: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	res, err := g.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, -1, ctx.Err()
		}
		return nil, -1, g.errf("gemini quiz: request failed")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		return nil, parseRetryAfter(res.Header.Get("Retry-After")), g.httpErr(res.StatusCode, body)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, -1, g.httpErr(res.StatusCode, body)
	}
	text, err := geminiGenerateText(body)
	if err != nil {
		return nil, -1, err
	}
	return []byte(text), -1, nil
}

func geminiGenerateText(body []byte) (string, error) {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("gemini quiz: bad response")
	}
	if parsed.Error.Message != "" || parsed.Error.Status != "" {
		reason := parseGeminiError(body)
		if reason == "" {
			reason = "generate error"
		}
		return "", errors.New(redactSecrets("gemini quiz: "+reason, ""))
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini quiz: empty response")
	}
	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

func (g *geminiQuiz) errf(format string, args ...any) error {
	return errors.New(redactSecrets(fmt.Sprintf(format, args...), g.apiKey))
}

func (g *geminiQuiz) httpErr(status int, body []byte) error {
	reason := parseGeminiError(body)
	if reason == "" {
		return g.errf("gemini quiz: HTTP %d", status)
	}
	return g.errf("gemini quiz: HTTP %d: %s", status, reason)
}
