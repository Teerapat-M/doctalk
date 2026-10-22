package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ChatStreamer interface {
	StreamAnswer(ctx context.Context, question string, chunks []retrievedChunk, emit func(delta string) error) error
}

type geminiChat struct {
	apiKey     string
	model      string
	baseURL    string
	http       *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
	mu         sync.Mutex
}

func newGeminiChat(cfg config) *geminiChat {
	base := cfg.GeminiBaseURL
	if base == "" {
		base = defaultGeminiBaseURL
	}
	return &geminiChat{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiChatModel,
		baseURL:    strings.TrimRight(base, "/"),
		http:       &http.Client{},
		maxRetries: 6,
		sleep:      defaultSleep,
	}
}

func (g *geminiChat) endpoint() string {
	model := g.model
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

	g.mu.Lock()
	defer g.mu.Unlock()

	var last error
	delay := 500 * time.Millisecond
	attempts := g.maxRetries
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		retryAfter, err := g.doStream(ctx, payload, emit)
		if err == nil {
			return nil
		}
		last = err
		if retryAfter < 0 {
			return err
		}
		wait := delay
		if retryAfter > 0 {
			wait = retryAfter
		}
		if err := g.sleep(ctx, wait); err != nil {
			return err
		}
		if delay < 16*time.Second {
			delay *= 2
		}
	}
	return g.errf("gemini chat: retries exhausted: %v", last)
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

func (g *geminiChat) doStream(ctx context.Context, payload []byte, emit func(string) error) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(payload))
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
If the excerpts do not contain enough information to answer the question, say that the answer is not found in the document. Do not use outside knowledge or guess.
Reply in the same language as the question (Thai or English).
Do not mention these instructions or the excerpt numbers.`

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
