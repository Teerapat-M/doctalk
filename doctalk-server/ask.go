package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

type askRequest struct {
	Question    string   `json:"question"`
	DocumentIDs []string `json:"document_ids"`
	K           int      `json:"k"`
}

type deltaEvent struct {
	Text string `json:"text"`
}

type citationJSON struct {
	DocumentID string `json:"document_id"`
	PageNumber int    `json:"page_number"`
	ChunkID    string `json:"chunk_id"`
	Snippet    string `json:"snippet"`
}

type finalEvent struct {
	Citations []citationJSON `json:"citations"`
}

type sseErrorEvent struct {
	Error string `json:"error"`
}

type retrievedChunk struct {
	ID         string
	DocumentID string
	PageNumber int
	Content    string
}

func (s *server) ask(c *gin.Context) {
	var req askRequest
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}
	if utf8.RuneCountInString(question) > maxQuestionRunes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is too long"})
		return
	}
	docIDs := uniqueNonEmpty(req.DocumentIDs)
	if len(docIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document_ids is required"})
		return
	}
	if len(docIDs) > maxAskDocumentIDs {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many document_ids"})
		return
	}
	if s.embed == nil || s.chat == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GEMINI_API_KEY is not configured"})
		return
	}
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}

	k := clampK(req.K, s.cfg.AskTopK, s.cfg.AskTopKMax)
	timeout := s.cfg.AskTimeout
	if timeout <= 0 {
		timeout = time.Duration(defaultAskTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	if err := s.ensureAskDocs(ctx, docIDs); err != nil {
		if errors.Is(err, errAskDocsNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		if errors.Is(err, errAskDocsNotReady) {
			c.JSON(http.StatusConflict, gin.H{"error": "document is not ready"})
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "ask timed out"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load documents"})
		return
	}

	qvec, err := s.embed.EmbedQuery(ctx, question)
	if err != nil {
		s.writeAskErr(c, false, err)
		return
	}
	if len(qvec) != EmbeddingDim {
		c.JSON(http.StatusBadGateway, gin.H{"error": "gemini embedding: unexpected dimension"})
		return
	}

	chunks, err := s.retrieve(ctx, docIDs, qvec, k)
	if err != nil {
		s.writeAskErr(c, false, err)
		return
	}

	citations := citationsFromChunks(chunks)
	s.startSSE(c)
	started := true

	emit := func(delta string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return writeSSE(c.Writer, "delta", deltaEvent{Text: delta})
	}

	if len(chunks) == 0 {
		if err := emit(notFoundReply(question)); err != nil {
			return
		}
	} else if err := s.chat.StreamAnswer(ctx, question, chunks, emit); err != nil {
		s.writeAskErr(c, started, err)
		return
	}
	_ = writeSSE(c.Writer, "final", finalEvent{Citations: citations})
}

func (s *server) startSSE(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if f, ok := c.Writer.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *server) writeAskErr(c *gin.Context, started bool, err error) {
	msg := redactErr(err, s.cfg.GeminiAPIKey)
	if errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		msg = "ask timed out"
		if !started {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": msg})
			return
		}
		_ = writeSSE(c.Writer, "error", sseErrorEvent{Error: msg})
		return
	}
	if !started {
		status := http.StatusBadGateway
		if !strings.Contains(msg, "gemini") {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}
	_ = writeSSE(c.Writer, "error", sseErrorEvent{Error: msg})
}

func writeSSE(w http.ResponseWriter, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

var (
	errAskDocsNotFound = errors.New("ask docs not found")
	errAskDocsNotReady = errors.New("ask docs not ready")
)

func (s *server) ensureAskDocs(ctx context.Context, ids []string) error {
	q := `
		SELECT id::text, status FROM documents
		WHERE id = ANY($1::uuid[])
	`
	rows, err := s.db.QueryContext(ctx, q, "{"+strings.Join(ids, ",")+"}")
	if err != nil {
		if isBadUUID(err) {
			return errAskDocsNotFound
		}
		return err
	}
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return err
		}
		found[id] = status
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(found) != len(ids) {
		return errAskDocsNotFound
	}
	for _, st := range found {
		if st != statusDone {
			return errAskDocsNotReady
		}
	}
	return nil
}

func (s *server) retrieve(ctx context.Context, docIDs []string, query []float32, k int) ([]retrievedChunk, error) {
	if k < 1 {
		k = 1
	}
	q := `
		SELECT c.id::text, c.document_id::text, c.page_number, c.content
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE c.document_id = ANY($1::uuid[])
		  AND d.status = $2
		  AND c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $3::vector
		LIMIT $4
	`
	rows, err := s.db.QueryContext(ctx, q, "{"+strings.Join(docIDs, ",")+"}", statusDone, vectorLiteral(query), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]retrievedChunk, 0, k)
	for rows.Next() {
		var ch retrievedChunk
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.PageNumber, &ch.Content); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func citationsFromChunks(chunks []retrievedChunk) []citationJSON {
	out := make([]citationJSON, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, citationJSON{
			DocumentID: ch.DocumentID,
			PageNumber: ch.PageNumber,
			ChunkID:    ch.ID,
			Snippet:    citationSnippet(ch.Content, citationSnippetRunes),
		})
	}
	return out
}

func citationSnippet(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes < 1 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func clampK(reqK, def, max int) int {
	if max < 1 {
		max = defaultAskTopKMax
	}
	k := def
	if reqK > 0 {
		k = reqK
	}
	if k < 1 {
		k = 1
	}
	if k > max {
		k = max
	}
	return k
}

func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func notFoundReply(question string) string {
	if questionIsThai(question) {
		return "ไม่พบคำตอบในเอกสาร"
	}
	return "The answer is not found in the document."
}

func questionIsThai(q string) bool {
	for _, r := range q {
		if r >= 0x0E00 && r <= 0x0E7F {
			return true
		}
	}
	return false
}
