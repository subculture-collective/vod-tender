# vod-tender

Small Go service that discovers Twitch VODs for a channel, downloads them with yt-dlp, records live chat tied to VODs, and optionally uploads to YouTube. It ships with a minimal API and a small frontend for browsing VODs and chat replay.

## Quick start

- Copy `.env.example` to `backend/.env` and fill in values.
- Build and run locally:

```bash
make run
```

Or with Docker:

```bash
make docker-build
docker run --env-file backend/.env --rm vod-tender
```

### Docker Compose (server)

Project ships a `docker-compose.yml` with:

- Postgres (persistent volume)
- API (Go backend with yt-dlp + ffmpeg)
- Frontend (Vite build served by nginx)
- Backup service (daily pg_dump to a backup volume)

Basic ops:

```bash
# Ensure shared web network exists (used by Caddy too)
docker network create web 2>/dev/null || true

# Build & start
docker compose up -d --build

# Status
docker compose ps

# Tail logs
docker logs -f vod-api
```

## Configuration

Environment variables (place in `backend/.env` for local dev):

- TWITCH_CHANNEL
- TWITCH_BOT_USERNAME
- TWITCH_OAUTH_TOKEN
- TWITCH_CLIENT_ID (for Helix API discovery)
- TWITCH_CLIENT_SECRET (for Helix API discovery)
- TWITCH_VOD_ID (optional; default demo-vod-id)
- TWITCH_VOD_START (RFC3339; optional; default now)
- DB_DSN (optional; default postgres://vod:vod@postgres:5432/vod?sslmode=disable)
- DATA_DIR (optional; default data)
- LOG_LEVEL (optional; default info)
- LOG_FORMAT (optional; text|json; default text)

Chat recorder starts only when Twitch creds are present. Auto mode can start the recorder when the channel goes live (see `CHAT_AUTO_START`).

Full configuration reference and operational guidance:

- Architecture: `docs/ARCHITECTURE.md`
- Configuration: `docs/CONFIG.md`
- Operations / Runbook: `docs/OPERATIONS.md`

## End-to-end overview

1. Catalog backfill periodically discovers historical VODs using Twitch Helix and fills the `vods` table.
2. Processing job loops: selects next unprocessed VOD, downloads via `yt-dlp` (resumable), and uploads to YouTube if configured. Progress is written to DB and exposed via the API.
3. Chat recorder can run in two modes:

- Manual: provide `TWITCH_VOD_ID` and `TWITCH_VOD_START` and it will record chat for that VOD.
- Auto: set `CHAT_AUTO_START=1` and it will detect when the channel goes live, record under a placeholder id, and reconcile to the real VOD after it’s published.

See `docs/ARCHITECTURE.md` for a deeper flow description and diagrams.

## Configuration

All environment variables are documented in `docs/CONFIG.md` with defaults and tips. Key ones for a minimal run:

- TWITCH_CHANNEL, TWITCH_BOT_USERNAME, TWITCH_OAUTH_TOKEN (or store in DB via OAuth endpoints)
- TWITCH_CLIENT_ID, TWITCH_CLIENT_SECRET (Helix discovery / auto chat)
- DB_DSN (Postgres)
- DATA_DIR (download storage)
- LOG_LEVEL, LOG_FORMAT

## Cookies for subscriber-only/private VODs

yt-dlp needs Twitch cookies to authenticate for subscriber-only or otherwise private VODs.

- Export your browser cookies in Netscape format to `./secrets/twitch-cookies.txt`.
- Mount them in Docker Compose to `/run/cookies/twitch-cookies.txt` and set `YTDLP_COOKIES_PATH=/run/cookies/twitch-cookies.txt`.
- The downloader copies cookies to a private temp file (0600) before invoking `yt-dlp --cookies <temp>` and avoids verbose logs to protect secrets.
- Refresh cookies periodically; sessions expire.

## OAuth and tokens

- Twitch Chat: Provide `TWITCH_OAUTH_TOKEN` (format `oauth:xxxxx`) or obtain/store via `/auth/twitch/start` → `/auth/twitch/callback` endpoints. The token is saved to `oauth_tokens` with expiry and refreshed automatically.
- YouTube Upload: Provide `YT_CLIENT_ID`, `YT_CLIENT_SECRET`, `YT_REDIRECT_URI`, then visit `/auth/youtube/start` to authorize. The refresh token is stored and the uploader uses it automatically.

## Deployment

Docker Compose (bundled) is the simplest path for self-hosting:

- Postgres with a persistent volume
- API (Go backend) with yt-dlp and ffmpeg in the image
- Frontend built with Vite and served by nginx
- Optional daily backups via `pg_dump`

Routing: example Caddyfile assumes two hostnames, mapping to the shared `web` network. Ensure your reverse proxy attaches to the `web` network created by `docker network create web`.

For Kubernetes, map the same containers to Deployments and expose `/metrics` to Prometheus. Mount cookies as a Secret to the API pod and set `YTDLP_COOKIES_PATH` accordingly.

- Database: Postgres by default (see DB_DSN). Local docker-compose supplies a `postgres` service; override with your own DSN if needed.
- VOD processing job runs periodically (see configuration); discovery uses Twitch Helix if client id/secret provided.
- Downloader requires yt-dlp available in PATH.

  - Resumable downloads are enabled (yt-dlp --continue with infinite retries and fragment retries).
  - Optional: install aria2c for faster and more robust downloads.
  - ffmpeg is recommended for muxing and may be required by yt-dlp.

  ### Backups

  - Automatic: `vod-backup` runs `pg_dump` daily into volume `pgbackups`.
  - Manual one-off:

  ```bash
  docker compose run --rm backup sh -lc '/scripts/backup.sh /backups'
  ```

  - Copy backups to host:

  ```bash
  docker run --rm -v vod-tender_pgbackups:/src -v "$PWD":/dst alpine sh -lc 'cp -av /src/* /dst/'
  ```

  - Restore into running Postgres:

  ```bash
  zcat /path/to/vod_YYYYMMDD_HHMMSS.sql.gz | docker exec -i vod-postgres psql -U vod -d vod
  ```

  ### Caddy routing

  Routes assumed by the compose and Caddyfile:

  - Frontend: https://vod-tender.onnwee.me → `vod-frontend:80`
  - API: https://vod-api.onnwee.me → `vod-api:8080`

  Ensure `caddy` container is attached to the shared `web` network.

## YouTube upload configuration

Set either STAR_FILE with a JSON file path or STAR_JSON with the JSON string for credentials and token (replace STAR with YT_CREDENTIALS or YT_TOKEN):

- YT_CREDENTIALS_FILE or YT_CREDENTIALS_JSON (Google OAuth client secrets JSON)
- YT_TOKEN_FILE or YT_TOKEN_JSON (OAuth token JSON containing a refresh token)

The token must include scope:

```text
https://www.googleapis.com/auth/youtube.upload
```

Tip: Use Google OAuth 2.0 Playground to authorize the scope above and exchange an authorization code for a refresh token; save the resulting token JSON for `YT_TOKEN_*`.

## API and frontend client

OpenAPI spec lives at `backend/api/openapi.yaml`.

Generate a TypeScript client (example using openapi-typescript):

```bash
npx openapi-typescript backend/api/openapi.yaml -o web/src/api/types.ts
```

Simple CORS is enabled for dev (Access-Control-Allow-Origin: \*). For production, tighten CORS or place API behind your reverse proxy with appropriate headers.

### Monitoring

- Health: `GET /healthz` (200 OK or 503)
- Status: `GET /status` (queue counts, circuit state, moving averages, last processing run)
- Metrics: `GET /metrics` (Prometheus format: download/upload counters, durations, queue depth, circuit gauge)
- Logs: default human text; switch to JSON with `LOG_FORMAT=json` for ingestion.

## Troubleshooting quick hits

- Chat not recording: ensure `TWITCH_BOT_USERNAME` matches token owner, token has `chat:read chat:edit`, and is prefixed with `oauth:`. Auto mode requires valid Helix app credentials.
- Downloads failing with auth-required: configure `YTDLP_COOKIES_PATH` and verify the path is mounted inside the container.
- Circuit open and processing paused: check `CIRCUIT_FAILURE_THRESHOLD`, investigate root error in logs, and clear with `DELETE FROM kv WHERE key IN ('circuit_*');` if necessary.

## Security notes

- OAuth tokens are stored in plaintext in Postgres for convenience. For production, consider encrypting at rest or using a dedicated secret store.
- Avoid enabling `YTDLP_VERBOSE=1` when passing cookies; secrets may leak in verbose output.


# feature ideas

- 10 per 24hrs
- edit info
- edit thumbnail
- indexed chat
- on/off switches
- auto update cookie