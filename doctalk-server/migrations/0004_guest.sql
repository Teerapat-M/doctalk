ALTER TABLE documents ADD COLUMN IF NOT EXISTS guest_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS documents_guest_id_idx ON documents (guest_id);
CREATE INDEX IF NOT EXISTS documents_created_at_idx ON documents (created_at);
