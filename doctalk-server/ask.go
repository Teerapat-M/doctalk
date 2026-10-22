package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

type askRequest struct {
	Question    string   `json:"question"`
	DocumentIDs []string `json:"document_ids"`
	K           *int     `json:"k"`
}

type deltaEvent struct {
	Text string `json:"text"`
}

type citationJSON struct {
	DocumentID string `json:"document_id"`
	PageNumber int    `json:"page_number,omitempty"`
	SheetName  string `json:"sheet_name,omitempty"`
	RowNumber  int    `json:"row_number,omitempty"`
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
	ID          string
	DocumentID  string
	PageNumber  int
	SheetName   string
	RowNumber   int
	Content     string
	Question    string
	Answer      string
	Explanation string
	Embedding   []float32
}

func (s *server) ask(c *gin.Context) {
	var req askRequest
	if !requireJSONBody(c, &req) {
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
	if invalidDocumentIDs(docIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
		return
	}
	k, kerr := parseOptionalK(req.K, s.cfg.AskTopK, s.cfg.AskTopKMax)
	if kerr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": kerr})
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

	timeout := s.cfg.AskTimeout
	if timeout <= 0 {
		timeout = time.Duration(defaultAskTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	if err := s.ensureAskDocs(ctx, docIDs, guestIDFrom(c)); err != nil {
		if errors.Is(err, errAskDocsBadID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
			return
		}
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

	res, rejected := s.reserveQuota(c, rateAsk)
	if rejected {
		return
	}
	keep := false
	defer func() {
		if !keep {
			res.release()
		}
	}()

	s.startSSE(c)

	qvec, err := s.embed.EmbedQuery(ctx, question)
	if err != nil {
		s.writeAskErr(c, true, err)
		return
	}
	if len(qvec) != EmbeddingDim {
		s.writeAskErr(c, true, errors.New("gemini embedding: unexpected dimension"))
		return
	}

	chunks, err := s.retrieve(ctx, docIDs, qvec, k)
	if err != nil {
		s.writeAskErr(c, true, err)
		return
	}

	emit := func(delta string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return writeSSE(c.Writer, "delta", deltaEvent{Text: delta})
	}

	notFound := false
	if len(chunks) == 0 {
		notFound = true
		if err := emit(notFoundReply(question)); err != nil {
			return
		}
	} else {
		var gate answerGate
		if err := s.chat.StreamAnswer(ctx, question, chunks, func(delta string) error {
			return gate.push(delta, emit)
		}); err != nil {
			s.writeAskErr(c, true, err)
			return
		}
		if err := gate.flush(emit); err != nil {
			s.writeAskErr(c, true, err)
			return
		}
		notFound = gate.notFound
	}
	citations := citationsFromChunks(chunks)
	if notFound {
		citations = []citationJSON{}
	}
	_ = writeSSE(c.Writer, "final", finalEvent{Citations: citations})
	keep = true
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
	}
	log.Printf("ask: %s", msg)
	if !started {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		} else if !strings.Contains(msg, "gemini") {
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
	errAskDocsBadID    = errors.New("ask docs bad id")
)

func (s *server) ensureAskDocs(ctx context.Context, ids []string, guest string) error {
	q := `
		SELECT id::text, status FROM documents
		WHERE id = ANY($1::uuid[]) AND guest_id = $2
	`
	rows, err := s.db.QueryContext(ctx, q, "{"+strings.Join(ids, ",")+"}", guest)
	if err != nil {
		if isBadUUID(err) {
			return errAskDocsBadID
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
		SELECT c.id::text, c.document_id::text, c.page_number, c.sheet_name, c.row_number,
		       c.content, c.question, c.answer, c.explanation
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
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.PageNumber, &ch.SheetName, &ch.RowNumber, &ch.Content, &ch.Question, &ch.Answer, &ch.Explanation); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func citationsFromChunks(chunks []retrievedChunk) []citationJSON {
	out := make([]citationJSON, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, citationFromChunk(ch))
	}
	return out
}

func citationFromChunk(ch retrievedChunk) citationJSON {
	return citationJSON{
		DocumentID: ch.DocumentID,
		PageNumber: ch.PageNumber,
		SheetName:  ch.SheetName,
		RowNumber:  ch.RowNumber,
		ChunkID:    ch.ID,
		Snippet:    citationSnippet(ch.Content, citationSnippetRunes),
	}
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
