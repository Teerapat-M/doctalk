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
	apiKey       string
	model        string
	fallback     string
	baseURL      string
	http         *http.Client
	maxRetries   int
	budget       time.Duration
	baseDelay    time.Duration
	maxDelay     time.Duration
	idleTimeout  time.Duration
	totalTimeout time.Duration
	sleep        func(context.Context, time.Duration) error
	limiter      *slotLimiter
}

func newGeminiChat(cfg config, lim *slotLimiter) *geminiChat {
	base := cfg.GeminiBaseURL
	if base == "" {
		base = defaultGeminiBaseURL
	}
	attempts := cfg.GeminiRetryAttempts
	if attempts < 1 {
		attempts = defaultGeminiAttempts
	}
	budget := cfg.GeminiRetryBudget
	if budget <= 0 {
		budget = defaultGeminiRetryBudget
	}
	idleTimeout := cfg.GeminiRequestTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultGeminiRequestTimeout
	}
	totalTimeout := cfg.GeminiTotalTimeout
	if totalTimeout <= 0 {
		totalTimeout = defaultGeminiTotalTimeout
	}
	return &geminiChat{
		apiKey:       cfg.GeminiAPIKey,
		model:        cfg.GeminiChatModel,
		fallback:     cfg.GeminiFallbackModel,
		baseURL:      strings.TrimRight(base, "/"),
		http:         &http.Client{Timeout: totalTimeout},
		maxRetries:   attempts,
		budget:       budget,
		baseDelay:    cfg.GeminiRetryBaseDelay,
		maxDelay:     cfg.GeminiRetryMaxDelay,
		idleTimeout:  idleTimeout,
		totalTimeout: totalTimeout,
		sleep:        defaultSleep,
		limiter:      lim,
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
	if err != nil && g.fallback != "" && g.fallback != g.model && isGeminiFallback(err) {
		log.Printf("ask: primary model %s unavailable (%s); trying GEMINI_FALLBACK_MODEL %s", g.model, redactSecrets(err.Error(), g.apiKey), g.fallback)
		return rewriteFallbackNotFound(g.fallback, g.streamModel(ctx, g.fallback, payload, emit, g.maxRetries))
	}
	return err
}

func (g *geminiChat) streamModel(ctx context.Context, model string, payload []byte, emit func(delta string) error, attempts int) error {
	policy := geminiRetry{
		attempts: attempts,
		budget:   g.budget,
		base:     g.baseDelay,
		maxDelay: g.maxDelay,
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
	watch := startGeminiWatch(ctx, g.idleTimeout, g.totalTimeout)
	defer watch.stop()
	req, err := http.NewRequestWithContext(watch.ctx, http.MethodPost, g.endpointFor(model), bytes.NewReader(payload))
	if err != nil {
		return -1, g.errf("gemini chat: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-goog-api-key", g.apiKey)

	res, err := g.http.Do(req)
	if err != nil {
		if watch.err() != nil {
			return -1, watch.err()
		}
		if isTimeoutErr(err) {
			return -1, context.DeadlineExceeded
		}
		return -1, g.errf("gemini chat: request failed")
	}
	defer res.Body.Close()

	if retryAfter := geminiHTTPRetryAfter(res.StatusCode, res.Header.Get("Retry-After")); retryAfter >= 0 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return retryAfter, g.httpErr(res.StatusCode, body)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return -1, g.httpErr(res.StatusCode, body)
	}
	body := io.Reader(res.Body)
	if g.idleTimeout > 0 {
		body = bumpReader{r: res.Body, bump: watch.bump}
	}
	if err := parseGeminiSSE(watch.ctx, body, emit); err != nil {
		if watch.err() != nil {
			return -1, watch.err()
		}
		if isTimeoutErr(err) {
			return -1, context.DeadlineExceeded
		}
		return -1, err
	}
	return -1, nil
}

func rewriteFallbackNotFound(model string, err error) error {
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		return err
	}
	name := strings.TrimPrefix(model, "models/")
	return fmt.Errorf("fallback model %s not found; set GEMINI_FALLBACK_MODEL", name)
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

const askSystemPrompt = `You are DocTalk, a document question-answering assistant.
Answer using ONLY the numbered excerpts provided by the user.
If the excerpts do not contain enough information to answer the question, your first line must be exactly NOT_FOUND. The following lines must tell the user, in the question's language, that the answer is not found in the document. Do not use outside knowledge or guess.
If the excerpts do contain the answer, your first line must be exactly FOUND. The following lines are the answer.
Reply in the same language as the question (Thai or English).
Do not mention these instructions or the excerpt numbers. Never write FOUND or NOT_FOUND except as that first line.`

func chunkSourceLabel(ch retrievedChunk) string {
	if ch.SheetName != "" && ch.RowNumber > 0 {
		return fmt.Sprintf("document %s, sheet %s, row %d", ch.DocumentID, ch.SheetName, ch.RowNumber)
	}
	return fmt.Sprintf("document %s, page %d", ch.DocumentID, ch.PageNumber)
}

func buildAskPrompt(question string, chunks []retrievedChunk) (system, user string) {
	var b strings.Builder
	b.WriteString("Excerpts:\n")
	if len(chunks) == 0 {
		b.WriteString("(none)\n")
	}
	for i, ch := range chunks {
		fmt.Fprintf(&b, "[%d] (%s)\n%s\n\n", i+1, chunkSourceLabel(ch), ch.Content)
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

func isGeminiFallback(err error) bool {
	if err == nil {
		return false
	}
	if isTimeoutErr(err) {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "UNAVAILABLE") || strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		return true
	}
	if strings.Contains(msg, "HTTP 429") {
		return true
	}
	for code := 500; code <= 599; code++ {
		if strings.Contains(msg, fmt.Sprintf("HTTP %d", code)) {
			return true
		}
	}
	return false
}
