package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeChat struct {
	deltas    []string
	err       error
	block     bool
	gotQ      string
	gotChunks []retrievedChunk
	calls     int
	sleep     time.Duration
}

func (f *fakeChat) StreamAnswer(ctx context.Context, question string, chunks []retrievedChunk, emit func(string) error) error {
	f.calls++
	f.gotQ = question
	f.gotChunks = chunks
	if f.sleep > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.sleep):
		}
	}
	for _, d := range f.deltas {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(d); err != nil {
			return err
		}
	}
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func TestClampK(t *testing.T) {
	if got := clampK(0, 5, 20); got != 5 {
		t.Fatalf("default k=%d", got)
	}
	if got := clampK(3, 5, 20); got != 3 {
		t.Fatalf("override k=%d", got)
	}
	if got := clampK(99, 5, 20); got != 20 {
		t.Fatalf("capped k=%d", got)
	}
}

func TestCitationSnippet(t *testing.T) {
	got := citationSnippet("a\n\nb   c", 10)
	if got != "a b c" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("x", 10)
	if got := citationSnippet(long, 4); got != "xxxx…" {
		t.Fatalf("got %q", got)
	}
}

func TestAskRejectsEmptyQuestion(t *testing.T) {
	r := newRouter(&server{
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute},
		embed: &fakeEmbedder{},
		chat:  &fakeChat{},
	})
	w := httptest.NewRecorder()
	body := `{"question":"   ","document_ids":["00000000-0000-0000-0000-000000000001"]}`
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAskRequiresGemini(t *testing.T) {
	r := newRouter(&server{cfg: config{CORSOrigin: "*"}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(`{"question":"hi","document_ids":["x"]}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAskSSEWithMockGemini(t *testing.T) {
	db := openTestDB(t)
	idA := insertDoneDoc(t, db, "a.pdf")
	idB := insertDoneDoc(t, db, "b.pdf")
	vecA := unitVec(0)
	vecB := unitVec(1)
	chunkA := insertChunk(t, db, idA, 0, 1, "Embeddings are 768 dimensional vectors.", vecA)
	_ = insertChunk(t, db, idB, 0, 9, "unrelated other document", vecB)

	chat := &fakeChat{deltas: []string{"The ", "vectors are 768-d."}}
	embed := &fakeEmbedder{query: vecA}
	s := &server{
		ping:  db,
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute, GeminiAPIKey: "not-a-real-key"},
		embed: embed,
		chat:  chat,
	}
	r := newRouter(s)
	payload, _ := json.Marshal(askRequest{
		Question:    "What dimensionality are the embeddings?",
		DocumentIDs: []string{idA},
		K:           3,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%s", ct)
	}
	events := parseTestSSE(t, w.Body.String())
	if strings.Join(events.deltas, "") != "The vectors are 768-d." {
		t.Fatalf("deltas=%q", events.deltas)
	}
	if events.err != "" {
		t.Fatalf("sse error=%s", events.err)
	}
	if len(events.citations) != 1 {
		t.Fatalf("citations=%+v", events.citations)
	}
	cit := events.citations[0]
	if cit.DocumentID != idA || cit.ChunkID != chunkA || cit.PageNumber != 1 {
		t.Fatalf("citation=%+v", cit)
	}
	if !strings.Contains(cit.Snippet, "768") {
		t.Fatalf("snippet=%q", cit.Snippet)
	}
	if chat.gotQ != "What dimensionality are the embeddings?" {
		t.Fatalf("question=%q", chat.gotQ)
	}
	if len(chat.gotChunks) != 1 || chat.gotChunks[0].DocumentID != idA {
		t.Fatalf("retrieved %+v, leaked doc B?", chat.gotChunks)
	}
	if strings.Contains(w.Body.String(), "not-a-real-key") {
		t.Fatal("key leaked into SSE")
	}
}

func TestAskCapsKAndScopesToChosenDocs(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "many.pdf")
	q := unitVec(0)
	for i := 0; i < 4; i++ {
		v := unitVec(0)
		v[2] = float32(i) * 0.01
		l2normalize(v)
		insertChunk(t, db, id, i, i+1, strings.Repeat("chunk", i+1), v)
	}
	chat := &fakeChat{deltas: []string{"ok"}}
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 2, AskTimeout: time.Minute},
		embed: &fakeEmbedder{query: q},
		chat:  chat,
	}
	r := newRouter(s)
	payload, _ := json.Marshal(askRequest{Question: "q", DocumentIDs: []string{id}, K: 99})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	events := parseTestSSE(t, w.Body.String())
	if len(events.citations) != 2 {
		t.Fatalf("want k capped to 2, got %+v", events.citations)
	}
}

func TestAskMissingDocumentIs404(t *testing.T) {
	db := openTestDB(t)
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute},
		embed: &fakeEmbedder{},
		chat:  &fakeChat{},
	}
	r := newRouter(s)
	w := httptest.NewRecorder()
	body := `{"question":"hi","document_ids":["00000000-0000-0000-0000-000000000001"]}`
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(body)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAskEmptyRetrievalStreamsNotFound(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "empty-chunks.pdf")
	chat := &fakeChat{deltas: []string{"should not run"}}
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute},
		embed: &fakeEmbedder{query: unitVec(0)},
		chat:  chat,
	}
	r := newRouter(s)
	payload, _ := json.Marshal(askRequest{Question: "What is this about?", DocumentIDs: []string{id}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload)))
	events := parseTestSSE(t, w.Body.String())
	if chat.calls != 0 {
		t.Fatal("chat should not run when there are no chunks")
	}
	if strings.Join(events.deltas, "") != "The answer is not found in the document." {
		t.Fatalf("deltas=%q", events.deltas)
	}
	if len(events.citations) != 0 {
		t.Fatalf("citations=%+v", events.citations)
	}
}

func TestAskTimeoutBeforeStream(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "slow.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: 80 * time.Millisecond},
		embed: &fakeEmbedder{query: unitVec(0)},
		chat:  &fakeChat{sleep: time.Second, deltas: []string{"late"}},
	}
	r := newRouter(s)
	payload, _ := json.Marshal(askRequest{Question: "q", DocumentIDs: []string{id}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload)))
	body := w.Body.String()
	if !strings.Contains(body, "timed out") && !strings.Contains(body, "error") {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
	if strings.Contains(body, "late") {
		t.Fatalf("streamed after timeout: %s", body)
	}
}

func TestAskCancelStopsChat(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "cancel.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	chat := &fakeChat{block: true, deltas: []string{"hi"}}
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute},
		embed: &fakeEmbedder{query: unitVec(0)},
		chat:  chat,
	}
	r := newRouter(s)
	ctx, cancel := context.WithCancel(context.Background())
	payload, _ := json.Marshal(askRequest{Question: "q", DocumentIDs: []string{id}})
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload)).WithContext(ctx)
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if chat.calls < 1 {
		t.Fatal("chat was not called")
	}
}

func TestAskRedactsKeyInErrorEvent(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "fail.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	const secret = "live-ask-secret"
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute, GeminiAPIKey: secret},
		embed: &fakeEmbedder{query: unitVec(0)},
		chat:  &fakeChat{err: errors.New("gemini chat: HTTP 400 key=" + secret)},
	}
	r := newRouter(s)
	payload, _ := json.Marshal(askRequest{Question: "q", DocumentIDs: []string{id}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload)))
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("key leaked: %s", w.Body.String())
	}
	events := parseTestSSE(t, w.Body.String())
	if events.err == "" {
		t.Fatalf("expected error event, body=%s", w.Body.String())
	}
}

type parsedSSE struct {
	deltas    []string
	citations []citationJSON
	err       string
}

func parseTestSSE(t *testing.T, body string) parsedSSE {
	t.Helper()
	var out parsedSSE
	var event string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		switch event {
		case "delta":
			var d deltaEvent
			if err := json.Unmarshal([]byte(raw), &d); err != nil {
				t.Fatalf("delta json: %v %s", err, raw)
			}
			out.deltas = append(out.deltas, d.Text)
		case "final":
			var f finalEvent
			if err := json.Unmarshal([]byte(raw), &f); err != nil {
				t.Fatalf("final json: %v %s", err, raw)
			}
			out.citations = f.Citations
		case "error":
			var e sseErrorEvent
			if err := json.Unmarshal([]byte(raw), &e); err != nil {
				t.Fatalf("error json: %v %s", err, raw)
			}
			out.err = e.Error
		}
	}
	return out
}

func insertDoneDoc(t *testing.T, db *sql.DB, filename string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
		INSERT INTO documents (filename, content_type, byte_size, status, storage_key, page_count)
		VALUES ($1, 'application/pdf', 1, $2, $3, 1)
		RETURNING id::text
	`, filename, statusDone, filename+".pdf").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertChunk(t *testing.T, db *sql.DB, docID string, idx, page int, content string, vec []float32) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
		INSERT INTO chunks (document_id, chunk_index, page_number, content, embedding)
		VALUES ($1, $2, $3, $4, $5::vector)
		RETURNING id::text
	`, docID, idx, page, content, vectorLiteral(vec)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func unitVec(hot int) []float32 {
	v := make([]float32, EmbeddingDim)
	if hot < 0 || hot >= len(v) {
		hot = 0
	}
	v[hot] = 1
	return v
}
