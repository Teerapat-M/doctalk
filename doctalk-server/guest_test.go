package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testJar struct {
	cookie *http.Cookie
}

func (j *testJar) do(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	if j.cookie != nil {
		req.AddCookie(j.cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == guestCookieName {
			j.cookie = c
		}
	}
	return w
}

func TestGuestJWTRoundTrip(t *testing.T) {
	secret := "unit-test-secret"
	id := "guest-abc"
	tok, err := signGuestJWT(secret, id, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseGuestJWT(secret, tok)
	if err != nil || got != id {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := parseGuestJWT("other", tok); err == nil {
		t.Fatal("wrong secret should fail")
	}
	expired, err := signGuestJWT(secret, id, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseGuestJWT(secret, expired); err == nil {
		t.Fatal("expired token should fail")
	}
}

func TestGuestCookieIsHTTPOnly(t *testing.T) {
	r := newRouter(&server{cfg: config{CORSOrigin: "*", JWTSecret: "cookie-secret", GuestTTL: 24 * time.Hour}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/quota", nil))
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name != guestCookieName {
			continue
		}
		found = true
		if !c.HttpOnly {
			t.Fatal("guest cookie must be httpOnly")
		}
		if _, err := parseGuestJWT("cookie-secret", c.Value); err != nil {
			t.Fatalf("cookie jwt: %v", err)
		}
	}
	if !found {
		t.Fatalf("missing Set-Cookie: %s", w.Header().Get("Set-Cookie"))
	}
}

func TestGuestDocumentsAreIsolated(t *testing.T) {
	db := openTestDB(t)
	cfg := config{
		CORSOrigin:     "*",
		MaxUploadBytes: 1 << 20,
		JWTSecret:      "isolate-secret",
		GuestTTL:       24 * time.Hour,
	}
	s := &server{
		ping:    db,
		db:      db,
		cfg:     cfg,
		store:   newMemStorage(),
		embed:   &fakeEmbedder{},
		extract: stubExtract{pages: []pageText{{Number: 1, Text: "only for guest a"}}},
	}
	r := newRouter(s)
	a, b := &testJar{}, &testJar{}

	body, ctype := pdfForm(t, "a.pdf", []byte("%PDF-1.4\nfake"))
	req := httptest.NewRequest(http.MethodPost, "/documents", body)
	req.Header.Set("Content-Type", ctype)
	w := a.do(r, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("upload a status=%d body=%s", w.Code, w.Body.String())
	}
	var created documentJSON
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("upload json=%s", w.Body.String())
	}
	s.wg.Wait()

	w = b.do(r, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list b status=%d", w.Code)
	}
	var listB []documentJSON
	if err := json.Unmarshal(w.Body.Bytes(), &listB); err != nil {
		t.Fatal(err)
	}
	for _, d := range listB {
		if d.ID == created.ID {
			t.Fatal("guest B listed guest A's document")
		}
	}

	w = b.do(r, httptest.NewRequest(http.MethodGet, "/documents/"+created.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("guest B get status=%d body=%s", w.Code, w.Body.String())
	}

	w = a.do(r, httptest.NewRequest(http.MethodGet, "/documents/"+created.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("guest A get status=%d body=%s", w.Code, w.Body.String())
	}

	askBody := `{"question":"hi","document_ids":["` + created.ID + `"]}`
	req = httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(askBody))
	req.Header.Set("Content-Type", "application/json")
	s.chat = &fakeChat{deltas: []string{"nope"}}
	w = b.do(r, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("guest B ask status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGuestCleanupDeletesExpiredDocs(t *testing.T) {
	db := openTestDB(t)
	store := newMemStorage()
	ctx := context.Background()
	if err := store.Put(ctx, "old.pdf", strings.NewReader("pdf")); err != nil {
		t.Fatal(err)
	}
	var id string
	err := db.QueryRow(`
		INSERT INTO documents (filename, content_type, byte_size, status, storage_key, guest_id, created_at)
		VALUES ('old.pdf', 'application/pdf', 3, 'done', 'old.pdf', 'expired-guest', now() - interval '8 days')
		RETURNING id::text
	`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{db: db, store: store, cfg: config{GuestTTL: 7 * 24 * time.Hour}}
	n, err := s.cleanupExpiredGuests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cleaned=%d", n)
	}
	if _, err := store.Get(ctx, "old.pdf"); err == nil {
		t.Fatal("storage object should be gone")
	}
	var leftover int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id=$1`, id).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Fatal("row should be gone")
	}
}
