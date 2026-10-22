package main

import (
	"context"
	"time"
)

// slotLimiter is a bounded semaphore for Gemini HTTP calls. Acquire only
// around the request, never across backoff sleeps, so concurrent /ask
// handlers can wait out 429/503 without blocking each other.
type slotLimiter struct {
	slots chan struct{}
}

func newSlotLimiter(n int) *slotLimiter {
	if n < 1 {
		n = 1
	}
	return &slotLimiter{slots: make(chan struct{}, n)}
}

func (l *slotLimiter) acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *slotLimiter) release() {
	if l == nil {
		return
	}
	select {
	case <-l.slots:
	default:
	}
}

type geminiRetry struct {
	attempts int
	budget   time.Duration
	sleep    func(context.Context, time.Duration) error
	limiter  *slotLimiter
}

func (p geminiRetry) do(ctx context.Context, call func() (retryAfter time.Duration, err error)) error {
	attempts := p.attempts
	if attempts < 1 {
		attempts = 1
	}
	budget := p.budget
	if budget <= 0 {
		budget = defaultGeminiRetryBudget
	}
	sleep := p.sleep
	if sleep == nil {
		sleep = defaultSleep
	}
	deadline := time.Now().Add(budget)
	delay := 500 * time.Millisecond
	var last error
	for i := 0; i < attempts; i++ {
		if err := p.limiter.acquire(ctx); err != nil {
			return err
		}
		retryAfter, err := call()
		p.limiter.release()
		if err == nil {
			return nil
		}
		last = err
		if retryAfter < 0 {
			return err
		}
		if i == attempts-1 {
			break
		}
		wait := delay
		if retryAfter > wait {
			wait = retryAfter
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			break
		}
		if wait > remain {
			wait = remain
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
		if delay < 4*time.Second {
			delay *= 2
		}
	}
	return last
}
