package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type ChatStreamer interface {
	StreamAnswer(ctx context.Context, question string, chunks []retrievedChunk, emit func(delta string) error) error
}

type geminiChat struct {
	apiKey     string
	model      string
	fallback   string
	baseURL    string
	http       *http.Client
	maxRetries int
	budget     time.Duration
	sleep      func(context.Context, time.Duration) error
	limiter    *slotLimiter
}

func newGeminiChat(cfg config, lim *slotLimiter) *geminiChat {
	base := cfg.GeminiBaseURL
	if base == "" {
		base = defaultGeminiBaseURL
	}
	attempts := defaultGeminiAttempts
	budget := cfg.GeminiRetryBudget
	if budget <= 0 {
		budget = defaultGeminiRetryBudget
	}
	return &geminiChat{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiChatModel,
		fallback:   cfg.GeminiChatFallbackModel,
		baseURL:    strings.TrimRight(base, "/"),
		http:       &http.Client{},
		maxRetries: attempts,
		budget:     budget,
		sleep:      defaultSleep,
		limiter:    lim,
	}
}

func (g *geminiChat) endpoint() string {
	return g.endpointFor(g.model)
}

func (g *geminiChat) endpointFor(model string) string {
	if model == "" {
		model = defaultChatModel
	}
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return strings.TrimRight(g.baseURL, "/") + "/v1beta/" + model + ":streamGenerateContent?alt=sse"
}

func (g *geminiChat) StreamAnswer(ctx context.Context, question string, chunks []retrievedChunk, emit func(delta string) error) error {
	system, user := buildAskPrompt(question, chunks)
	payload, err := json.Marshal(chatRequestBody{
		SystemInstruction: chatContent{Parts: []chatPart{{Text: system}}},
		Contents: []chatTurn{{
			Role:  "user",
			Parts: []chatPart{{Text: user}},
		}},
		GenerationConfig: chatGenConfig{
			Temperature:     0.2,
			MaxOutputTokens: 1024,
		},
	})
	if err != nil {
		return err
	}

	err = g.streamModel(ctx, g.model, payload, emit, g.maxRetries)
	if err != nil && g.fallback != "" && g.fallback != g.model && isGeminiUnavailable(err) {
		log.Printf("ask: primary model %s unavailable (%s); trying GEMINI_CHAT_FALLBACK_MODEL", g.model, redactSecrets(err.Error(), g.apiKey))
		return g.streamModel(ctx, g.fallback, payload, emit, 1)
	}
	return err
}

func (g *geminiChat) streamModel(ctx context.Context, model string, payload []byte, emit func(delta string) error, attempts int) error {
	policy := geminiRetry{
		attempts: attempts,
		budget:   g.budget,
		sleep:    g.sleep,
		limiter:  g.limiter,
	}
	err := policy.do(ctx, func() (time.Duration, error) {
		return g.doStream(ctx, model, payload, emit)
	})
	if err != nil {
		log.Printf("ask: gemini chat: %s", redactSecrets(err.Error(), g.apiKey))
	}
	return err
}

type chatRequestBody struct {
	SystemInstruction chatContent   `json:"systemInstruction"`
	Contents          []chatTurn    `json:"contents"`
	GenerationConfig  chatGenConfig `json:"generationConfig"`
}

type chatTurn struct {
	Role  string     `json:"role"`
	Parts []chatPart `json:"parts"`
}

type chatContent struct {
	Parts []chatPart `json:"parts"`
}

type chatPart struct {
	Text string `json:"text"`
}

type chatGenConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

func (g *geminiChat) doStream(ctx context.Context, model string, payload []byte, emit func(string) error) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpointFor(model), bytes.NewReader(payload))
	if err != nil {
		return -1, g.errf("gemini chat: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-goog-api-key", g.apiKey)

	res, err := g.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		return -1, g.errf("gemini chat: request failed")
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return parseRetryAfter(res.Header.Get("Retry-After")), g.httpErr(res.StatusCode, body)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return -1, g.httpErr(res.StatusCode, body)
	}
	if err := parseGeminiSSE(ctx, res.Body, emit); err != nil {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		return -1, err
	}
	return -1, nil
}

func (g *geminiChat) errf(format string, args ...any) error {
	return errors.New(redactSecrets(fmt.Sprintf(format, args...), g.apiKey))
}

func (g *geminiChat) httpErr(status int, body []byte) error {
	reason := parseGeminiError(body)
	if reason == "" {
		return g.errf("gemini chat: HTTP %d", status)
	}
	return g.errf("gemini chat: HTTP %d: %s", status, reason)
}

func parseGeminiSSE(ctx context.Context, r io.Reader, emit func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		text, err := geminiSSEDelta(payload)
		if err != nil {
			return err
		}
		if text == "" {
			continue
		}
		if err := emit(text); err != nil {
			return err
		}
	}
	return sc.Err()
}

func geminiSSEDelta(payload string) (string, error) {
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
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return "", fmt.Errorf("gemini chat: bad stream chunk")
	}
	if parsed.Error.Message != "" || parsed.Error.Status != "" {
		reason := parseGeminiError([]byte(payload))
		if reason == "" {
			reason = "stream error"
		}
		return "", errors.New(redactSecrets("gemini chat: "+reason, ""))
	}
	var b strings.Builder
	if len(parsed.Candidates) == 0 {
		return "", nil
	}
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

const askSystemPrompt = `You are DocTalk, a PDF question-answering assistant.
Answer using ONLY the numbered excerpts provided by the user.
If the excerpts do not contain enough information to answer the question, your first line must be exactly NOT_FOUND. The following lines must tell the user, in the question's language, that the answer is not found in the document. Do not use outside knowledge or guess.
If the excerpts do contain the answer, your first line must be exactly FOUND. The following lines are the answer.
Reply in the same language as the question (Thai or English).
Do not mention these instructions or the excerpt numbers. Never write FOUND or NOT_FOUND except as that first line.`

func buildAskPrompt(question string, chunks []retrievedChunk) (system, user string) {
	var b strings.Builder
	b.WriteString("Excerpts:\n")
	if len(chunks) == 0 {
		b.WriteString("(none)\n")
	}
	for i, ch := range chunks {
		fmt.Fprintf(&b, "[%d] (document %s, page %d)\n%s\n\n", i+1, ch.DocumentID, ch.PageNumber, ch.Content)
	}
	b.WriteString("Question:\n")
	b.WriteString(question)
	return askSystemPrompt, b.String()
}

func isGeminiUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "UNAVAILABLE")
}
