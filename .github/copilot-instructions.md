## Copilot instructions for vod-tender

Purpose: Small Go service that discovers Twitch VODs, downloads via yt-dlp, records chat, and optionally uploads to YouTube; ships a minimal HTTP API and a React/Vite frontend.

Big picture (where things live)

- Backend entrypoint: `backend/main.go` (loads env, migrates Postgres, starts background jobs and HTTP server).
- Core jobs in `backend/vod/`: catalog backfill (`catalog.go`), processing loop (`processing.go`), helpers/model (`vod.go`).
- Chat recorder: `backend/chat/` (manual and auto live poller in `auto.go`).
- HTTP API: `backend/server/server.go` (health/status/metrics, VOD + chat endpoints, OAuth flows).
- Twitch/YouTube clients: `backend/twitchapi/*`, `backend/youtubeapi/*`.
- OpenAPI spec: `backend/api/openapi.yaml` (keep in sync when adding endpoints).
- Frontend (React + Vite): `frontend/` (API base resolution in `src/lib/api.ts`).

How the system flows

- On start, jobs run as goroutines: VOD catalog backfill, processing loop, optional chat (manual or auto), OAuth token refreshers; HTTP server listens on `:8080`.
- Processing loop picks the next eligible VOD (unprocessed, priority desc, oldest first; retry cooldown honored), downloads via yt‑dlp, optionally uploads to YouTube, updates DB, and advances moving averages in `kv`.
- Auto chat: when live, inserts a placeholder VOD id, records chat; when real VOD appears, reconciles IDs and time shift.

Important conventions and patterns

- Configuration is ENV-first; for local dev `backend/.env` is auto-loaded by `godotenv` (see `docs/CONFIG.md`). Sensitive values are NOT exposed by `/config` (only a small whitelist backed by `kv` keys prefixed `cfg:`).
- Database is Postgres (pgx); `db.Migrate` is idempotent and called on boot. Key tables: `vods`, `chat_messages`, `oauth_tokens`, `kv` (cursors, circuit breaker, moving averages).
- Circuit breaker state lives in `kv` (`circuit_state`, `circuit_failures`, `circuit_open_until`); processing loop respects/open/half‑open and resets on success.
- Download/Upload abstraction: `vod.Downloader` and `vod.Uploader` with globals `downloader`/`uploader`. Tests swap them; you can inject alternates similarly if extending behavior.
  - Example: `vod.downloader = customDownloader{}`; `vod.uploader = s3Uploader{...}` before starting the job.
- yt‑dlp uses `--continue` and parses stderr progress; set `YTDLP_COOKIES_PATH` for subscriber‑only VODs. ffmpeg is expected on PATH (Docker image includes yt‑dlp + ffmpeg).
- Frontend API base: `VITE_API_BASE_URL`, else map `vod-tender.<domain>` → `https://vod-api.<domain>`, else same‑origin (`src/lib/api.ts`).

Developer workflows (commands run from repo root unless noted)

- Docker Compose dev stack (Postgres + API + Frontend + Backup): create the shared network once, then up:
  - Create network: `docker network create web` (no-op if exists)
  - Start: `docker compose up -d --build`
  - Logs: `docker compose logs -f api`
- Run backend locally (requires Postgres and yt‑dlp/ffmpeg in PATH):
  - From `backend/`: `go run .` (loads `backend/.env` if present)
- Run frontend locally:
  - From `frontend/`: `npm ci && echo "VITE_API_BASE_URL=http://localhost:8080" > .env.local && npm run dev`
- Tests (require a Postgres DSN): set `TEST_PG_DSN`, then from `backend/`: `go test ./...`
  - Unit tests mock `vod.Downloader/Uploader`; migration test skips if `TEST_PG_DSN` unset.

Adding or changing API

- Edit/add handlers in `backend/server/server.go` and wire routes in `NewMux`.
- Update `backend/api/openapi.yaml` and keep response shapes consistent (prefer empty arrays over nulls; see existing list endpoints).
- Frontend fetches via simple fetch wrappers; reuse `getApiBase()` and return JSON with stable field names used in `src/components/*`.

Operational notes (what agents should respect)

- Don’t print secrets or cookie contents in logs; verbose yt‑dlp logging is disabled when cookies are configured.
- After successful upload, local files are removed by default to save disk; retention is controlled via `RETAIN_KEEP_NEWER_THAN_DAYS` and `BACKFILL_AUTOCLEAN`.
- Health and monitoring: `/healthz`, `/status`, `/metrics` (Prometheus); correlation id `X-Correlation-ID` is echoed and logged.

Where to look when unsure

- Behavior toggles and defaults: `docs/CONFIG.md`.
- Architecture details and flows: `docs/ARCHITECTURE.md`.
- Operations/runbook, compose tips, and admin tasks: `docs/OPERATIONS.md`.
