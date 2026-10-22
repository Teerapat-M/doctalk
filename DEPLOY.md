# Deploy DocTalk (Supabase + Render + Vercel)

Config and dashboard steps only. Do not put secret values in git, CI logs, screenshots, or chat. Generate `JWT_SECRET` with `openssl rand -base64 32` and paste it in the dashboards.

Free-tier notes: Render web services spin down after 15 minutes idle; the next request is slow. Supabase free Postgres and the Gemini free tier have their own quotas.

Order: **Supabase → Render → Vercel**, then paste the Vercel origin back into Render `CORS_ORIGIN`.

---

## Environment variables

### Render (`doctalk-server`)

Secrets (Blueprint `sync: false` — Bell sets these in the Render Dashboard; never commit or echo the values):

| Name | Purpose | Example / how to get it |
| --- | --- | --- |
| `DATABASE_URL` | Postgres URI (pgvector). App runs migrations on boot. | Supabase **Session pooler** URI (see below) |
| `JWT_SECRET` | HS256 key for the `dt_guest` cookie. Must be ≥32 bytes. | `openssl rand -base64 32` |
| `GEMINI_API_KEY` | Gemini embeddings, ask, and quiz. | Google AI Studio free-tier key |
| `SUPABASE_URL` | Supabase API origin for Storage. | Project Settings → API → Project URL |
| `SUPABASE_SERVICE_ROLE_KEY` | Server-only Storage access (bypasses RLS). | Project Settings → API → `service_role` |
| `CORS_ORIGIN` | Exact Vercel origin(s), comma-separated. Credentials are always on; `*` is rejected at startup. | `https://<project>.vercel.app` |

Non-secrets (already set in `render.yaml` for production):

| Name | Purpose | Default in Blueprint |
| --- | --- | --- |
| `COOKIE_SAMESITE` | Cross-site cookie. Production must be `None`. | `None` |
| `COOKIE_SECURE` | Required `true` when SameSite is `None`. | `true` |
| `GIN_MODE` | Gin release logging. | `release` |
| `SUPABASE_BUCKET` | Private Storage bucket name. | `doctalk-uploads` |
| `STORAGE_DIR` | Disk fallback if Supabase keys are unset (Render disk is ephemeral). Production with `COOKIE_SAMESITE=None` requires Supabase. | `/data/uploads` |
| `CLIENT_IP_HEADER` | Edge client-IP header. Render overwrites `True-Client-IP`; `X-Forwarded-For` is not trusted by default. | `True-Client-IP` |
| `TRUSTED_PROXIES` | Optional CIDRs for Gin's `X-Forwarded-For` trust. Leave unset. `0.0.0.0/0` is rejected. | (unset) |
| `GUEST_TTL_HOURS` | Guest document TTL (`last_seen`). | `168` |
| `QUIZ_PER_DAY` | Per-guest daily quiz quota (Postgres, Asia/Bangkok). | `5` |
| `QUIZ_RESET_HOUR` | Quiz window reset hour in Asia/Bangkok. | `7` |
| `QUIZ_IP_PER_DAY` | Loose per-IP daily quiz cap (Postgres). | `20` |
| `ASK_PER_MINUTE` | In-memory `/ask` cap. | `20` |
| `INGEST_PER_MINUTE` | In-memory ingest cap. | `10` |
| `MAX_UPLOAD_BYTES` | Upload size cap. | `10485760` |
| `MAX_PAGES` | PDF page cap. | `50` |
| `MAX_ROWS` | Spreadsheet row cap. | `2000` |
| `GEMINI_BASE_URL` | Gemini REST host. | `https://generativelanguage.googleapis.com` |
| `GEMINI_EMBEDDING_MODEL` | Embedding model. | `gemini-embedding-001` |
| `GEMINI_EMBED_BATCH` | Texts per embedding request. | `16` |
| `GEMINI_CHAT_MODEL` | Ask model. | `gemini-3.8-flash` |
| `GEMINI_QUIZ_MODEL` | Quiz model; empty uses the chat model. | (unset) |
| `GEMINI_FALLBACK_MODEL` | Chat and quiz fallback after retries fail on 429/5xx. Embeddings keep the primary model and 768 dims. | `gemini-flash-lite-latest` |
| `GEMINI_CHAT_FALLBACK_MODEL` | Alias used only if `GEMINI_FALLBACK_MODEL` is unset. | (unset) |
| `GEMINI_MAX_IN_FLIGHT` | Concurrent Gemini slots. | `2` |
| `GEMINI_RETRY_ATTEMPTS` | Max Gemini attempts per model (bounded 1–8). | `3` |
| `GEMINI_RETRY_BUDGET_SECONDS` | Cap on total retry *sleep* between Gemini attempts (clamped to half of `ASK_TIMEOUT_SECONDS`). HTTP call time is separate and uses `GEMINI_TOTAL_TIMEOUT_SECONDS`. | `8` |
| `GEMINI_RETRY_BASE_DELAY_MS` | Exponential backoff start, plus jitter. Honours `Retry-After` when larger. | `500` |
| `GEMINI_RETRY_MAX_DELAY_MS` | Backoff cap. | `4000` |
| `GEMINI_REQUEST_TIMEOUT_SECONDS` | Idle / first-token timeout for Gemini streams; resets on each chunk. Values below 5s fail at startup. | `25` |
| `GEMINI_TOTAL_TIMEOUT_SECONDS` | Hard cap on one Gemini HTTP call (stream, quiz, embed). Must be ≥ idle timeout and must not exceed `ASK_TIMEOUT_SECONDS`. | `90` |
| `QUIZ_N_MAX` | Max quiz questions per request. | `20` |
| `ASK_TOP_K` | Default retrieval k. | `5` |
| `ASK_TOP_K_MAX` | Max retrieval k. | `20` |
| `ASK_TIMEOUT_SECONDS` | Outer `/ask` envelope (retrieve + Gemini calls + retry sleep + fallback). Must be ≥ `GEMINI_TOTAL_TIMEOUT_SECONDS`. Remaining time is shared, so a long primary stream can leave little room for retries or fallback. | `120` |

Render also injects `PORT`. The process binds `0.0.0.0:$PORT`. Do not set `API_PORT` in production (that override is for local compose).

The server **refuses to start** if `DATABASE_URL` or `JWT_SECRET` is missing, `JWT_SECRET` is shorter than 32 bytes, `COOKIE_SAMESITE` or `COOKIE_SECURE` is invalid, `COOKIE_SAMESITE=None` without `COOKIE_SECURE=true`, `CORS_ORIGIN` contains `*` or `http://` while SameSite is None+Secure, `QUIZ_RESET_HOUR` is out of 0–23, `TRUSTED_PROXIES` is malformed or `0.0.0.0/0`, production cookies lack Supabase, only one of `SUPABASE_URL` / `SUPABASE_SERVICE_ROLE_KEY` is set, a Gemini timeout is set below 5 seconds, `GEMINI_TOTAL_TIMEOUT_SECONDS` is below the idle timeout, or `GEMINI_TOTAL_TIMEOUT_SECONDS` exceeds `ASK_TIMEOUT_SECONDS`. Locally, missing Supabase logs a warning that disk storage is ephemeral on Render.

### Vercel (`doctalk-client`)

| Name | Purpose | Example |
| --- | --- | --- |
| `VITE_API_URL` | Render API origin baked in at **build** time. No trailing slash. Required in production so the browser does not call the Vercel origin. | `https://doctalk-server.onrender.com` |

No other client env vars. Do not put Gemini, JWT, database, or Supabase keys on Vercel.

### Local (not production)

See `.env.example`. Local cookies stay `COOKIE_SAMESITE=Lax` and `COOKIE_SECURE=false`. Vite is `3917` with `strictPort`; if you override `VITE_PORT`, set `CORS_ORIGIN` to both `http://localhost:<port>` and `http://127.0.0.1:<port>`.

---

## 1. Supabase

1. Open [https://supabase.com/dashboard](https://supabase.com/dashboard) and sign in.
2. **New project**. Region: **Southeast Asia (Singapore)** when listed, to sit next to Render `singapore`. Save the database password in a password manager (it is shown once).
3. Wait until the project is healthy.
4. **Database → Extensions**. Find **vector** and enable it.
5. **SQL Editor → New query**. Paste `supabase/migrations/20261022120000_enable_pgvector.sql` and run it (`CREATE EXTENSION IF NOT EXISTS vector`).
6. **SQL Editor → New query**. Paste `supabase/migrations/20261022120100_storage_bucket.sql` and run it (private bucket `doctalk-uploads`, 10 MiB).
   - Or **Storage → New bucket**: name `doctalk-uploads`, **Public bucket** off, file size limit 10 MB.
7. **Project Settings → API**:
   - Copy **Project URL** (this is `SUPABASE_URL`).
   - Copy the **`service_role`** key (`SUPABASE_SERVICE_ROLE_KEY`). Do not copy the `anon` key for the API. Do not paste either key into git or the Vercel project.
8. **Project Settings → Database → Connection string**. Choose **URI** and **Session pooler** (port **5432**, not Transaction/6543). Copy it, replace `[YOUR-PASSWORD]` with the password from step 2, keep `sslmode=require`. That string is `DATABASE_URL`. Session mode works with pgx and the quiz quota advisory locks.

The Go service still applies `doctalk-server/migrations/*.sql` on boot (documents, chunks, guests, quiz quota). The SQL files under `supabase/migrations/` are only the dashboard/pgvector + bucket steps.

---

## 2. Render (Blueprint)

1. Push this repo to GitHub (already done for DocTalk).
2. Open [https://dashboard.render.com](https://dashboard.render.com) → **New +** → **Blueprint**.
3. Connect the GitHub repo. Render reads root `render.yaml`.
4. Confirm service **doctalk-server**: **Free**, region **Singapore**, health check `/health`, Docker from `doctalk-server/Dockerfile`.
5. When prompted for `sync: false` values, paste (do not screenshot):
   - `DATABASE_URL` — Session pooler URI from Supabase
   - `JWT_SECRET` — output of `openssl rand -base64 32` (not the `.env.example` placeholder)
   - `GEMINI_API_KEY` — AI Studio key
   - `SUPABASE_URL` — Project URL
   - `SUPABASE_SERVICE_ROLE_KEY` — `service_role`
   - `CORS_ORIGIN` — leave a placeholder such as `https://localhost` if Vercel does not exist yet; you will replace it in step 3 after Vercel gives you a URL
6. Apply the Blueprint. Wait until the deploy is **Live** and `GET https://<service>.onrender.com/health` returns `{"status":"ok",...}`.
7. Copy the service URL (`https://<name>.onrender.com`). That is `VITE_API_URL` for Vercel. No trailing slash.

`COOKIE_SAMESITE=None` and `COOKIE_SECURE=true` are already in `render.yaml` so the `dt_guest` cookie works from Vercel to Render.

---

## 3. Vercel, then CORS

1. Open [https://vercel.com/new](https://vercel.com/new) and import the same GitHub repo.
2. **Root Directory**: `doctalk-client` (Edit / Configure Project). Framework: **Vite**. Build `npm run build`, output `dist`. SPA fallback is `doctalk-client/vercel.json` (`/(.*) → /index.html`).
3. **Environment Variables** → Production (and Preview if you will use preview URLs):
   - Name: `VITE_API_URL`
   - Value: the Render URL from step 2 (example shape `https://doctalk-server.onrender.com`)
   - `VITE_API_URL` is compile-time. Changing it later needs **Deployments → … → Redeploy**.
4. Deploy. Copy the Production origin (`https://<project>.vercel.app`), no trailing slash.
5. Back on Render → **doctalk-server** → **Environment** → set `CORS_ORIGIN` to that Vercel origin (comma-separate extra preview origins if you need them). Save and deploy (env-only is enough).
6. Open the Vercel URL, upload a PDF, and confirm a guest cookie is set (`dt_guest`, Secure, SameSite=None) and `/quota` works with credentials.

Do not use `CORS_ORIGIN=*`. The server exits on a wildcard because the client always sends `credentials: 'include'`.

---

## Checklist

- [ ] pgvector enabled; bucket `doctalk-uploads` is private
- [ ] Render health is `ok`
- [ ] Vercel `VITE_API_URL` is the Render origin
- [ ] Render `CORS_ORIGIN` is the Vercel origin (https, no trailing slash)
- [ ] `COOKIE_SAMESITE=None` and `COOKIE_SECURE=true` on Render
- [ ] Secrets exist only in dashboards, not in the repo
