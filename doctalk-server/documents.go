package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	statusUploading  = "uploading"
	statusExtracting = "extracting"
	statusEmbedding  = "embedding"
	statusDone       = "done"
	statusFailed     = "failed"
)

type documentJSON struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"content_type"`
	ByteSize     int64     `json:"byte_size"`
	Status       string    `json:"status"`
	ErrorMessage *string   `json:"error_message"`
	PageCount    int       `json:"page_count"`
	ChunkCount   int       `json:"chunk_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *server) uploadDocument(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.cfg.MaxUploadBytes+1024)

	file, hdr, err := c.Request.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, s.cfg.MaxUploadBytes+1))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read file"})
		return
	}
	if int64(len(data)) > s.cfg.MaxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
		return
	}
	if !isPDF(data) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is not a PDF"})
		return
	}
	if s.embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GEMINI_API_KEY is not configured"})
		return
	}
	if s.db == nil || s.store == nil || s.extract == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}

	filename := filepath.Base(hdr.Filename)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "upload.pdf"
	}
	if !utf8.ValidString(filename) {
		filename = "upload.pdf"
	}

	ctx := c.Request.Context()
	var id string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO documents (filename, content_type, byte_size, status, storage_key)
		VALUES ($1, 'application/pdf', $2, $3, '')
		RETURNING id
	`, filename, int64(len(data)), statusUploading).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create document"})
		return
	}

	key := id + ".pdf"
	if err := s.store.Put(ctx, key, bytes.NewReader(data)); err != nil {
		s.fail(ctx, id, fmt.Errorf("store: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not store file"})
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE documents SET storage_key=$2, updated_at=now() WHERE id=$1`, id, key); err != nil {
		s.fail(ctx, id, fmt.Errorf("store key: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not store file"})
		return
	}

	s.enqueueProcess(id, key)

	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		c.JSON(http.StatusAccepted, gin.H{"id": id, "status": statusUploading})
		return
	}
	c.JSON(http.StatusAccepted, doc)
}

func (s *server) enqueueProcess(id, key string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		s.process(ctx, id, key)
	}()
}

func (s *server) process(ctx context.Context, id, key string) {
	if err := s.setStatus(ctx, id, statusExtracting, 0, ""); err != nil {
		s.fail(ctx, id, err)
		return
	}

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

	pages, err := s.extract.ExtractPages(data)
	if err != nil {
		s.fail(ctx, id, fmt.Errorf("extract: %w", err))
		return
	}
	chunks := chunkPages(pages)
	if len(chunks) == 0 {
		s.fail(ctx, id, fmt.Errorf("extract: no extractable text"))
		return
	}
	if err := s.setStatus(ctx, id, statusEmbedding, len(pages), ""); err != nil {
		s.fail(ctx, id, err)
		return
	}

	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		texts[i] = ch.Text
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(ctx, id, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = $1`, id); err != nil {
		s.fail(ctx, id, err)
		return
	}
	for i, ch := range chunks {
		if len(vecs[i]) != EmbeddingDim {
			s.fail(ctx, id, fmt.Errorf("embed: vector dim %d, want %d", len(vecs[i]), EmbeddingDim))
			return
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chunks (document_id, chunk_index, page_number, content, embedding)
			VALUES ($1, $2, $3, $4, $5::vector)
		`, id, ch.Index, ch.Page, ch.Text, vectorLiteral(vecs[i])); err != nil {
			s.fail(ctx, id, fmt.Errorf("store chunks: %w", err))
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents SET status=$2, error_message=NULL, page_count=$3, updated_at=now() WHERE id=$1
	`, id, statusDone, len(pages)); err != nil {
		s.fail(ctx, id, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.fail(ctx, id, err)
	}
}

func (s *server) listDocuments(c *gin.Context) {
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}
	docs, err := s.loadDocuments(c.Request.Context(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list documents"})
		return
	}
	if docs == nil {
		docs = []documentJSON{}
	}
	c.JSON(http.StatusOK, docs)
}

func (s *server) getDocument(c *gin.Context) {
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	doc, err := s.loadDocument(c.Request.Context(), id)
	if err == sql.ErrNoRows || isBadUUID(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load document"})
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (s *server) loadDocument(ctx context.Context, id string) (documentJSON, error) {
	docs, err := s.loadDocuments(ctx, id)
	if err != nil {
		return documentJSON{}, err
	}
	if len(docs) == 0 {
		return documentJSON{}, sql.ErrNoRows
	}
	return docs[0], nil
}

func (s *server) loadDocuments(ctx context.Context, id string) ([]documentJSON, error) {
	q := `
		SELECT d.id::text, d.filename, d.content_type, d.byte_size, d.status,
		       d.error_message, d.page_count, d.created_at, d.updated_at,
		       COUNT(c.id)::int
		FROM documents d
		LEFT JOIN chunks c ON c.document_id = d.id
	`
	args := []any{}
	if id != "" {
		q += ` WHERE d.id = $1`
		args = append(args, id)
	}
	q += ` GROUP BY d.id ORDER BY d.created_at DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]documentJSON, 0)
	for rows.Next() {
		var d documentJSON
		var errMsg sql.NullString
		if err := rows.Scan(
			&d.ID, &d.Filename, &d.ContentType, &d.ByteSize, &d.Status,
			&errMsg, &d.PageCount, &d.CreatedAt, &d.UpdatedAt, &d.ChunkCount,
		); err != nil {
			return nil, err
		}
		if errMsg.Valid {
			d.ErrorMessage = &errMsg.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *server) setStatus(ctx context.Context, id, status string, pageCount int, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE documents
		SET status=$2, page_count=CASE WHEN $3 > 0 THEN $3 ELSE page_count END,
		    error_message=NULLIF($4, ''), updated_at=now()
		WHERE id=$1
	`, id, status, pageCount, errMsg)
	return err
}

func (s *server) fail(ctx context.Context, id string, err error) {
	msg := redactErr(err, s.cfg.GeminiAPIKey)
	log.Printf("document %s failed: %s", id, msg)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE documents SET status=$2, error_message=$3, updated_at=now() WHERE id=$1
	`, id, statusFailed, msg)
}

func isBadUUID(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "invalid input syntax for type uuid")
}

func redactErr(err error, apiKey string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if apiKey != "" {
		msg = strings.ReplaceAll(msg, apiKey, "[redacted]")
	}
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return msg
}
