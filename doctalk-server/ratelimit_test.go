package main

import (
	"encoding/json"
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
	l := newRateLimiter(config{QuizPerDay: 5, AskPerMinute: 20, IngestPerMinute: 10, QuizResetHour: 7})
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
	l := newRateLimiter(config{QuizPerDay: 1, AskPerMinute: 1, IngestPerMinute: 1, QuizResetHour: 7})
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
	cfg := config{CORSOrigin: "*", AskPerMinute: 1, QuizPerDay: 5, IngestPerMinute: 10, QuizResetHour: 7}
	r := newRouter(&server{cfg: cfg, limits: newRateLimiter(cfg)})
	body := `{"question":"hi","document_ids":["00000000-0000-0000-0000-000000000001"]}`
	first := postAsk(r, body)
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("first ask should not 429, got %d %s", first.Code, first.Body.String())
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

func TestQuizAndQuota429Shape(t *testing.T) {
	cfg := config{CORSOrigin: "*", AskPerMinute: 20, QuizPerDay: 1, IngestPerMinute: 10, QuizResetHour: 7}
	s := &server{cfg: cfg, limits: newRateLimiter(cfg)}
	r := newRouter(s)
	body := `{"document_ids":["00000000-0000-0000-0000-000000000001"],"n":1,"mode":"from_columns"}`
	postQuiz := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/quiz", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	_ = postQuiz()
	w := postQuiz()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("quiz status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Error == "" {
		t.Fatalf("429 json=%s", w.Body.String())
	}

	qw := httptest.NewRecorder()
	r.ServeHTTP(qw, httptest.NewRequest(http.MethodGet, "/quota", nil))
	if qw.Code != http.StatusOK {
		t.Fatalf("quota status=%d", qw.Code)
	}
	var quota quotaJSON
	if err := json.Unmarshal(qw.Body.Bytes(), &quota); err != nil {
		t.Fatal(err)
	}
	if quota.Quiz.Limit != 1 || quota.Quiz.Remaining != 0 || quota.Quiz.Used != 1 {
		t.Fatalf("quiz quota=%+v", quota.Quiz)
	}
	if quota.Quiz.ResetAt == "" {
		t.Fatal("quiz reset_at missing")
	}
}
