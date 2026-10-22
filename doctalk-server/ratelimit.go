package main

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateKind string

const (
	rateAsk    rateKind = "ask"
	rateQuiz   rateKind = "quiz"
	rateIngest rateKind = "ingest"
)

type rateLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	loc         *time.Location
	resetHour   int
	quizLimit   int
	askLimit    int
	ingestLimit int
	hits        map[string][]time.Time
}

func bangkokTZ() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}

func newRateLimiter(cfg config) *rateLimiter {
	quiz := cfg.QuizPerDay
	if quiz < 1 {
		quiz = defaultQuizPerDay
	}
	ask := cfg.AskPerMinute
	if ask < 1 {
		ask = defaultAskPerMinute
	}
	ingest := cfg.IngestPerMinute
	if ingest < 1 {
		ingest = defaultIngestPerMinute
	}
	hour := cfg.QuizResetHour
	if hour < 0 || hour > 23 {
		hour = defaultQuizResetHour
	}
	return &rateLimiter{
		now:         time.Now,
		loc:         bangkokTZ(),
		resetHour:   hour,
		quizLimit:   quiz,
		askLimit:    ask,
		ingestLimit: ingest,
		hits:        map[string][]time.Time{},
	}
}

func quizWindowStart(now time.Time, loc *time.Location, hour int) time.Time {
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
	if !local.Before(start) {
		return start
	}
	return start.Add(-24 * time.Hour)
}

func (l *rateLimiter) snapshot(kind rateKind, guest, ip string, now time.Time) (used, limit, retry int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peekLocked(kind, guest, ip, now)
}

func (l *rateLimiter) allow(kind rateKind, guest, ip string, now time.Time) (retry int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	used, limit, retry := l.peekLocked(kind, guest, ip, now)
	if used >= limit {
		return retry, false
	}
	l.recordLocked(kind, guest, ip, now)
	return 0, true
}

func (l *rateLimiter) peekLocked(kind rateKind, guest, ip string, now time.Time) (used, limit, retry int) {
	limit = l.limit(kind)
	gUsed, gRetry := l.countLocked(l.key(kind, "g", guest), kind, now)
	iUsed, iRetry := l.countLocked(l.key(kind, "ip", ip), kind, now)
	used = gUsed
	retry = gRetry
	if iUsed > used {
		used = iUsed
		retry = iRetry
	}
	return used, limit, retry
}

func (l *rateLimiter) recordLocked(kind rateKind, guest, ip string, now time.Time) {
	if guest != "" {
		k := l.key(kind, "g", guest)
		l.hits[k] = append(l.hits[k], now)
	}
	if ip != "" {
		k := l.key(kind, "ip", ip)
		l.hits[k] = append(l.hits[k], now)
	}
}

func (l *rateLimiter) countLocked(key string, kind rateKind, now time.Time) (used, retry int) {
	cutoff, resetAt := l.window(kind, now)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	l.hits[key] = kept
	used = len(kept)
	retry = int(resetAt.Sub(now).Seconds())
	if retry < 1 {
		retry = 1
	}
	return used, retry
}

func (l *rateLimiter) window(kind rateKind, now time.Time) (cutoff, resetAt time.Time) {
	if kind == rateQuiz {
		cutoff = quizWindowStart(now, l.loc, l.resetHour)
		return cutoff, cutoff.Add(24 * time.Hour)
	}
	cutoff = now.Add(-time.Minute)
	return cutoff, now.Add(time.Minute)
}

func (l *rateLimiter) limit(kind rateKind) int {
	switch kind {
	case rateQuiz:
		return l.quizLimit
	case rateIngest:
		return l.ingestLimit
	default:
		return l.askLimit
	}
}

func (l *rateLimiter) key(kind rateKind, scope, id string) string {
	if id == "" {
		id = "-"
	}
	return string(kind) + ":" + scope + ":" + id
}

func (s *server) rejectIfLimited(c *gin.Context, kind rateKind) bool {
	if s.limits == nil {
		return false
	}
	now := s.limits.now()
	retry, ok := s.limits.allow(kind, guestIDFrom(c), c.ClientIP(), now)
	if ok {
		return false
	}
	c.Header("Retry-After", strconv.Itoa(retry))
	c.JSON(429, gin.H{"error": "rate limit exceeded"})
	return true
}

type quotaBucketJSON struct {
	Used       int    `json:"used"`
	Remaining  int    `json:"remaining"`
	Limit      int    `json:"limit"`
	ResetAt    string `json:"reset_at,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

type quotaJSON struct {
	Quiz   quotaBucketJSON `json:"quiz"`
	Ask    quotaBucketJSON `json:"ask"`
	Ingest quotaBucketJSON `json:"ingest"`
}

func (s *server) getQuota(c *gin.Context) {
	if s.limits == nil {
		c.JSON(200, quotaJSON{})
		return
	}
	now := s.limits.now()
	guest := guestIDFrom(c)
	ip := c.ClientIP()
	quizStart := quizWindowStart(now, s.limits.loc, s.limits.resetHour)
	c.JSON(200, quotaJSON{
		Quiz:   s.quotaBucket(rateQuiz, guest, ip, now, quizStart.Add(24*time.Hour).UTC().Format(time.RFC3339)),
		Ask:    s.quotaBucket(rateAsk, guest, ip, now, ""),
		Ingest: s.quotaBucket(rateIngest, guest, ip, now, ""),
	})
}

func (s *server) quotaBucket(kind rateKind, guest, ip string, now time.Time, resetAt string) quotaBucketJSON {
	used, limit, retry := s.limits.snapshot(kind, guest, ip, now)
	if used > limit {
		used = limit
	}
	b := quotaBucketJSON{Used: used, Remaining: limit - used, Limit: limit, RetryAfter: retry}
	if kind == rateQuiz {
		b.ResetAt = resetAt
	}
	return b
}
