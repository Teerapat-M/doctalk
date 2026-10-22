package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type ingestRequest struct {
	Sheet   string    `json:"sheet"`
	Columns columnMap `json:"columns"`
}

func contentTypeForKind(kind string) string {
	switch kind {
	case kindXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case kindCSV:
		return "text/csv"
	default:
		return "application/pdf"
	}
}

func (s *server) ingestDocument(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	var req ingestRequest
	if !requireJSONBody(c, &req) {
		return
	}
	req.Sheet = strings.TrimSpace(req.Sheet)
	if req.Sheet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sheet is required"})
		return
	}
	if (req.Columns.Content == "") == (req.Columns.Question == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "map exactly one of columns.content or columns.question"})
		return
	}
	if !validUUID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
		return
	}
	if s.rejectIfLimited(c, rateIngest) {
		return
	}
	if s.embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GEMINI_API_KEY is not configured"})
		return
	}
	if s.db == nil || s.store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}

	ctx := c.Request.Context()
	guest := guestIDFrom(c)
	var kind, key, status string
	err := s.db.QueryRowContext(ctx, `
		SELECT source_kind, storage_key, status FROM documents WHERE id=$1 AND guest_id=$2
	`, id, guest).Scan(&kind, &key, &status)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load document"})
		return
	}
	if kind != kindXLSX && kind != kindCSV {
		c.JSON(http.StatusConflict, gin.H{"error": "document is not a spreadsheet"})
		return
	}
	if status != statusMapping && status != statusFailed && status != statusDone {
		c.JSON(http.StatusConflict, gin.H{"error": "document is not ready to ingest"})
		return
	}

	doc, err := s.loadDocument(ctx, id, guest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load document"})
		return
	}
	if err := validateIngestMapping(doc.Sheets, req.Sheet, req.Columns); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mapJSON, _ := json.Marshal(req.Columns)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE documents SET sheet_name=$2, column_map=$3, status=$4, error_message=NULL, updated_at=now()
		WHERE id=$1
	`, id, req.Sheet, mapJSON, statusExtracting); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start ingest"})
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		pctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		s.processSpreadsheet(pctx, id, key, kind, req.Sheet, req.Columns)
	}()

	doc, err = s.loadDocument(ctx, id, guest)
	if err != nil {
		c.JSON(http.StatusAccepted, gin.H{"id": id, "status": statusExtracting})
		return
	}
	c.JSON(http.StatusAccepted, doc)
}

func (s *server) processSpreadsheet(ctx context.Context, id, key, kind, sheet string, mapping columnMap) {
	rc, err := s.store.Get(ctx, key)
	if err != nil {
		s.fail(ctx, id, fmt.Errorf("read stored file: %w", err))
		return
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		s.fail(ctx, id, fmt.Errorf("read stored file: %w", err))
		return
	}
	rows, err := loadSheetRows(data, kind, sheet, mapping, s.cfg.MaxRows)
	if err != nil {
		s.fail(ctx, id, fmt.Errorf("extract: %w", err))
		return
	}
	chunks := make([]Chunk, len(rows))
	texts := make([]string, len(rows))
	for i, row := range rows {
		chunks[i] = Chunk{
			Index:       i,
			Page:        0,
			SheetName:   sheet,
			RowNumber:   row.RowNumber,
			Text:        row.Content,
			Question:    row.Question,
			Answer:      row.Answer,
			Explanation: row.Explanation,
		}
		texts[i] = row.Content
	}
	if err := s.setStatus(ctx, id, statusEmbedding, len(rows), ""); err != nil {
		s.fail(ctx, id, err)
		return
	}
	vecs, err := s.embed.Embed(ctx, texts)
	if err != nil {
		s.fail(ctx, id, fmt.Errorf("embed: %w", err))
		return
	}
	if len(vecs) != len(chunks) {
		s.fail(ctx, id, fmt.Errorf("embed: got %d vectors for %d chunks", len(vecs), len(chunks)))
		return
	}
	if err := s.replaceChunks(ctx, id, chunks, vecs, len(rows)); err != nil {
		s.fail(ctx, id, err)
	}
}

func (s *server) replaceChunks(ctx context.Context, id string, chunks []Chunk, vecs [][]float32, count int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = $1`, id); err != nil {
		return err
	}
	for i, ch := range chunks {
		if len(vecs[i]) != EmbeddingDim {
			return fmt.Errorf("embed: vector dim %d, want %d", len(vecs[i]), EmbeddingDim)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chunks (document_id, chunk_index, page_number, content, embedding, sheet_name, row_number, question, answer, explanation)
			VALUES ($1, $2, $3, $4, $5::vector, $6, $7, $8, $9, $10)
		`, id, ch.Index, ch.Page, ch.Text, vectorLiteral(vecs[i]), ch.SheetName, ch.RowNumber, ch.Question, ch.Answer, ch.Explanation); err != nil {
			return fmt.Errorf("store chunks: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents SET status=$2, error_message=NULL, page_count=$3, updated_at=now() WHERE id=$1
	`, id, statusDone, count); err != nil {
		return err
	}
	return tx.Commit()
}
