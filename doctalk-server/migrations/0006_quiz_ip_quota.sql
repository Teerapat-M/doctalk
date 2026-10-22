ALTER TABLE quiz_quota_hits
    ADD COLUMN IF NOT EXISTS client_ip TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS quiz_quota_hits_ip_hit_idx ON quiz_quota_hits (client_ip, hit_at);
