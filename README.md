# DocTalk

Ask questions about your PDFs. Go + Gin + Postgres/pgvector on the server, React + Vite on the client, Gemini for embeddings and answers.

This repo is a monorepo:

- `doctalk-server/` — HTTP API
- `doctalk-client/` — web UI

## Phase 2 (this PR)

- `POST /documents` — upload a PDF (10 MiB default cap, `%PDF-` magic sniff, not the filename)
- Files go through a `Storage` interface (`DiskStorage` for local dev; swap in Supabase Storage at deploy time)
- Per-page text extraction, then Thai-aware chunking (see below)
- Embeddings via `gemini-embedding-001` at **768** dims, batched, with 429 backoff
- Document status: `uploading` → `extracting` → `embedding` → `done`, or `failed` with an error message
- `GET /documents` and `GET /documents/:id` (status + chunk count)
- Gemini is mocked in tests; CI does not need `GEMINI_API_KEY`

### Thai-aware chunking

Thai has no spaces between words. DocTalk does **not** use a dictionary segmenter (those usually need ICU/CGO). Each PDF page is split into a **700-rune window with 120-rune overlap**. If a newline or space sits in the last 80 runes of a window, the cut moves there so mixed Thai/English prefers paragraph or word boundaries. Every chunk stores its 1-based `page_number` for citations in Phase 3.

### Embedding dimension

`chunks.embedding` is **`vector(768)`**. That matches `gemini-embedding-001` with `outputDimensionality=768`. Values are L2-normalized (required by Gemini for non-3072 dims). The API key is read from `GEMINI_API_KEY` only, sent as `x-goog-api-key`, and never logged or committed. The Gemini REST host is `GEMINI_BASE_URL` (default `https://generativelanguage.googleapis.com`).

## Local ports

Defaults avoid collisions with another project on 5433 / 5173 / 8080:

| Service | Env | Default |
| --- | --- | --- |
| Postgres (host) | `DB_PORT` | 5435 |
| API | `API_PORT` (then `PORT`) | 8081 |
| Vite | `VITE_PORT` | 5174 |
| Browser → API | `VITE_API_URL` | http://localhost:8081 |

Inside docker-compose, Postgres is still 5432 on the private network. `DB_PORT` / `API_PORT` only change the **host** bindings. The server prefers `API_PORT`, then `PORT` (Render), then 8081.

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
   set DATABASE_URL=postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable
   set API_PORT=8081
   set CORS_ORIGIN=http://localhost:5174
   set GEMINI_API_KEY=your-free-tier-key
   set GEMINI_BASE_URL=https://generativelanguage.googleapis.com
   cd doctalk-server && go run .
   ```

   Unix:

   ```
   export DATABASE_URL=postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable
   export API_PORT=8081
   export CORS_ORIGIN=http://localhost:5174
   export GEMINI_API_KEY=your-free-tier-key
   export GEMINI_BASE_URL=https://generativelanguage.googleapis.com
   cd doctalk-server && go run .
   ```

   The process binds to `0.0.0.0:$API_PORT` (or `$PORT`). Open http://localhost:8081/health

4. Run the client (Vite loads the repo-root `.env` via `envDir`):

   ```
   cd doctalk-client
   npm install
   npm run dev
   ```

   Vite listens on `VITE_PORT` (5174) and proxies `/health` and `/documents` to `VITE_API_URL`.

Use the Gemini **free tier**. Do not put the key in git or CI.

### Upload a PDF

```
curl -s -F file=@paper.pdf http://localhost:8081/documents
curl -s http://localhost:8081/documents
curl -s http://localhost:8081/documents/<id>
```

`POST` returns **202** and processing continues in the background. Poll `GET /documents/:id` until `status` is `done` or `failed`. Embed failures store Gemini's HTTP status and error message, with the API key stripped (including `?key=` query values).

### Smoke test (real Gemini key)

With the stack running (`docker compose up --build` or `go run .` plus Postgres) and `GEMINI_API_KEY` set in the environment or a repo-root `.env`:

```
bash doctalk-server/scripts/smoke.sh
```

The script uploads `testdata/english.pdf` and `testdata/thai.pdf`, polls until `done`/`failed`, then checks every chunk has `page_number` and `vector_dims(embedding) = 768`. It never prints the API key. Needs `curl`, `python3`, and either `psql` (`DATABASE_URL`) or `docker compose exec` to the `db` service. Exits non-zero on failure.

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

Server (skips DB integration tests unless `DATABASE_URL` is set):

```
cd doctalk-server && go test ./...
```

Client:

```
cd doctalk-client && npm test
```

CI runs both, with a `pgvector/pgvector:pg16` service for the server job. Gemini is always mocked.

## Roadmap

- **Phase 1:** health, migrations, local Postgres, CI. (done)
- **Phase 2:** PDF upload, text extraction, Thai-aware chunking, embedding. (this PR)
- **Phase 3:** ask endpoint with top-k retrieval, Gemini answers with citations, SSE streaming.
- **Phase 4:** chat UI.
- **Phase 5:** guest auth and rate limiting.
- **Phase 6:** deploy (Render server, Vercel client, Supabase Postgres, all free tiers).
