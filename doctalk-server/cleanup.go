package main

import (
	"context"
	"log"
	"strconv"
	"time"
)

func (s *server) cleanupExpiredGuests(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	ttl := s.cfg.GuestTTL
	if ttl < time.Hour {
		ttl = defaultGuestTTL
	}
	interval := formatInterval(ttl)
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id::text, d.storage_key FROM documents d
		LEFT JOIN guests g ON g.id = d.guest_id
		WHERE d.guest_id <> ''
		  AND COALESCE(g.last_seen, d.created_at) < now() - ($1::text)::interval
	`, interval)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type row struct{ id, key string }
	var doomed []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key); err != nil {
			return 0, err
		}
		doomed = append(doomed, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, r := range doomed {
		if s.store != nil && r.key != "" {
			_ = s.store.Delete(ctx, r.key)
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id=$1`, r.id); err != nil {
			return 0, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM quiz_quota_hits
		WHERE guest_id IN (
			SELECT g.id FROM guests g
			WHERE g.last_seen < now() - ($1::text)::interval
		)
	`, interval); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM guests WHERE last_seen < now() - ($1::text)::interval
	`, interval); err != nil {
		return 0, err
	}
	return len(doomed), nil
}

func formatInterval(d time.Duration) string {
	h := int(d.Hours())
	if h < 1 {
		h = 1
	}
	return strconv.Itoa(h) + " hours"
}

func (s *server) startGuestCleanup() {
	if s.db == nil {
		return
	}
	go func() {
		s.runGuestCleanup()
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			s.runGuestCleanup()
		}
	}()
}

func (s *server) runGuestCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := s.cleanupExpiredGuests(ctx)
	if err != nil {
		log.Printf("guest cleanup: %v", err)
	} else if n > 0 {
		log.Printf("guest cleanup: removed %d documents", n)
	}
}
