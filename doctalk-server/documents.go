package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
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
	ID           string      `json:"id"`
	Filename     string      `json:"filename"`
	ContentType  string      `json:"content_type"`
	ByteSize     int64       `json:"byte_size"`
	Status       string      `json:"status"`
	ErrorMessage *string     `json:"error_message"`
	PageCount    *int        `json:"page_count,omitempty"`
	RowCount     *int        `json:"row_count,omitempty"`
	ChunkCount   int         `json:"chunk_count"`
	SourceKind   string      `json:"source_kind"`
	SheetName    string      `json:"sheet_name,omitempty"`
	Sheets       []sheetInfo `json:"sheets,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
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
	filename := filepath.Base(hdr.Filename)
	if hasVBAProject(data) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "macro-enabled Excel files are not supported"})
		return
	}
	kind := detectKind(data, filename)
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is not a PDF, XLSX, or CSV"})
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
	if kind == kindPDF && s.extract == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}

	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "upload." + kind
	}
	if !utf8.ValidString(filename) {
		filename = "upload." + kind
	}

	ctype := contentTypeForKind(kind)
	status := statusUploading
	var sheets []sheetInfo
	if kind == kindXLSX || kind == kindCSV {
		parsed, err := parseWorkbook(data, kind)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		sheets = parsed
		status = statusMapping
	}

	res, rejected := s.reserveQuota(c, rateIngest)
	if rejected {
		return
	}
	keep := false
	defer func() {
		if !keep {
			res.release()
		}
	}()

	ctx := c.Request.Context()
	var id string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO documents (filename, content_type, byte_size, status, storage_key, source_kind, sheets_json, guest_id)
		VALUES ($1, $2, $3, $4, '', $5, $6, $7)
		RETURNING id
	`, filename, ctype, int64(len(data)), status, kind, sheetsJSON(sheets), guestIDFrom(c)).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create document"})
		return
	}

	key := id + "." + kind
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

	if kind == kindPDF {
		s.enqueueProcess(id, key)
	}

	doc, err := s.loadDocument(ctx, id, guestIDFrom(c))
	if err != nil {
		keep = true
		c.JSON(http.StatusAccepted, gin.H{"id": id, "status": status})
		return
	}
	keep = true
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
	if max := s.cfg.MaxPages; max > 0 && len(pages) > max {
		s.fail(ctx, id, fmt.Errorf("too many pages (max %d)", max))
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

	if err := s.replaceChunks(ctx, id, chunks, vecs, len(pages)); err != nil {
		s.fail(ctx, id, err)
	}
}

func (s *server) listDocuments(c *gin.Context) {
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}
	docs, err := s.loadDocuments(c.Request.Context(), "", guestIDFrom(c))
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
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	if !validUUID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
		return
	}
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}
	doc, err := s.loadDocument(c.Request.Context(), id, guestIDFrom(c))
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load document"})
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (s *server) loadDocument(ctx context.Context, id, guest string) (documentJSON, error) {
	docs, err := s.loadDocuments(ctx, id, guest)
	if err != nil {
		return documentJSON{}, err
	}
	if len(docs) == 0 {
		return documentJSON{}, sql.ErrNoRows
	}
	return docs[0], nil
}

func (s *server) loadDocuments(ctx context.Context, id, guest string) ([]documentJSON, error) {
	q := `
		SELECT d.id::text, d.filename, d.content_type, d.byte_size, d.status,
		       d.error_message, d.page_count, d.created_at, d.updated_at,
		       COUNT(c.id)::int, d.source_kind, d.sheet_name, d.sheets_json
		FROM documents d
		LEFT JOIN chunks c ON c.document_id = d.id
		WHERE d.guest_id = $1
	`
	args := []any{guest}
	if id != "" {
		q += ` AND d.id = $2`
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
		var sheets sql.NullString
		var storedCount int
		if err := rows.Scan(
			&d.ID, &d.Filename, &d.ContentType, &d.ByteSize, &d.Status,
			&errMsg, &storedCount, &d.CreatedAt, &d.UpdatedAt, &d.ChunkCount,
			&d.SourceKind, &d.SheetName, &sheets,
		); err != nil {
			return nil, err
		}
		if errMsg.Valid {
			d.ErrorMessage = &errMsg.String
		}
		if sheets.Valid && sheets.String != "" && sheets.String != "null" {
			_ = json.Unmarshal([]byte(sheets.String), &d.Sheets)
		}
		count := storedCount
		if d.SourceKind == kindXLSX || d.SourceKind == kindCSV {
			if count > 0 {
				d.RowCount = &count
			}
		} else if count > 0 {
			d.PageCount = &count
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
	return redactSecrets(err.Error(), apiKey)
}

// keyQueryRe matches ?key= and &key= query values (any case) so a leaked
// Gemini URL never keeps the secret, even when it is not the configured key.
var keyQueryRe = regexp.MustCompile(`(?i)([?&]key=)[^&\s"'<>\\]+`)

func redactSecrets(msg, apiKey string) string {
	if apiKey != "" {
		msg = strings.ReplaceAll(msg, apiKey, "[redacted]")
	}
	msg = keyQueryRe.ReplaceAllString(msg, `${1}[redacted]`)
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return msg
}
