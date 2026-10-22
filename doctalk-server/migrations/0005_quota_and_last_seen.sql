CREATE TABLE IF NOT EXISTS guests (
    id TEXT PRIMARY KEY,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS quiz_quota_hits (
    id BIGSERIAL PRIMARY KEY,
    guest_id TEXT NOT NULL,
    hit_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS quiz_quota_hits_guest_hit_idx ON quiz_quota_hits (guest_id, hit_at);
