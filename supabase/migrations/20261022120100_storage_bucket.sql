-- Private bucket for uploaded PDFs and spreadsheets.
-- The API uses SUPABASE_SERVICE_ROLE_KEY (bypasses RLS). Do not make this public.
INSERT INTO storage.buckets (id, name, public, file_size_limit)
VALUES ('doctalk-uploads', 'doctalk-uploads', false, 10485760)
ON CONFLICT (id) DO NOTHING;
