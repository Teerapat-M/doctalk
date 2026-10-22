# DocTalk

Ask questions about your PDFs. Go + Gin + Postgres/pgvector on the server, React + Vite on the client, Gemini for embeddings and answers.

This repo is a monorepo:

- `doctalk-server/` — HTTP API
- `doctalk-client/` — web UI

## Phase 1 (this PR)

- Server `/health` reports database connectivity
- SQL migrations: `vector` extension, `documents` and `chunks` tables
- Config from environment variables (no secrets in git)
- Local Postgres via docker compose (`pgvector/pgvector`)
- Client page that calls `/health`
- GitHub Actions CI that builds and tests both apps

### Embedding dimension

`chunks.embedding` is **`vector(768)`**.

That matches `gemini-embedding-001` with `outputDimensionality=768` (Matryoshka; also the native size of `text-embedding-004`). 768 is the storage-friendly size for free-tier Postgres (Phase 6: Supabase). The same model also supports 1536 and 3072; changing the column later needs a new migration.

## Setup

Prerequisites: Go 1.22+, Node 22+, Docker (for Postgres).

1. Copy `.env.example` and fill in local values. Never commit `.env`.

   ```
   copy .env.example .env
   ```

   Unix: `cp .env.example .env`

2. Start Postgres (and optionally the server container):

   ```
   docker compose up db
   ```

   Or the full stack: `docker compose up --build` (Makefile: `make compose-up`).

3. Run the API from the host (loads env from your shell, not from `.env` files):

   Windows cmd.exe:

   ```
   set DATABASE_URL=postgres://doctalk:doctalk@localhost:5432/doctalk?sslmode=disable
   set PORT=8080
   set CORS_ORIGIN=http://localhost:5173
   cd doctalk-server && go run .
   ```

   Unix:

   ```
   export DATABASE_URL=postgres://doctalk:doctalk@localhost:5432/doctalk?sslmode=disable
   export PORT=8080
   export CORS_ORIGIN=http://localhost:5173
   cd doctalk-server && go run .
   ```

   The process binds to `0.0.0.0:$PORT` (Render-compatible). Open http://localhost:8080/health

4. Run the client:

   ```
   cd doctalk-client
   npm install
   npm run dev
   ```

   Vite proxies `/health` to `http://localhost:8080`. To call the API by absolute URL instead, set `VITE_API_URL` (see `.env.example`).

`GEMINI_API_KEY` is unused in Phase 1. Add a real key locally before Phase 2; use the Gemini **free tier**. Do not put the key in git or CI.

## Makefile

Recipes are Windows `cmd.exe`-safe (ezwinports make).

```
make server-test
make server-build
make client-install
make client-test
make client-build
make compose-up
```

## Tests

Server (skips the migration integration test unless `DATABASE_URL` is set):

```
cd doctalk-server && go test ./...
```

Client:

```
cd doctalk-client && npm test
```

CI runs both, with a `pgvector/pgvector:pg16` service for the server job.

## Roadmap

- **Phase 2:** PDF upload, text extraction, Thai-aware chunking, embedding.
- **Phase 3:** ask endpoint with top-k retrieval, Gemini answers with citations, SSE streaming.
- **Phase 4:** chat UI.
- **Phase 5:** guest auth and rate limiting.
- **Phase 6:** deploy (Render server, Vercel client, Supabase Postgres, all free tiers).
