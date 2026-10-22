package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type fakePinger struct {
	err error
}

func (f fakePinger) PingContext(context.Context) error {
	return f.err
}

func TestHealthOKWhenDBPings(t *testing.T) {
	t.Parallel()
	r := newRouter(&server{ping: fakePinger{}, cfg: config{CORSOrigin: "*"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeHealth(t, w)
	if got.Status != "ok" || got.Checks.Database != "ok" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestCORSAllowsLocalhostAndLoopback(t *testing.T) {
	r := newRouter(&server{ping: fakePinger{}, cfg: config{CORSOrigin: defaultCORSOrigin}})
	for _, origin := range []string{"http://localhost:3917", "http://127.0.0.1:3917"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/health", nil)
		req.Header.Set("Origin", origin)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("origin=%s status=%d", origin, w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("origin=%s allow=%q", origin, got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("origin=%s credentials=%q", origin, got)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "http://evil.example")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow=%q", got)
	}
}

func TestHealthUnhealthyWhenDBFails(t *testing.T) {
	t.Parallel()
	r := newRouter(&server{ping: fakePinger{err: errors.New("connection refused")}, cfg: config{CORSOrigin: "*"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	got := decodeHealth(t, w)
	if got.Status != "unhealthy" || got.Checks.Database != "unreachable" {
		t.Fatalf("payload = %+v", got)
	}
}

type healthPayload struct {
	Status string `json:"status"`
	Checks struct {
		Database string `json:"database"`
	} `json:"checks"`
}

func decodeHealth(t *testing.T, w *httptest.ResponseRecorder) healthPayload {
	t.Helper()
	var got healthPayload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	return got
}
