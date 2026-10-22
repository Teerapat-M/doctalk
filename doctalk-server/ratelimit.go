package main

import (
	"context"
	"log"
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
	quizIPLimit int
	askLimit    int
	ingestLimit int
	hits        map[string][]time.Time
}

type quotaReservation struct {
	kind      rateKind
	guest     string
	ip        string
	now       time.Time
	quizHitID int64
	s         *server
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
	quizIP := cfg.QuizIPPerDay
	if quizIP < 1 {
		quizIP = defaultQuizIPPerDay
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
		quizIPLimit: quizIP,
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
	if kind == rateQuiz {
		if gUsed >= l.quizLimit {
			return gUsed, l.quizLimit, gRetry
		}
		if iUsed >= l.quizIPLimit {
			return iUsed, l.quizIPLimit, iRetry
		}
		return gUsed, l.quizLimit, gRetry
	}
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

func (l *rateLimiter) unrecordLocked(kind rateKind, guest, ip string, at time.Time) {
	drop := func(scope, id string) {
		if id == "" {
			return
		}
		k := l.key(kind, scope, id)
		hits := l.hits[k]
		for i := range hits {
			if hits[i].Equal(at) {
				l.hits[k] = append(hits[:i], hits[i+1:]...)
				return
			}
		}
	}
	drop("g", guest)
	drop("ip", ip)
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

func write429(c *gin.Context, retry int) {
	if retry < 1 {
		retry = 1
	}
	c.Header("Retry-After", strconv.Itoa(retry))
	c.JSON(429, gin.H{"error": "rate limit exceeded"})
}

func (r *quotaReservation) release() {
	if r == nil || r.s == nil || r.s.limits == nil {
		return
	}
	r.s.limits.mu.Lock()
	r.s.limits.unrecordLocked(r.kind, r.guest, r.ip, r.now)
	r.s.limits.mu.Unlock()
	if r.quizHitID > 0 && r.s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = r.s.db.ExecContext(ctx, `DELETE FROM quiz_quota_hits WHERE id=$1`, r.quizHitID)
		cancel()
	}
}

func (s *server) reserveQuota(c *gin.Context, kind rateKind) (res *quotaReservation, rejected bool) {
	if s.limits == nil {
		return nil, false
	}
	now := s.limits.now()
	guest := guestIDFrom(c)
	ip := c.ClientIP()
	retry, ok := s.peekQuota(c.Request.Context(), kind, guest, ip, now)
	if !ok {
		write429(c, retry)
		return nil, true
	}
	res, retry, ok = s.commitQuota(c.Request.Context(), kind, guest, ip, now)
	if !ok {
		write429(c, retry)
		return nil, true
	}
	return res, false
}

func (s *server) peekQuota(ctx context.Context, kind rateKind, guest, ip string, now time.Time) (retry int, ok bool) {
	l := s.limits
	if kind != rateQuiz {
		l.mu.Lock()
		used, limit, retry := l.peekLocked(kind, guest, ip, now)
		l.mu.Unlock()
		if used >= limit {
			return retry, false
		}
		return 0, true
	}
	gUsed, gRetry, err := s.quizGuestUsed(ctx, guest, now)
	if err != nil {
		log.Printf("quiz quota peek: %v", err)
		return 1, false
	}
	if gUsed >= l.quizLimit {
		return gRetry, false
	}
	ipUsed, ipRetry, err := s.quizIPUsed(ctx, ip, now)
	if err != nil {
		log.Printf("quiz ip quota peek: %v", err)
		return 1, false
	}
	if ipUsed >= l.quizIPLimit {
		return ipRetry, false
	}
	return 0, true
}

func (s *server) commitQuota(ctx context.Context, kind rateKind, guest, ip string, now time.Time) (*quotaReservation, int, bool) {
	res := &quotaReservation{kind: kind, guest: guest, ip: ip, now: now, s: s}
	if kind == rateQuiz && s.db != nil {
		id, retry, ok, err := s.insertQuizHit(ctx, guest, ip, now)
		if err != nil {
			log.Printf("quiz quota reserve: %v", err)
			return nil, 1, false
		}
		if !ok {
			return nil, retry, false
		}
		res.quizHitID = id
		return res, 0, true
	}
	s.limits.mu.Lock()
	s.limits.recordLocked(kind, guest, ip, now)
	s.limits.mu.Unlock()
	return res, 0, true
}

func (s *server) quizWindowRetry(now time.Time) (start time.Time, retry int) {
	start = quizWindowStart(now, s.limits.loc, s.limits.resetHour)
	retry = int(start.Add(24 * time.Hour).Sub(now).Seconds())
	if retry < 1 {
		retry = 1
	}
	return start, retry
}

func (s *server) quizGuestUsed(ctx context.Context, guest string, now time.Time) (used, retry int, err error) {
	if s.limits == nil {
		return 0, 1, nil
	}
	start, retry := s.quizWindowRetry(now)
	if s.db == nil {
		s.limits.mu.Lock()
		used, _ = s.limits.countLocked(s.limits.key(rateQuiz, "g", guest), rateQuiz, now)
		s.limits.mu.Unlock()
		return used, retry, nil
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quiz_quota_hits
		WHERE guest_id=$1 AND hit_at >= $2
	`, guest, start).Scan(&used)
	return used, retry, err
}

func (s *server) quizIPUsed(ctx context.Context, ip string, now time.Time) (used, retry int, err error) {
	if s.limits == nil {
		return 0, 1, nil
	}
	start, retry := s.quizWindowRetry(now)
	if s.db == nil {
		s.limits.mu.Lock()
		used, _ = s.limits.countLocked(s.limits.key(rateQuiz, "ip", ip), rateQuiz, now)
		s.limits.mu.Unlock()
		return used, retry, nil
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quiz_quota_hits
		WHERE client_ip=$1 AND hit_at >= $2
	`, ip, start).Scan(&used)
	return used, retry, err
}

func (s *server) insertQuizHit(ctx context.Context, guest, ip string, now time.Time) (id int64, retry int, ok bool, err error) {
	start, retry := s.quizWindowRetry(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, retry, false, err
	}
	defer tx.Rollback()
	gLock, ipLock := "quiz:"+guest, "quizip:"+ip
	if gLock > ipLock {
		gLock, ipLock = ipLock, gLock
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, gLock); err != nil {
		return 0, retry, false, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, ipLock); err != nil {
		return 0, retry, false, err
	}
	var used int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quiz_quota_hits WHERE guest_id=$1 AND hit_at >= $2
	`, guest, start).Scan(&used); err != nil {
		return 0, retry, false, err
	}
	if used >= s.limits.quizLimit {
		return 0, retry, false, nil
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quiz_quota_hits WHERE client_ip=$1 AND hit_at >= $2
	`, ip, start).Scan(&used); err != nil {
		return 0, retry, false, err
	}
	if used >= s.limits.quizIPLimit {
		return 0, retry, false, nil
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO quiz_quota_hits (guest_id, client_ip, hit_at) VALUES ($1, $2, $3) RETURNING id
	`, guest, ip, now).Scan(&id); err != nil {
		return 0, retry, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, retry, false, err
	}
	return id, 0, true, nil
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
		Quiz:   s.quizQuotaBucket(c.Request.Context(), guest, now, quizStart.Add(24*time.Hour).UTC().Format(time.RFC3339)),
		Ask:    s.memQuotaBucket(rateAsk, guest, ip, now, ""),
		Ingest: s.memQuotaBucket(rateIngest, guest, ip, now, ""),
	})
}

func (s *server) quizQuotaBucket(ctx context.Context, guest string, now time.Time, resetAt string) quotaBucketJSON {
	limit := s.limits.quizLimit
	used, retry, err := s.quizGuestUsed(ctx, guest, now)
	if err != nil {
		log.Printf("quota: %v", err)
	}
	if used > limit {
		used = limit
	}
	return quotaBucketJSON{Used: used, Remaining: limit - used, Limit: limit, ResetAt: resetAt, RetryAfter: retry}
}

func (s *server) memQuotaBucket(kind rateKind, guest, ip string, now time.Time, resetAt string) quotaBucketJSON {
	used, limit, retry := s.limits.snapshot(kind, guest, ip, now)
	if used > limit {
		used = limit
	}
	b := quotaBucketJSON{Used: used, Remaining: limit - used, Limit: limit, RetryAfter: retry}
	if resetAt != "" {
		b.ResetAt = resetAt
	}
	return b
}
