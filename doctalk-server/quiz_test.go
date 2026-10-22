package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeQuiz struct {
	bodies [][]byte
	errs   []error
	calls  int
}

func (f *fakeQuiz) GenerateJSON(context.Context, string, string) ([]byte, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.bodies) {
		return f.bodies[i], nil
	}
	if len(f.bodies) == 0 {
		return nil, fmt.Errorf("no quiz body")
	}
	return f.bodies[len(f.bodies)-1], nil
}

func TestParseQuizJSONAndRetry(t *testing.T) {
	chunks := []retrievedChunk{{
		ID: "c1", DocumentID: "d1", PageNumber: 3, Content: "Embeddings are 768-d.",
	}}
	raw := quizJSONFixture(t, 2, 1)
	got, err := parseQuizJSON(raw, chunks, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Correct != "A" || got[0].Citation.PageNumber != 3 {
		t.Fatalf("%+v", got)
	}

	s := &server{quiz: &fakeQuiz{bodies: [][]byte{[]byte("not-json"), raw}}}
	out, err := s.generateQuizFromChunks(context.Background(), chunks, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("after retry len=%d", len(out))
	}
	if s.quiz.(*fakeQuiz).calls != 2 {
		t.Fatalf("calls=%d want 2", s.quiz.(*fakeQuiz).calls)
	}
}

func TestBuildQuizFromColumns(t *testing.T) {
	chunks := []retrievedChunk{
		{ID: "1", DocumentID: "d", SheetName: "คำถาม", RowNumber: 2, Question: "เมืองหลวง?", Answer: "กรุงเทพมหานคร", Explanation: "เมืองหลวงของไทย", Content: "q"},
		{ID: "2", DocumentID: "d", SheetName: "คำถาม", RowNumber: 3, Question: "แถบกลาง?", Answer: "น้ำเงิน", Content: "q"},
		{ID: "3", DocumentID: "d", SheetName: "คำถาม", RowNumber: 4, Question: "สกุลเงิน?", Answer: "บาท", Content: "q"},
		{ID: "4", DocumentID: "d", SheetName: "คำถาม", RowNumber: 5, Question: "ดอยอินทนนท์?", Answer: "เชียงใหม่", Content: "q"},
	}
	got, err := buildQuizFromColumns(chunks, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("n=%d", len(got))
	}
	for _, q := range got {
		if q.Correct != "A" && q.Correct != "B" && q.Correct != "C" && q.Correct != "D" {
			t.Fatalf("correct=%s", q.Correct)
		}
		opts := []string{q.Options.A, q.Options.B, q.Options.C, q.Options.D}
		if q.Citation.SheetName != "คำถาม" || q.Citation.RowNumber < 2 {
			t.Fatalf("citation=%+v", q.Citation)
		}
		seen := map[string]struct{}{}
		for _, o := range opts {
			if o == "" {
				t.Fatalf("empty option %+v", q)
			}
			seen[o] = struct{}{}
		}
		if len(seen) != 4 {
			t.Fatalf("options not distinct: %+v", opts)
		}
		correctText := map[string]string{"A": q.Options.A, "B": q.Options.B, "C": q.Options.C, "D": q.Options.D}[q.Correct]
		if correctText == "" {
			t.Fatal("missing correct text")
		}
	}
	if _, err := buildQuizFromColumns(chunks[:2], 2); err == nil {
		t.Fatal("need 4 distinct answers")
	}
}

func postJSON(r http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestQuizCapsNAndRequiresDocs(t *testing.T) {
	r := newRouter(&server{
		cfg:   config{CORSOrigin: "*", QuizNMax: 20, AskTimeout: time.Minute},
		embed: &fakeEmbedder{},
		quiz:  &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1)}},
	})
	id := "00000000-0000-0000-0000-000000000001"
	w := postJSON(r, "/quiz", fmt.Sprintf(`{"document_ids":[%q],"n":99}`, id))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "capped") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	w = postJSON(r, "/quiz", `{"n":2}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing docs status=%d", w.Code)
	}

	w = postJSON(r, "/quiz", fmt.Sprintf(`{"document_ids":[%q],"mode":"nope"}`, id))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad mode status=%d", w.Code)
	}
}

func TestQuizRequiresGemini(t *testing.T) {
	r := newRouter(&server{cfg: config{CORSOrigin: "*"}})
	w := postJSON(r, "/quiz", `{"document_ids":["00000000-0000-0000-0000-000000000001"],"n":1}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestQuizAndIngestJSONValidation(t *testing.T) {
	r := newRouter(&server{cfg: config{CORSOrigin: "*", QuizNMax: 20}})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(`{"document_ids":["00000000-0000-0000-0000-000000000001"]}`)))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("quiz missing content-type status=%d", w.Code)
	}

	w = postJSON(r, "/quiz", `{"document_ids":["not-a-uuid"]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid document_id") {
		t.Fatalf("quiz malformed id status=%d body=%s", w.Code, w.Body.String())
	}

	w = postJSON(r, "/quiz", `{"document_ids":["00000000-0000-0000-0000-000000000001"]} extra`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("quiz trailing junk status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/documents/not-a-uuid/ingest", strings.NewReader(`{"sheet":"s","columns":{"question":"q"}}`)))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("ingest missing content-type status=%d", w.Code)
	}

	w = postJSON(r, "/documents/not-a-uuid/ingest", `{"sheet":"s","columns":{"question":"q"}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid document_id") {
		t.Fatalf("ingest malformed id status=%d body=%s", w.Code, w.Body.String())
	}

	w = postJSON(r, "/documents/00000000-0000-0000-0000-000000000001/ingest", `{"sheet":"s","columns":{"question":"q"}} extra`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ingest trailing junk status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPickDistractorsByEmbeddingSimilarity(t *testing.T) {
	blue := retrievedChunk{Question: "แถบกลาง?", Answer: "น้ำเงิน", Embedding: unitVec(0)}
	red := retrievedChunk{Question: "สีอื่น?", Answer: "แดง", Embedding: nearVec(0, 1)}
	white := retrievedChunk{Question: "สีธง?", Answer: "ขาว", Embedding: nearVec(0, 2)}
	green := retrievedChunk{Question: "ใบไม้?", Answer: "เขียว", Embedding: nearVec(0, 3)}
	baht := retrievedChunk{Question: "สกุลเงิน?", Answer: "บาท", Embedding: unitVec(100)}
	cnx := retrievedChunk{Question: "ดอย?", Answer: "เชียงใหม่", Embedding: unitVec(200)}
	got := pickDistractors([]retrievedChunk{blue, baht, cnx, red, white, green}, blue, 3)
	if len(got) != 3 {
		t.Fatalf("distractors=%v", got)
	}
	want := map[string]struct{}{"แดง": {}, "ขาว": {}, "เขียว": {}}
	for _, d := range got {
		if _, ok := want[d]; !ok {
			t.Fatalf("unrelated distractor %q in %v", d, got)
		}
	}
}

type fakeTypedQuiz struct {
	fakeQuiz
}

func (f *fakeTypedQuiz) GenerateTypedJSON(ctx context.Context, system, user string, schema map[string]any) ([]byte, error) {
	return f.GenerateJSON(ctx, system, user)
}

func TestGeminiSameTypeDistractors(t *testing.T) {
	chunks := []retrievedChunk{{
		ID: "1", DocumentID: "d", SheetName: "คำถาม", RowNumber: 2,
		Question: "สีของธงชาติไทยแถบกลางคือสีใด", Answer: "น้ำเงิน", Content: "q",
	}}
	quiz := &fakeTypedQuiz{fakeQuiz: fakeQuiz{bodies: [][]byte{[]byte(`{"items":[{"distractors":["แดง","ขาว","เขียว"]}]}`)}}}
	got, err := assembleQuizFromColumns(context.Background(), chunks, 1, quiz)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	opts := []string{got[0].Options.A, got[0].Options.B, got[0].Options.C, got[0].Options.D}
	seen := map[string]struct{}{}
	for _, o := range opts {
		seen[o] = struct{}{}
	}
	for _, want := range []string{"น้ำเงิน", "แดง", "ขาว", "เขียว"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing %q in %v", want, opts)
		}
	}
}

func nearVec(hot, extra int) []float32 {
	v := unitVec(hot)
	if extra >= 0 && extra < len(v) && extra != hot {
		v[extra] = 0.05
		l2normalize(v)
	}
	return v
}

func TestQuizUnknownDocumentIs404(t *testing.T) {
	db := openTestDB(t)
	r := newRouter(&server{
		db:    db,
		cfg:   config{CORSOrigin: "*", QuizNMax: 20, AskTimeout: time.Minute},
		quiz:  &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1)}},
		embed: &fakeEmbedder{},
		store: newMemStorage(),
	})
	w := postJSON(r, "/quiz", `{"document_ids":["00000000-0000-0000-0000-000000000099"],"n":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	w = postJSON(r, "/documents/00000000-0000-0000-0000-000000000099/ingest", `{"sheet":"s","columns":{"question":"q"}}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ingest status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestQuizGenerateAndFromColumnsHTTP(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "quiz.xlsx")
	if _, err := db.Exec(`UPDATE documents SET source_kind='xlsx' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	insertSheetChunk(t, db, id, 0, "คำถาม", 2, "Question: เมืองหลวง?\nAnswer: กรุงเทพมหานคร", "เมืองหลวงของประเทศไทยคือที่ใด", "กรุงเทพมหานคร", "เมืองหลวงของไทย", unitVec(0))
	insertSheetChunk(t, db, id, 1, "คำถาม", 3, "Question: สี?\nAnswer: น้ำเงิน", "สีของธงชาติไทยแถบกลางคือสีใด", "น้ำเงิน", "", unitVec(1))
	insertSheetChunk(t, db, id, 2, "คำถาม", 4, "Question: สกุลเงิน?\nAnswer: บาท", "สกุลเงินของประเทศไทยเรียกว่าอะไร", "บาท", "", unitVec(2))
	insertSheetChunk(t, db, id, 3, "คำถาม", 5, "Question: ดอย?\nAnswer: เชียงใหม่", "ดอยอินทนนท์ตั้งอยู่ในจังหวัดใด", "เชียงใหม่", "", unitVec(3))

	quiz := &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 2, 1)}}
	s := &server{
		db:    db,
		cfg:   config{CORSOrigin: "*", QuizNMax: 20, AskTimeout: time.Minute, GeminiAPIKey: "quiz-secret"},
		embed: &fakeEmbedder{},
		quiz:  quiz,
	}
	r := newRouter(s)

	payload, _ := json.Marshal(quizRequest{DocumentIDs: []string{id}, N: 2, Mode: "generate"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/quiz", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", w.Code, w.Body.String())
	}
	var gen quizResponse
	if err := json.Unmarshal(w.Body.Bytes(), &gen); err != nil {
		t.Fatal(err)
	}
	if len(gen.Questions) != 2 || gen.Questions[0].Citation.SheetName != "คำถาม" {
		t.Fatalf("generate %+v", gen)
	}
	if strings.Contains(w.Body.String(), "quiz-secret") {
		t.Fatal("key leaked")
	}

	payload, _ = json.Marshal(quizRequest{DocumentIDs: []string{id}, N: 4, Mode: "from_columns"})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/quiz", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("from_columns status=%d body=%s", w.Code, w.Body.String())
	}
	var cols quizResponse
	if err := json.Unmarshal(w.Body.Bytes(), &cols); err != nil {
		t.Fatal(err)
	}
	if len(cols.Questions) != 4 {
		t.Fatalf("from_columns n=%d", len(cols.Questions))
	}
	if cols.Questions[0].Citation.RowNumber < 2 {
		t.Fatalf("row citation=%+v", cols.Questions[0].Citation)
	}
}

func TestSpreadsheetUploadIngestAndAskCitation(t *testing.T) {
	db := openTestDB(t)
	embed := &fakeEmbedder{}
	chat := &fakeChat{deltas: []string{"กรุงเทพมหานคร"}}
	s := &server{
		ping:  db,
		db:    db,
		cfg:   config{CORSOrigin: "*", MaxUploadBytes: 1 << 20, AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute},
		store: newMemStorage(),
		embed: embed,
		chat:  chat,
		quiz:  &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1)}},
	}
	r := newRouter(s)

	xlsx := mustThaiXLSX(t)
	body, ctype := pdfForm(t, "thai.xlsx", xlsx)
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
	if created.Status != statusMapping || created.SourceKind != kindXLSX || len(created.Sheets) != 2 {
		t.Fatalf("upload %+v", created)
	}

	ingestBody, _ := json.Marshal(ingestRequest{
		Sheet: "คำถาม",
		Columns: columnMap{
			Question:    "คำถาม",
			Answer:      "คำตอบ",
			Explanation: "คำอธิบาย",
		},
	})
	w = httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/documents/"+created.ID+"/ingest", bytes.NewReader(ingestBody))
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, ireq)
	if w.Code != http.StatusAccepted {
		t.Fatalf("ingest status=%d body=%s", w.Code, w.Body.String())
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

	got, err := s.loadDocument(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != statusDone || got.ChunkCount != 4 {
		t.Fatalf("after ingest %+v", got)
	}
	if got.RowCount == nil || *got.RowCount != 4 {
		t.Fatalf("row_count=%v", got.RowCount)
	}
	if got.PageCount != nil {
		t.Fatalf("spreadsheet page_count=%v", *got.PageCount)
	}

	var sheet string
	var row int
	if err := db.QueryRow(`SELECT sheet_name, row_number FROM chunks WHERE document_id=$1 AND chunk_index=0`, created.ID).Scan(&sheet, &row); err != nil {
		t.Fatal(err)
	}
	if sheet != "คำถาม" || row != 2 {
		t.Fatalf("sheet=%s row=%d", sheet, row)
	}

	payload, _ := json.Marshal(askRequest{Question: "เมืองหลวงของไทยคือที่ใด", DocumentIDs: []string{created.ID}})
	w = httptest.NewRecorder()
	areq := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewReader(payload))
	areq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, areq)
	events := parseTestSSE(t, w.Body.String())
	if len(events.citations) < 1 {
		t.Fatalf("citations=%+v body=%s", events.citations, w.Body.String())
	}
	cit := events.citations[0]
	if cit.SheetName != "คำถาม" || cit.RowNumber < 2 || cit.PageNumber != 0 {
		t.Fatalf("excel citation=%+v", cit)
	}
}

func quizJSONFixture(t *testing.T, n, sourceIndex int) []byte {
	t.Helper()
	qs := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		qs[i] = map[string]any{
			"prompt":       fmt.Sprintf("Q%d?", i+1),
			"options":      map[string]string{"A": "a", "B": "b", "C": "c", "D": "d"},
			"correct":      "A",
			"explanation":  "because a",
			"source_index": sourceIndex,
		}
	}
	raw, err := json.Marshal(map[string]any{"questions": qs})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func insertSheetChunk(t *testing.T, db *sql.DB, docID string, idx int, sheet string, row int, content, q, a, e string, vec []float32) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
		INSERT INTO chunks (document_id, chunk_index, page_number, content, embedding, sheet_name, row_number, question, answer, explanation)
		VALUES ($1, $2, 0, $3, $4::vector, $5, $6, $7, $8, $9)
		RETURNING id::text
	`, docID, idx, content, vectorLiteral(vec), sheet, row, q, a, e).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
