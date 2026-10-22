-- Enable pgvector for chunk embeddings (768-d cosine retrieval).
-- On a new Supabase project, also enable "vector" under Database → Extensions
-- if this statement is denied for the SQL editor role.
CREATE EXTENSION IF NOT EXISTS vector;
