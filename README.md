# vod-tender

Small service to record Twitch chat tied to VODs and process VOD metadata.

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

Chat recorder starts only when Twitch creds are present.

Full configuration reference and operational guidance:

- Architecture: `docs/ARCHITECTURE.md`
- Configuration: `docs/CONFIG.md`
- Operations / Runbook: `docs/OPERATIONS.md`

## Notes

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

Simple CORS is enabled for dev (Access-Control-Allow-Origin: \*). For production, tighten CORS settings.

### Monitoring

- Health: `GET /healthz` (200 OK or 503)
- Status: `GET /status` (queue counts, circuit state, moving averages, last processing run)
- Metrics: `GET /metrics` (Prometheus format: download/upload counters, durations, queue depth, circuit gauge)
- Logs: default human text; switch to JSON with `LOG_FORMAT=json` for ingestion.
