package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type stubExtract struct {
	pages []pageText
	err   error
}

func (s stubExtract) ExtractPages([]byte) ([]pageText, error) {
	return s.pages, s.err
}

type fakeEmbedder struct {
	dim   int
	err   error
	calls int
	query []float32
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	dim := f.dim
	if dim == 0 {
		dim = EmbeddingDim
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, dim)
		v[0] = float32(i + 1)
		l2normalize(v)
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if len(f.query) > 0 {
		if f.err != nil {
			return nil, f.err
		}
		out := append([]float32(nil), f.query...)
		return out, nil
	}
	vecs, err := f.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, errString("empty query embed")
	}
	return vecs[0], nil
}

func pdfForm(t *testing.T, filename string, body []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestUploadRejectsNonPDFEvenIfNamedPdf(t *testing.T) {
	s := &server{
		cfg:     config{CORSOrigin: "*", MaxUploadBytes: 1 << 20},
		embed:   &fakeEmbedder{},
		store:   newMemStorage(),
		extract: stubExtract{pages: []pageText{{Number: 1, Text: "x"}}},
	}
	r := newRouter(s)
	body, ctype := pdfForm(t, "tricky.pdf", []byte("JPEG-looking-bytes"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadRejectsMacroEnabledXLSX(t *testing.T) {
	s := &server{cfg: config{CORSOrigin: "*", MaxUploadBytes: 1 << 20}, embed: &fakeEmbedder{}}
	r := newRouter(s)
	xlsm := zipWithFiles(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"xl/workbook.xml":     "<workbook/>",
		"xl/vbaProject.bin":   "macro",
	})
	body, ctype := pdfForm(t, "quiz.xlsm", xlsm)
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "macro-enabled") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadRejectsTooLarge(t *testing.T) {
	s := &server{
		cfg:     config{CORSOrigin: "*", MaxUploadBytes: 32},
		embed:   &fakeEmbedder{},
		store:   newMemStorage(),
		extract: stubExtract{},
	}
	r := newRouter(s)
	payload := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("a"), 64)...)
	body, ctype := pdfForm(t, "big.pdf", payload)
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadRequiresGeminiWhenNotMocked(t *testing.T) {
	s := &server{
		cfg:     config{CORSOrigin: "*", MaxUploadBytes: 1 << 20},
		store:   newMemStorage(),
		extract: stubExtract{},
	}
	r := newRouter(s)
	body, ctype := pdfForm(t, "a.pdf", []byte("%PDF-1.4\n"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "GEMINI_API_KEY=") {
		t.Fatal("must not echo env assignment")
	}
}

func TestUploadMissingFile(t *testing.T) {
	s := &server{cfg: config{CORSOrigin: "*", MaxUploadBytes: 1 << 20}, embed: &fakeEmbedder{}}
	r := newRouter(s)
	req := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDocumentIngestWithMockGemini(t *testing.T) {
	db := openTestDB(t)
	embed := &fakeEmbedder{}
	s := &server{
		ping:  db,
		db:    db,
		cfg:   config{CORSOrigin: "*", MaxUploadBytes: 1 << 20},
		store: newMemStorage(),
		embed: embed,
		extract: stubExtract{pages: []pageText{
			{Number: 1, Text: "หน้าหนึ่งของเอกสารภาษาไทยไม่มีช่องว่าง"},
			{Number: 2, Text: "page two still cites as 2"},
		}},
	}
	r := newRouter(s)

	body, ctype := pdfForm(t, "thai.pdf", []byte("%PDF-1.4\nfake"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("upload status=%d body=%s", w.Code, w.Body.String())
	}
	var created documentJSON
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("missing id")
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ingest timed out")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents/"+created.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
	}
	var got documentJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != statusDone {
		t.Fatalf("status=%s err=%v body=%s", got.Status, got.ErrorMessage, w.Body.String())
	}
	if got.ChunkCount != 2 || got.PageCount == nil || *got.PageCount != 2 {
		t.Fatalf("chunks=%d pages=%v", got.ChunkCount, got.PageCount)
	}
	if got.RowCount != nil {
		t.Fatalf("pdf row_count=%v", *got.RowCount)
	}
	if embed.calls < 1 {
		t.Fatal("embedder was not called")
	}

	var page int
	if err := db.QueryRow(`SELECT page_number FROM chunks WHERE document_id=$1 AND chunk_index=1`, created.ID).Scan(&page); err != nil {
		t.Fatal(err)
	}
	if page != 2 {
		t.Fatalf("chunk 1 page=%d want 2", page)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d", w.Code)
	}
	var list []documentJSON
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range list {
		if d.ID == created.ID {
			found = true
			if d.ChunkCount != 2 {
				t.Fatalf("list chunk_count=%d", d.ChunkCount)
			}
		}
	}
	if !found {
		t.Fatal("uploaded document missing from list")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents/00000000-0000-0000-0000-000000000000", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing doc status=%d", w.Code)
	}
}

func TestIngestFailsWhenTooManyPages(t *testing.T) {
	db := openTestDB(t)
	s := &server{
		ping:    db,
		db:      db,
		cfg:     config{CORSOrigin: "*", MaxUploadBytes: 1 << 20, MaxPages: 1},
		store:   newMemStorage(),
		embed:   &fakeEmbedder{},
		extract: stubExtract{pages: []pageText{{Number: 1, Text: "one"}, {Number: 2, Text: "two"}}},
	}
	r := newRouter(s)
	body, ctype := pdfForm(t, "big.pdf", []byte("%PDF-1.4\n"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created documentJSON
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	s.wg.Wait()
	got, err := s.loadDocument(context.Background(), created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != statusFailed || got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "too many pages") {
		t.Fatalf("got %+v", got)
	}
}

func TestIngestFailsWhenExtractEmpty(t *testing.T) {
	db := openTestDB(t)
	s := &server{
		ping:    db,
		db:      db,
		cfg:     config{CORSOrigin: "*", MaxUploadBytes: 1 << 20},
		store:   newMemStorage(),
		embed:   &fakeEmbedder{},
		extract: stubExtract{pages: []pageText{{Number: 1, Text: "   "}}},
	}
	r := newRouter(s)
	body, ctype := pdfForm(t, "empty.pdf", []byte("%PDF-1.4\n"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created documentJSON
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	s.wg.Wait()

	got, err := s.loadDocument(context.Background(), created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != statusFailed || got.ErrorMessage == nil {
		t.Fatalf("got %+v", got)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGetDocumentWithoutDBIs500(t *testing.T) {
	r := newRouter(&server{cfg: config{CORSOrigin: "*"}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents/00000000-0000-0000-0000-000000000001", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/documents/abc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status=%d", w.Code)
	}
}
