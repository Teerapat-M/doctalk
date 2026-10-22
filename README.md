# DocTalk

Ask questions about your PDFs and spreadsheets. Go + Gin + Postgres/pgvector on the server, React + Vite on the client, Gemini for embeddings, answers, and quizzes.

This repo is a monorepo:

- `doctalk-server/` — HTTP API
- `doctalk-client/` — web UI

## Phase 2 — ingest

- `POST /documents` — upload a PDF, XLSX, or CSV (10 MiB default cap, magic-byte sniff, not the filename)
- Files go through a `Storage` interface (`DiskStorage` for local dev; swap in Supabase Storage at deploy time)
- Per-page text extraction, then Thai-aware chunking (see below)
- Embeddings via `gemini-embedding-001` at **768** dims, batched, with 429 backoff
- Document status: `uploading` → `extracting` → `embedding` → `done`, or `failed` with an error message
- `GET /documents` and `GET /documents/:id` (status + chunk count)
- Gemini is mocked in tests; CI does not need `GEMINI_API_KEY`

## Phase 3 — ask

`POST /ask` answers a question over chosen documents. The question is embedded with `gemini-embedding-001` (768-d, `RETRIEVAL_QUERY`), then pgvector does top-k cosine retrieval (`<=>`) scoped to those docs. The grounded prompt tells Gemini to answer only from the retrieved chunks, say the answer is not in the document when they do not cover the question, and reply in the question's language (Thai or English). The chat model is `GEMINI_CHAT_MODEL` (default `gemini-3.8-flash`, free tier).

### Request

```
POST /ask
Content-Type: application/json

{
  "question": "What dimensionality are the embeddings?",
  "document_ids": ["<uuid>", "<uuid>"],
  "k": 5
}
```

`k` is optional. If omitted, the server uses `ASK_TOP_K` (5). If present, it must be an integer from 1 through `ASK_TOP_K_MAX` (20); `0`, negatives, and values above the cap are **400**. `document_ids` must be UUIDs (**400** if malformed, **404** if unknown). `Content-Type` must be `application/json` (**415** otherwise). Trailing junk after the JSON object is **400**.

`ASK_TIMEOUT_SECONDS` (60) bounds the whole request. Client disconnect cancels the upstream Gemini call. Gemini 429/503 retries are capped (`GEMINI_RETRY_BUDGET_SECONDS` default 8, at most 3 attempts) well under that timeout; if they still fail, the client gets an SSE `error` with the redacted upstream reason (for example `HTTP 503 UNAVAILABLE`), not a generic timeout. Optional `GEMINI_CHAT_FALLBACK_MODEL` is tried **once** after the primary returns 503; leave it unset unless you have a working free-tier model.

Validation errors stay JSON **before** the stream (`400` / `404` / `409` / `415` / `503` if the API key is missing). Once the request is valid and the documents are ready, the response is **SSE** (`text/event-stream`). Query embedding, retrieval, and chat failures after that are SSE `error` events (including Gemini 429). Delta / final / error are the only stream events. A not-found answer (empty retrieval, or Gemini's `NOT_FOUND` first-line signal) sends `final` with `"citations":[]`.

### SSE events

Delta (token/chunk) events, then one final citations event:

```
event: delta
data: {"text":"The embeddings are 768-dimensional."}

event: final
data: {"citations":[{"document_id":"<uuid>","page_number":2,"chunk_id":"<uuid>","snippet":"…"}]}
```

Spreadsheet citations use `sheet_name` and `row_number` instead of `page_number`.

If an upstream call fails after the stream has started:

```
event: error
data: {"error":"gemini chat: HTTP 503: UNAVAILABLE: high demand"}
```

Errors keep Gemini's status and message with the API key stripped (including `?key=`). The key is sent only as `x-goog-api-key`, never as a query parameter. Concurrent `/ask` (and quiz / embed) calls share a bounded Gemini slot limiter (`GEMINI_MAX_IN_FLIGHT`, default 2) that is released before backoff sleeps. Gin runs in release mode unless `GIN_MODE` is set; trusted proxies default to none (`TRUSTED_PROXIES` comma-separated to override).

## Phase 3.5 — Excel import and quiz

Phases 1–3.5 landed in [PR #1](https://github.com/Teerapat-M/doctalk/pull/1)–[#5](https://github.com/Teerapat-M/doctalk/pull/5). This follow-up ([PR #6](https://github.com/Teerapat-M/doctalk/pull/6)) is QA on that stack.

### Excel / CSV ingest

`POST /documents` accepts `.xlsx` (ZIP `PK` magic plus `xl/` and `[Content_Types].xml`) and `.csv` (filename ends with `.csv`, UTF-8, no NUL). Macro-enabled workbooks (`xl/vbaProject.bin`) are **400**. Size uses the same `MAX_UPLOAD_BYTES` cap as PDFs.

Spreadsheet uploads return **202** with `status: "mapping"` and the workbook's sheets/headers. Processing does **not** start until the user maps columns:

```
POST /documents/<id>/ingest
Content-Type: application/json

{
  "sheet": "คำถาม",
  "columns": {
    "question": "คำถาม",
    "answer": "คำตอบ",
    "explanation": "คำอธิบาย"
  }
}
```

`Content-Type` must be `application/json` (**415** otherwise; compared case-insensitively, charset allowed). Trailing junk after the JSON object (including extra `}` or `]`) is **400**. A malformed document id is **400**; an unknown id is **404**. An unknown sheet or column is a synchronous **400**, not 202 then a later `failed` status.

Map **exactly one** of `columns.content` or `columns.question`. `answer` and `explanation` are optional. Each data row becomes a chunk with `sheet_name` and `row_number` (1-based, including the header row as 1) in place of `page_number`, embedded the same way as PDF chunks (`gemini-embedding-001` @ 768). `from_columns` quiz mode needs question **and** answer columns.

PDF documents expose `page_count` and omit `row_count`. Spreadsheets store the ingested row count in the same column, but the API exposes it as `row_count` and **omits** `page_count`.

Upload response (mapping):

```
{
  "id": "<uuid>",
  "filename": "thai.xlsx",
  "status": "mapping",
  "source_kind": "xlsx",
  "sheets": [
    {"name": "คำถาม", "headers": ["คำถาม", "คำตอบ", "คำอธิบาย"], "row_count": 4}
  ]
}
```

### Quiz

```
POST /quiz
Content-Type: application/json

{
  "document_ids": ["<uuid>"],
  "chunk_ids": [],
  "n": 5,
  "mode": "generate"
}
```

- `n` is optional. If omitted, the server uses 5. If present, it must be an integer from 1 through `QUIZ_N_MAX` (default 20, hard max 50); `0`, negatives, and values above the cap are **400**.
- `mode` is `generate` (default) or `from_columns`.
- `document_ids` must be UUIDs (**400** if malformed, **404** if unknown). `Content-Type` must be `application/json` (**415** otherwise). Trailing junk is **400**.
- `generate` calls Gemini `generateContent` with `responseMimeType: application/json` and a `responseSchema`, then validates server-side and retries **once** on invalid JSON.
- `from_columns` builds A/B/C/D options from ingested question/answer rows (needs at least 4 distinct answers unless Gemini fills same-type distractors). Distractors are the other-row answers closest in embedding space to the correct answer, so a flag-color question gets other colors rather than a random currency. If Gemini is configured, one extra `generateContent` call may replace those with same-type wrong answers when the JSON parses.
- Each question has options A–D, exactly one `correct` letter, a short `explanation`, and a citation (`page_number` or `sheet_name`+`row_number`).

```
{
  "questions": [
    {
      "prompt": "เมืองหลวงของประเทศไทยคือที่ใด",
      "options": {"A": "เชียงใหม่", "B": "กรุงเทพมหานคร", "C": "พัทยา", "D": "ขอนแก่น"},
      "correct": "B",
      "explanation": "กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย",
      "citation": {"document_id": "<uuid>", "sheet_name": "คำถาม", "row_number": 2, "chunk_id": "<uuid>", "snippet": "…"}
    }
  ]
}
```

Chat model: `GEMINI_CHAT_MODEL` (default `gemini-3.8-flash`). Quiz model: `GEMINI_QUIZ_MODEL` (defaults to the chat model). Tests mock Gemini via `GEMINI_BASE_URL` / fakes; CI does not need `GEMINI_API_KEY`.

## Phase 4 — chat UI

The Vite app (`doctalk-client`, port **4317**, `strictPort`) is the chat and quiz UI. Tokens live in `src/theme.css` from the Penpot **Doctalk** file (Core / Light / Dark, desktop 1440 and mobile 390). Frames **01–04** were empty in Penpot, so landing/workspace/error are assembled from those tokens. Frames **05a–c** (Excel upload / sheet & columns / importing), **06** (quiz setup), **07a–b** (taking / show answers), and **08a–d** (generating / empty / error / quota) drive the Excel and quiz UI, including Option Row, Toggle, Stepper, Column Mapper (Question / Answer / Explanation / Ignore), Skeleton, and Progress.

`VITE_API_URL` is optional; leave it unset so the browser talks to Vite and the proxy forwards `/health`, `/documents`, `/ask`, and `/quiz`. Guest quiz quota (5/day, reset 07:00 Asia/Bangkok) is stored in `localStorage` until Phase 5 auth.

Hash routes:

| Hash | Screen |
| --- | --- |
| `#/` | Landing + upload (PDF and xlsx/csv). xlsx stays on a sheet/column picker until `POST /documents/:id/ingest`. |
| `#/workspace/:id` | Workspace chat. `POST /ask` is read as SSE (`delta` text, then `final` citations, or `error`). Citations render as page or sheet/row chips with snippets. |
| `#/workspace/:id/quiz` or `#/quiz` | Quiz: pick source and `n`, A–D options, show-all or per-question answer toggles, explanation + source chip. |
| `#/not-found` | Missing document or unknown route. |
| `#/error` | Error state (including a redacted SSE `error` payload shown in the chat thread). |

Light/dark follows `prefers-color-scheme` and a header toggle (`localStorage` key `dt-theme`). Below 720px the document list is a drawer. Vitest covers the SSE parser (`ask.test.ts`), quiz toggles and Option Row states (`quizToggles.test.ts`), column picker roles (`columnPicker.test.ts`), and the guest quota helper (`quizQuota.test.ts`); `npm test` runs in CI.

The client quiz UI picks a source and question count, shows A–D, has a global and per-question answer-key toggle, and a regenerate button. Colors, type, spacing, and radius live in `doctalk-client/src/theme.css` as CSS variables from the Penpot **Doctalk** file (Core / Light / Dark). Ready sources show `page_count` for PDFs and `row_count` for spreadsheets.

### Thai-aware chunking

Thai has no spaces between words. DocTalk does **not** use a dictionary segmenter (those usually need ICU/CGO). Each PDF page is split into a **700-rune window with 120-rune overlap**. If a newline or space sits in the last 80 runes of a window, the cut moves there so mixed Thai/English prefers paragraph or word boundaries. Every chunk stores its 1-based `page_number` for citations in Phase 3.

### Embedding dimension

`chunks.embedding` is **`vector(768)`**. That matches `gemini-embedding-001` with `outputDimensionality=768`. Values are L2-normalized (required by Gemini for non-3072 dims). The API key is read from `GEMINI_API_KEY` only, sent as `x-goog-api-key`, and never logged or committed. The Gemini REST host is `GEMINI_BASE_URL` (default `https://generativelanguage.googleapis.com`).

## Local ports

Defaults avoid collisions with 5433 / 5173 / 8080 and with 8081 / 5174 / 5191 on the owner's PC:

| Service | Env | Default |
| --- | --- | --- |
| Postgres (host) | `DB_PORT` | 5435 |
| API | `API_PORT` (then `PORT`) | 8181 |
| Vite | `VITE_PORT` | 4317 (bound to `127.0.0.1`, `strictPort`) |
| Browser → API | `VITE_API_URL` | unset (Vite proxies `/health`, `/documents`, `/ask`, `/quiz`) |
| CORS | `CORS_ORIGIN` | `http://localhost:4317,http://127.0.0.1:4317` |

Inside docker-compose, Postgres is still 5432 on the private network. `DB_PORT` / `API_PORT` only change the **host** bindings. The server prefers `API_PORT`, then `PORT` (Render), then 8181. If Vite's port is taken, it exits with a message to set `VITE_PORT`.

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
   set API_PORT=8181
   set CORS_ORIGIN=http://localhost:4317,http://127.0.0.1:4317
   set GEMINI_API_KEY=your-free-tier-key
   set GEMINI_BASE_URL=https://generativelanguage.googleapis.com
   set GEMINI_CHAT_MODEL=gemini-3.8-flash
   set GEMINI_QUIZ_MODEL=gemini-3.8-flash
   cd doctalk-server && go run .
   ```

   Unix:

   ```
   export DATABASE_URL=postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable
   export API_PORT=8181
   export CORS_ORIGIN=http://localhost:4317,http://127.0.0.1:4317
   export GEMINI_API_KEY=your-free-tier-key
   export GEMINI_BASE_URL=https://generativelanguage.googleapis.com
   export GEMINI_CHAT_MODEL=gemini-3.8-flash
   export GEMINI_QUIZ_MODEL=gemini-3.8-flash
   cd doctalk-server && go run .
   ```

   The process binds to `0.0.0.0:$API_PORT` (or `$PORT`). Open http://localhost:8181/health

4. Run the client (Vite loads the repo-root `.env` via `envDir`):

   ```
   cd doctalk-client
   npm install
   npm run dev
   ```

   Vite listens on `VITE_PORT` (4317) at `127.0.0.1` and proxies `/health`, `/documents`, `/ask`, and `/quiz` to the API. Leave `VITE_API_URL` unset so the browser talks to Vite (same origin) instead of calling `localhost` while Vite prints `http://127.0.0.1:4317`. `CORS_ORIGIN` still allows both hostnames if the client is pointed at the API directly. Open http://127.0.0.1:4317 — landing, upload, workspace chat, and quiz are hash-routed (`#/`, `#/workspace/:id`, `#/quiz`).

Use the Gemini **free tier**. Do not put the key in git or CI.

### Upload a PDF or spreadsheet

```
curl -s -F file=@paper.pdf http://localhost:8181/documents
curl -s -F file=@quiz.xlsx http://localhost:8181/documents
curl -s -H "Content-Type: application/json" -d "{\"sheet\":\"คำถาม\",\"columns\":{\"question\":\"คำถาม\",\"answer\":\"คำตอบ\"}}" http://localhost:8181/documents/<id>/ingest
curl -s http://localhost:8181/documents
curl -s http://localhost:8181/documents/<id>
```

PDF `POST` returns **202** and processing continues in the background. Spreadsheets stay at `mapping` until `POST /documents/:id/ingest`. Poll `GET /documents/:id` until `status` is `done` or `failed`. Embed failures store Gemini's HTTP status and error message, with the API key stripped (including `?key=` query values).

### Ask

```
curl -N -sS -H "Content-Type: application/json" ^
  -d "{\"question\":\"What dimensionality are the embeddings?\",\"document_ids\":[\"<id>\"]}" ^
  http://localhost:8181/ask
```

Unix: drop the `^` line continuations.

### Quiz

```
curl -sS -H "Content-Type: application/json" ^
  -d "{\"document_ids\":[\"<id>\"],\"n\":5,\"mode\":\"generate\"}" ^
  http://localhost:8181/quiz
```

### Smoke test (real Gemini key)

Postgres with pgvector on **5435**, API on **8181**, no Docker for the server:

Windows cmd.exe (Postgres already listening on 5435):

```
set DATABASE_URL=postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable
set API_PORT=8181
set GEMINI_API_KEY=your-free-tier-key
set GEMINI_BASE_URL=https://generativelanguage.googleapis.com
set GEMINI_CHAT_MODEL=gemini-3.8-flash
set GEMINI_QUIZ_MODEL=gemini-3.8-flash
cd doctalk-server && go run .
```

Unix:

```
export DATABASE_URL=postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable
export API_PORT=8181
export GEMINI_API_KEY=your-free-tier-key
export GEMINI_BASE_URL=https://generativelanguage.googleapis.com
export GEMINI_CHAT_MODEL=gemini-3.8-flash
export GEMINI_QUIZ_MODEL=gemini-3.8-flash
cd doctalk-server && go run .
```

In another terminal, with the same `GEMINI_API_KEY` (or a repo-root `.env`):

```
bash doctalk-server/scripts/smoke.sh
```

The script uploads `testdata/english.pdf`, `testdata/thai.pdf`, `testdata/thai.xlsx`, and `testdata/thai-quiz.csv`, maps Excel/CSV columns, polls until `done`/`failed`, checks every chunk has a page or sheet/row citation and `vector_dims(embedding) = 768`, then `POST /ask` and `POST /quiz` (`generate` and `from_columns`). It never prints the API key. Needs `curl`, `python3`, and either `psql` (`DATABASE_URL`) or `docker compose exec` to the `db` service. Exits non-zero on failure.

`docker compose up --build` is still fine if you prefer containers.

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
- **Phase 2:** PDF upload, text extraction, Thai-aware chunking, embedding.
- **Phase 3:** ask endpoint with top-k retrieval, Gemini answers with citations, SSE streaming.
- **Phase 3.5:** Excel/CSV ingest, quiz generation, quiz UI. (done)
- **Phase 4:** chat UI. (this PR)
- **Phase 5:** guest auth and rate limiting.
- **Phase 6:** deploy (Render server, Vercel client, Supabase Postgres, all free tiers).
