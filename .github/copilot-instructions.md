# vod-tender AI Coding Agent Instructions

## Project Overview

vod-tender is a Go + TypeScript service that automates Twitch VOD archival: discovers VODs via Helix API, downloads with yt-dlp, records live chat to Postgres, and optionally uploads to YouTube. Single-channel focus with job-based concurrency model.

## Architecture Essentials

### Component Boundaries

- **Backend** (Go): `main.go` orchestrates 5+ concurrent jobs (chat recorder, VOD processor, catalog backfiller, OAuth refreshers, HTTP server)
- **Frontend** (Vite+React+TypeScript): VOD browser + chat replay UI; API base URL auto-resolves via `frontend/src/lib/api.ts` (maps `vod-tender.<domain>` → `vod-api.<domain>`)
- **Database**: Postgres-only; schema in `backend/db/db.go` (idempotent migrations run on boot)
- **Storage**: Local filesystem (`DATA_DIR`) for downloaded videos; no object storage currently

### Data Flow (Critical Path)

1. **Catalog job** (`vod/catalog.go`): Paginates Helix `/videos` endpoint every 6h, inserts rows into `vods` table (idempotent `ON CONFLICT DO NOTHING`)
2. **Processing job** (`vod/processing.go`): Selects earliest unprocessed VOD, calls `downloadVOD()` (wrapper around yt-dlp subprocess), then `uploadToYouTube()` if credentials exist
3. **Auto chat** (`chat/auto.go`): Polls `/streams` endpoint every 30s; on live: inserts placeholder VOD `live-<unix>`, records chat; on offline: reconciles chat to real VOD ID via time-window matching (±10m)
4. **OAuth refreshers** (`oauth/refresh.go`): Jittered timers (5-15m Twitch, 10-20m YT) proactively renew tokens stored in `oauth_tokens` table

### State Management Patterns

- **Circuit breaker** (`vod/processing.go`): Uses `kv` table (`circuit_state`, `circuit_failures`, `circuit_open_until`). Threshold reached → skip processing for cooldown period. Reset on success.
- **Download progress**: `download_state`, `download_bytes`/`download_total`, `progress_updated_at` columns in `vods`; updated via stderr regex parsing of yt-dlp output
- **Cancellation**: Active downloads tracked in `downloadCancel` map; `POST /admin/download/<id>/cancel` triggers context cancellation

## Critical Workflows

### Docker Compose Development (Primary Workflow)

**All development uses Docker Compose.** The stack includes Postgres, API (Go backend), and frontend (Vite).

```bash
# Setup: Fill backend/.env with TWITCH_* credentials
make up           # docker compose up -d --build (starts all services)
make logs-backend # tail API logs
make logs-frontend # tail frontend logs
make ps           # list running services
make down         # stop and remove containers
```

**Environment files:**

- `backend/.env` → backend container env vars (Twitch/YouTube credentials, feature toggles)
- Root `.env` (optional) → compose-level params (`STACK_NAME`, `API_PORT`, `FRONTEND_PORT`, `DB_NAME`)

**Database access:**

```bash
make db-reset     # DROP/CREATE database (uses container name from STACK_NAME)
docker compose exec postgres psql -U vod -d vod  # interactive SQL
```

### Testing

- **Unit tests**: Run inside containers with Postgres; use `docker compose exec api go test ./...` OR set `TEST_PG_DSN` and test locally
- **CI**: `.github/workflows/ci.yml` runs 3 jobs: `gitleaks` (secrets), `govulncheck` (Go vulns), `build-test-lint` (builds, tests, race detector on PRs, golangci-lint, Docker builds, Trivy scans)
- **Test structure**: `processing_test.go` uses interface mocks (`Downloader`, `Uploader`); assign to `vod.downloader`/`vod.uploader` globals for deterministic tests
- **Local test Postgres**: If testing outside containers, set `TEST_PG_DSN=postgres://vod:vod@localhost:5432/vod_test?sslmode=disable`

### Multi-Instance (Multi-Channel) Pattern

Run multiple isolated stacks (one per Twitch channel) by configuring root `.env`:

```bash
# Instance 1: Channel A
cp .env.example .env
# Edit .env: STACK_NAME=vod-channelA, TWITCH_CHANNEL=channelA, API_PORT=8080, FRONTEND_PORT=3000
make up

# Instance 2: Channel B (in separate directory or via env override)
# Edit .env: STACK_NAME=vod-channelB, TWITCH_CHANNEL=channelB, API_PORT=8081, FRONTEND_PORT=3001, DB_NAME=vod_channelb
make up
```

Each instance gets isolated Postgres DB (set unique `DB_NAME`), volumes, and ports. Share external `WEB_NETWORK` for reverse proxy routing.

### Code Quality Commands

```bash
make lint         # golangci-lint (runs in container or locally; config: .golangci.yml)
make lint-fix     # auto-fix linter issues
```

## Project-Specific Conventions

### Configuration

- **All env vars** documented in `docs/CONFIG.md`; no hardcoded secrets
- **Backend env**: `backend/.env` contains Twitch/YouTube credentials; mounted into `api` container
- **Container defaults**: `DB_DSN` → `postgres:5432` (service name), `DATA_DIR` → `/data` (volume mount)
- **Feature toggles**: `CHAT_AUTO_START=1` enables auto mode; else manual single-VOD chat

### Logging & Observability

- **Structured logging**: `slog` with `component=<name>` attribute (e.g., `component=vod_download`)
- **Correlation IDs**: `server/server.go` injects `X-Correlation-ID` header into request context; propagated to logs as `corr` field
- **Metrics**: Prometheus exposition at `/metrics` (`vod_downloads_started_total`, `vod_queue_depth` gauge, histograms for duration)
- **Status endpoint**: `/status` JSON includes circuit breaker state, queue depth, EMAs (moving averages stored in `kv` table: `avg_download_ms`, `avg_upload_ms`)

### Error Handling

- **Processing errors** stored in `vods.processing_error` column; retries gated by `PROCESSING_RETRY_COOLDOWN` (default 600s)
- **Exponential backoff**: `DOWNLOAD_MAX_ATTEMPTS=5`, `DOWNLOAD_BACKOFF_BASE=2s` (formula: `2^n * base + jitter`)
- **Graceful shutdown**: `context.Context` cancelled by SIGINT/SIGTERM; all jobs listen to `ctx.Done()`

### yt-dlp Integration

- **Resume support**: `--continue` flag; progress persisted to DB before subprocess exits
- **External downloader**: aria2c auto-enabled if present (`--external-downloader aria2c`)
- **Restricted VODs**: Subscriber-only/restricted VODs are treated as auth-required and skipped
- **Verbose mode**: Use `YTDLP_VERBOSE=1` only for short debugging sessions (can produce very noisy logs)

### Database Patterns

- **Idempotent inserts**: `INSERT ... ON CONFLICT DO NOTHING` (catalog backfill, VOD discovery)
- **Migrations**: `db.Migrate()` runs SQL string embedded in `db/db.go`; single-pass schema setup (no migration history tracking yet)
- **Token storage**: `oauth_tokens` table; `provider` column = `twitch` | `youtube`

### Frontend API Resolution

- **Build-time override**: `VITE_API_BASE_URL` in `.env` or build args
- **Runtime heuristic**: `vod-tender.<domain>` → `vod-api.<domain>` (see `frontend/src/lib/api.ts`)
- **Same-origin fallback**: Useful when reverse proxy (e.g., Caddy) routes API at `/api`

## Extensibility Points

- **Swap downloader**: Implement `vod.Downloader` interface, assign to `vod.downloader` global before job start (e.g., custom transcoder)
- **Swap uploader**: Implement `vod.Uploader` interface (e.g., S3 archival instead of YouTube)
- **Multi-channel support**: Wrap Helix client to parameterize channel; update catalog/processing jobs to iterate channels (requires schema changes: add `channel` column to `vods`)

## Key Files to Reference

- `docs/ARCHITECTURE.md`: Component diagram, data flow, circuit breaker logic
- `docs/CONFIG.md`: Exhaustive env var reference with defaults
- `docs/OPERATIONS.md`: Runbook (multi-instance, monitoring, common failures)
- `backend/vod/processing.go`: Core VOD download/upload pipeline + circuit breaker
- `backend/chat/auto.go`: Live polling + placeholder reconciliation logic
- `backend/db/db.go`: Schema migration SQL (single source of truth for table definitions)
- `.github/workflows/ci.yml`: Full CI pipeline (secrets scan, vuln check, lint, tests, Docker builds, Trivy scans)

## Common Pitfalls

- **Forgetting backend/.env**: Stack won't start properly without Twitch credentials in `backend/.env`; copy from `backend/.env.example`
- **Circuit breaker stuck open**: Check `/status` endpoint for `circuit_state`; manually reset via `docker compose exec postgres psql -U vod -d vod -c "UPDATE kv SET value='closed' WHERE key='circuit_state'"`
- **yt-dlp auth failures**: Subscriber-only/restricted Twitch VODs are not downloaded; confirm the source VOD is public/accessible
- **Multi-instance port conflicts**: Each instance needs unique `API_PORT`/`FRONTEND_PORT` in root `.env`; DB conflicts if sharing Postgres without unique `DB_NAME`
- **Container name collisions**: Different `STACK_NAME` values required when running multiple instances on same host
