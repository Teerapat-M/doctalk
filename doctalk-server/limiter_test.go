package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGeminiRetryRetries5xxHonorsRetryAfter(t *testing.T) {
	var waits []time.Duration
	calls := 0
	err := geminiRetry{
		attempts: 3,
		budget:   time.Second,
		base:     10 * time.Millisecond,
		maxDelay: 40 * time.Millisecond,
		jitter:   func(d time.Duration) time.Duration { return d },
		sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}.do(context.Background(), func() (time.Duration, error) {
		calls++
		if calls < 3 {
			return 50 * time.Millisecond, errors.New("gemini: HTTP 503: UNAVAILABLE")
		}
		return -1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	if len(waits) != 2 {
		t.Fatalf("waits=%v", waits)
	}
	for _, w := range waits {
		if w < 50*time.Millisecond {
			t.Fatalf("Retry-After not honoured: %s", w)
		}
	}
}

func TestGeminiRetryDoesNotRetry4xx(t *testing.T) {
	calls := 0
	err := geminiRetry{
		attempts: 4,
		budget:   time.Second,
		sleep:    func(context.Context, time.Duration) error { return nil },
	}.do(context.Background(), func() (time.Duration, error) {
		calls++
		return -1, errors.New("gemini: HTTP 400: INVALID_ARGUMENT")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	if parseRetryAfter("") != 0 {
		t.Fatal("empty")
	}
	if parseRetryAfter("2") != 2*time.Second {
		t.Fatal("delta-seconds")
	}
	when := time.Now().Add(3 * time.Second).UTC().Format(time.RFC1123)
	d := parseRetryAfter(when)
	if d < 2*time.Second || d > 4*time.Second {
		t.Fatalf("http-date delay=%s", d)
	}
}

func TestGeminiHTTPRetryAfter(t *testing.T) {
	if geminiHTTPRetryAfter(429, "1") != time.Second {
		t.Fatal("429")
	}
	if geminiHTTPRetryAfter(503, "") != 0 {
		t.Fatal("503 without header still retryable")
	}
	if geminiHTTPRetryAfter(502, "0") != 0 {
		t.Fatal("502")
	}
	if geminiHTTPRetryAfter(400, "1") != -1 {
		t.Fatal("400 must not retry")
	}
}
