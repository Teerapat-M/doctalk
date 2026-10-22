package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuizWindowStartBangkokBoundary(t *testing.T) {
	loc := bangkokTZ()
	before := time.Date(2026, 10, 22, 6, 59, 59, 0, loc)
	got := quizWindowStart(before, loc, 7)
	want := time.Date(2026, 10, 21, 7, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("06:59 window=%s want %s", got, want)
	}
	atReset := time.Date(2026, 10, 22, 7, 0, 0, 0, loc)
	got = quizWindowStart(atReset, loc, 7)
	want = time.Date(2026, 10, 22, 7, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("07:00 window=%s want %s", got, want)
	}
}

func TestQuizLimiterResetsAtBangkok0700(t *testing.T) {
	l := newRateLimiter(config{QuizPerDay: 5, QuizIPPerDay: 20, AskPerMinute: 20, IngestPerMinute: 10, QuizResetHour: 7})
	loc := l.loc
	now := time.Date(2026, 10, 22, 6, 30, 0, 0, loc)
	for i := 0; i < 5; i++ {
		if _, ok := l.allow(rateQuiz, "guest-a", "1.1.1.1", now); !ok {
			t.Fatalf("hit %d should be allowed", i+1)
		}
	}
	if retry, ok := l.allow(rateQuiz, "guest-a", "1.1.1.1", now); ok || retry < 1 {
		t.Fatalf("6th hit ok=%v retry=%d", ok, retry)
	}
	after := time.Date(2026, 10, 22, 7, 0, 0, 0, loc)
	if _, ok := l.allow(rateQuiz, "guest-a", "1.1.1.1", after); !ok {
		t.Fatal("should reset at 07:00 Asia/Bangkok")
	}
}

func TestLimiterGuestAndIPIndependentKeys(t *testing.T) {
	l := newRateLimiter(config{QuizPerDay: 1, QuizIPPerDay: 20, AskPerMinute: 1, IngestPerMinute: 1, QuizResetHour: 7})
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, l.loc)
	if _, ok := l.allow(rateAsk, "g1", "10.0.0.1", now); !ok {
		t.Fatal("first ask")
	}
	if _, ok := l.allow(rateAsk, "g1", "10.0.0.1", now); ok {
		t.Fatal("same guest+ip should be limited")
	}
	if _, ok := l.allow(rateAsk, "g2", "10.0.0.1", now); ok {
		t.Fatal("shared IP should be limited")
	}
	if _, ok := l.allow(rateAsk, "g2", "10.0.0.2", now); !ok {
		t.Fatal("other guest and IP should pass")
	}
}

func TestAskReturnsJSON429BeforeSSE(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "ask429.pdf")
	insertChunk(t, db, id, 0, 1, "Embeddings are 768 dimensional vectors.", unitVec(0))
	cfg := config{CORSOrigin: "*", AskPerMinute: 1, QuizPerDay: 5, QuizIPPerDay: 20, IngestPerMinute: 10, QuizResetHour: 7, AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute}
	s := &server{
		db:     db,
		cfg:    cfg,
		limits: newRateLimiter(cfg),
		embed:  &fakeEmbedder{query: unitVec(0)},
		chat:   &fakeChat{deltas: []string{"768"}},
	}
	r := newRouter(s)
	body := `{"question":"What dim?","document_ids":["` + id + `"]}`
	first := postAsk(r, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first ask status=%d body=%s", first.Code, first.Body.String())
	}
	if ct := first.Header().Get("Content-Type"); !strings.Contains(ct, "event-stream") {
		t.Fatalf("first content-type=%s", ct)
	}
	w := postAsk(r, body)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type=%s", ct)
	}
	if strings.Contains(w.Header().Get("Content-Type"), "event-stream") {
		t.Fatal("429 must not be SSE")
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	if payload.Error == "" {
		t.Fatalf("empty error body=%s", w.Body.String())
	}
}

func TestQuotaNotChargedOnValidationOrUpstreamFailure(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "askfail.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	cfg := config{CORSOrigin: "*", AskPerMinute: 1, QuizPerDay: 5, QuizIPPerDay: 20, AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute}
	s := &server{
		db:     db,
		cfg:    cfg,
		limits: newRateLimiter(cfg),
		embed:  &fakeEmbedder{query: unitVec(0)},
		chat:   &fakeChat{err: errString("gemini chat: HTTP 503 UNAVAILABLE")},
	}
	r := newRouter(s)

	bad := postAsk(r, `{"question":"","document_ids":["`+id+`"]}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("validation status=%d body=%s", bad.Code, bad.Body.String())
	}
	missing := postAsk(r, `{"question":"hi","document_ids":["00000000-0000-0000-0000-000000000099"]}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing doc status=%d body=%s", missing.Code, missing.Body.String())
	}

	fail := postAsk(r, `{"question":"hi","document_ids":["`+id+`"]}`)
	if fail.Code != http.StatusOK {
		t.Fatalf("upstream status=%d body=%s", fail.Code, fail.Body.String())
	}
	events := parseTestSSE(t, fail.Body.String())
	if events.err == "" {
		t.Fatalf("expected SSE error, body=%s", fail.Body.String())
	}

	s.chat = &fakeChat{deltas: []string{"ok"}}
	ok := postAsk(r, `{"question":"hi","document_ids":["`+id+`"]}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("retry after upstream fail status=%d body=%s", ok.Code, ok.Body.String())
	}
	if !strings.Contains(ok.Header().Get("Content-Type"), "event-stream") {
		t.Fatalf("content-type=%s", ok.Header().Get("Content-Type"))
	}
	limited := postAsk(r, `{"question":"hi","document_ids":["`+id+`"]}`)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("after success status=%d body=%s", limited.Code, limited.Body.String())
	}
}

func TestQuizQuotaIsolatedPerGuestAndPersists(t *testing.T) {
	db := openTestDB(t)
	const secret = "isolate-quiz-secret-not-for-prod"
	cfg := config{
		CORSOrigin:      "*",
		JWTSecret:       secret,
		GuestTTL:        24 * time.Hour,
		QuizPerDay:      1,
		QuizIPPerDay:    20,
		AskPerMinute:    20,
		IngestPerMinute: 10,
		QuizNMax:        20,
		AskTimeout:      time.Minute,
	}
	s := &server{
		db:     db,
		cfg:    cfg,
		limits: newRateLimiter(cfg),
		embed:  &fakeEmbedder{},
		quiz:   &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1), quizJSONFixture(t, 1, 1)}},
	}
	r := newRouter(s)
	a, b := &testJar{}, &testJar{}
	a.do(r, httptest.NewRequest(http.MethodGet, "/quota", nil))
	b.do(r, httptest.NewRequest(http.MethodGet, "/quota", nil))
	guestA, err := parseGuestJWT(secret, a.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	guestB, err := parseGuestJWT(secret, b.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	idA := insertGuestQuizDoc(t, db, "qa.xlsx", guestA)
	idB := insertGuestQuizDoc(t, db, "qb.xlsx", guestB)

	post := func(j *testJar, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(`{"document_ids":["`+id+`"],"n":1,"mode":"generate"}`))
		req.Header.Set("Content-Type", "application/json")
		return j.do(r, req)
	}
	if w := post(a, idA); w.Code != http.StatusOK {
		t.Fatalf("guest A first=%d body=%s", w.Code, w.Body.String())
	}
	if w := post(b, idB); w.Code != http.StatusOK {
		t.Fatalf("guest B first=%d body=%s", w.Code, w.Body.String())
	}
	if w := post(a, idA); w.Code != http.StatusTooManyRequests {
		t.Fatalf("guest A second=%d body=%s", w.Code, w.Body.String())
	}

	s.limits = newRateLimiter(cfg)
	qw := a.do(r, httptest.NewRequest(http.MethodGet, "/quota", nil))
	if qw.Code != http.StatusOK {
		t.Fatalf("quota status=%d", qw.Code)
	}
	var quota quotaJSON
	if err := json.Unmarshal(qw.Body.Bytes(), &quota); err != nil {
		t.Fatal(err)
	}
	if quota.Quiz.Limit != 1 || quota.Quiz.Used != 1 || quota.Quiz.Remaining != 0 {
		t.Fatalf("persisted quiz quota=%+v", quota.Quiz)
	}
	if quota.Quiz.ResetAt == "" {
		t.Fatal("quiz reset_at missing")
	}
}

func TestQuizNotChargedOnUpstreamFailure(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`DELETE FROM quiz_quota_hits`); err != nil {
		t.Fatal(err)
	}
	id := insertDoneDoc(t, db, "quizfail.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	cfg := config{CORSOrigin: "*", QuizPerDay: 1, QuizIPPerDay: 20, QuizNMax: 20, AskTimeout: time.Minute}
	s := &server{
		db:     db,
		cfg:    cfg,
		limits: newRateLimiter(cfg),
		quiz:   &fakeQuiz{errs: []error{errString("gemini quiz: HTTP 503 UNAVAILABLE")}},
	}
	r := newRouter(s)
	body := `{"document_ids":["` + id + `"],"n":1,"mode":"generate"}`
	req := httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	s.quiz = &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1)}}
	req = httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUnrecordRemovesOwnHitNotLatest(t *testing.T) {
	l := newRateLimiter(config{AskPerMinute: 10, QuizPerDay: 5, QuizIPPerDay: 20, IngestPerMinute: 10, QuizResetHour: 7})
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, l.loc)
	t1 := now
	t2 := now.Add(time.Millisecond)
	t3 := now.Add(2 * time.Millisecond)
	l.mu.Lock()
	l.recordLocked(rateAsk, "g", "203.0.113.10", t1)
	l.recordLocked(rateAsk, "g", "203.0.113.10", t2)
	l.recordLocked(rateAsk, "g", "203.0.113.10", t3)
	l.unrecordLocked(rateAsk, "g", "203.0.113.10", t2)
	used, _, _ := l.peekLocked(rateAsk, "g", "203.0.113.10", t3)
	hits := append([]time.Time(nil), l.hits[l.key(rateAsk, "g", "g")]...)
	l.mu.Unlock()
	if used != 2 {
		t.Fatalf("used=%d want 2 (own hit refunded, later hit kept)", used)
	}
	if len(hits) != 2 || !hits[0].Equal(t1) || !hits[1].Equal(t3) {
		t.Fatalf("remaining hits=%v want [%s %s]", hits, t1, t3)
	}
}

func TestQuizIPCapPersistsPerAddress(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`DELETE FROM quiz_quota_hits`); err != nil {
		t.Fatal(err)
	}
	const secret = "ip-quiz-secret-not-for-prod-32b"
	cfg := config{
		CORSOrigin:      "http://localhost:3917",
		JWTSecret:       secret,
		GuestTTL:        24 * time.Hour,
		QuizPerDay:      5,
		QuizIPPerDay:    1,
		AskPerMinute:    20,
		IngestPerMinute: 10,
		QuizNMax:        20,
		AskTimeout:      time.Minute,
	}
	s := &server{
		db:     db,
		cfg:    cfg,
		limits: newRateLimiter(cfg),
		embed:  &fakeEmbedder{},
		quiz:   &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1), quizJSONFixture(t, 1, 1)}},
	}
	r := newRouter(s)
	a, b := &testJar{}, &testJar{}
	a.do(r, httptest.NewRequest(http.MethodGet, "/quota", nil))
	b.do(r, httptest.NewRequest(http.MethodGet, "/quota", nil))
	guestA, err := parseGuestJWT(secret, a.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	guestB, err := parseGuestJWT(secret, b.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	idA := insertGuestQuizDoc(t, db, "ipa.xlsx", guestA)
	idB := insertGuestQuizDoc(t, db, "ipb.xlsx", guestB)

	post := func(j *testJar, id, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(`{"document_ids":["`+id+`"],"n":1,"mode":"generate"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":1234"
		return j.do(r, req)
	}
	if w := post(a, idA, "203.0.113.50"); w.Code != http.StatusOK {
		t.Fatalf("first ip=%d body=%s", w.Code, w.Body.String())
	}
	if w := post(b, idB, "203.0.113.50"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("shared ip second guest=%d body=%s", w.Code, w.Body.String())
	}

	s.limits = newRateLimiter(cfg)
	if w := post(b, idB, "203.0.113.50"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("persisted ip cap=%d body=%s", w.Code, w.Body.String())
	}
	if w := post(b, idB, "203.0.113.51"); w.Code != http.StatusOK {
		t.Fatalf("other ip=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSpoofedForwardedHeadersDoNotBypassIPLimits(t *testing.T) {
	db := openTestDB(t)
	t.Run("ask", func(t *testing.T) {
		id := insertDoneDoc(t, db, "ask-spoof.pdf")
		insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
		cfg := config{CORSOrigin: "http://localhost:3917", AskPerMinute: 1, QuizPerDay: 5, QuizIPPerDay: 20, AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute}
		s := &server{db: db, cfg: cfg, limits: newRateLimiter(cfg), embed: &fakeEmbedder{query: unitVec(0)}, chat: &fakeChat{deltas: []string{"ok"}}}
		r := newRouter(s)
		body := `{"question":"hi","document_ids":["` + id + `"]}`
		if w := postAskWithIPHeaders(r, body, "203.0.113.9", "198.51.100.9"); w.Code != http.StatusOK {
			t.Fatalf("first=%d body=%s", w.Code, w.Body.String())
		}
		w := postAskWithIPHeaders(r, body, "203.0.113.9", "198.51.100.9")
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("spoofed headers bypassed ask cap status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("quiz", func(t *testing.T) {
		if _, err := db.Exec(`DELETE FROM quiz_quota_hits`); err != nil {
			t.Fatal(err)
		}
		id := insertDoneDoc(t, db, "quiz-spoof.pdf")
		insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
		cfg := config{CORSOrigin: "http://localhost:3917", QuizPerDay: 5, QuizIPPerDay: 1, QuizNMax: 20, AskTimeout: time.Minute}
		s := &server{db: db, cfg: cfg, limits: newRateLimiter(cfg), quiz: &fakeQuiz{bodies: [][]byte{quizJSONFixture(t, 1, 1), quizJSONFixture(t, 1, 1)}}}
		r := newRouter(s)
		body := `{"document_ids":["` + id + `"],"n":1,"mode":"generate"}`
		if w := postQuizWithIPHeaders(r, body, "203.0.113.9", "198.51.100.9"); w.Code != http.StatusOK {
			t.Fatalf("first=%d body=%s", w.Code, w.Body.String())
		}
		w := postQuizWithIPHeaders(r, body, "203.0.113.9", "198.51.100.9")
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("spoofed headers bypassed quiz cap status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("ingest", func(t *testing.T) {
		cfg := config{CORSOrigin: "http://localhost:3917", MaxUploadBytes: 1 << 20, IngestPerMinute: 1, AskPerMinute: 20, QuizPerDay: 5, QuizIPPerDay: 20}
		s := &server{
			db: db, cfg: cfg, limits: newRateLimiter(cfg), store: newMemStorage(),
			embed: &fakeEmbedder{}, extract: stubExtract{pages: []pageText{{Number: 1, Text: "hello"}}},
		}
		r := newRouter(s)
		if w := postPDFWithIPHeaders(t, r, "203.0.113.9", "198.51.100.9"); w.Code != http.StatusAccepted {
			t.Fatalf("first=%d body=%s", w.Code, w.Body.String())
		}
		w := postPDFWithIPHeaders(t, r, "203.0.113.9", "198.51.100.9")
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("spoofed headers bypassed ingest cap status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestClientIPHeaderUsesTrueClientIPNotXFF(t *testing.T) {
	db := openTestDB(t)
	id := insertDoneDoc(t, db, "ask-header.pdf")
	insertChunk(t, db, id, 0, 1, "hello", unitVec(0))
	cfg := config{
		CORSOrigin: "http://localhost:3917", ClientIPHeader: "True-Client-IP",
		AskPerMinute: 1, QuizPerDay: 5, QuizIPPerDay: 20, AskTopK: 5, AskTopKMax: 20, AskTimeout: time.Minute,
	}
	s := &server{db: db, cfg: cfg, limits: newRateLimiter(cfg), embed: &fakeEmbedder{query: unitVec(0)}, chat: &fakeChat{deltas: []string{"ok", "ok"}}}
	r := newRouter(s)
	body := `{"question":"hi","document_ids":["` + id + `"]}`
	if w := postAskWithIPHeaders(r, body, "8.8.8.8", "10.0.0.1"); w.Code != http.StatusOK {
		t.Fatalf("first=%d body=%s", w.Code, w.Body.String())
	}
	if w := postAskWithIPHeaders(r, body, "9.9.9.9", "10.0.0.1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("XFF bypassed True-Client-IP cap status=%d body=%s", w.Code, w.Body.String())
	}
	if w := postAskWithIPHeaders(r, body, "9.9.9.9", "10.0.0.2"); w.Code != http.StatusOK {
		t.Fatalf("other True-Client-IP=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNewRouterRejectsMalformedTrustedProxies(t *testing.T) {
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("expected panic for malformed TRUSTED_PROXIES")
		}
		if !strings.Contains(fmt.Sprint(got), "TRUSTED_PROXIES") {
			t.Fatalf("panic=%v", got)
		}
	}()
	newRouter(&server{cfg: config{TrustedProxies: []string{"not-a-cidr"}}})
}

func postAskWithIPHeaders(r http.Handler, body, xff, trueClient string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", xff)
	req.Header.Set("True-Client-IP", trueClient)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postQuizWithIPHeaders(r http.Handler, body, xff, trueClient string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", xff)
	req.Header.Set("True-Client-IP", trueClient)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postPDFWithIPHeaders(t *testing.T, r http.Handler, xff, trueClient string) *httptest.ResponseRecorder {
	t.Helper()
	body, ctype := pdfForm(t, "spoof.pdf", []byte("%PDF-1.4\nfake"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("X-Forwarded-For", xff)
	req.Header.Set("True-Client-IP", trueClient)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func insertGuestQuizDoc(t *testing.T, db *sql.DB, filename, guest string) string {
	t.Helper()
	id := insertDoneDoc(t, db, filename)
	if _, err := db.Exec(`UPDATE documents SET guest_id=$2 WHERE id=$1`, id, guest); err != nil {
		t.Fatal(err)
	}
	insertChunk(t, db, id, 0, 1, "Embeddings are 768 dimensional vectors.", unitVec(0))
	return id
}
