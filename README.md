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

## Configuration

Environment variables (place in `backend/.env` for local dev):

- TWITCH_CHANNEL
- TWITCH_BOT_USERNAME
- TWITCH_OAUTH_TOKEN
- TWITCH_CLIENT_ID (for Helix API discovery)
- TWITCH_CLIENT_SECRET (for Helix API discovery)
- TWITCH_VOD_ID (optional; default demo-vod-id)
- TWITCH_VOD_START (RFC3339; optional; default now)
- DB_DSN (optional; default vodtender.db)
- DATA_DIR (optional; default data)

Chat recorder starts only when Twitch creds are present.

Full configuration reference and operational guidance:

- Architecture: `docs/ARCHITECTURE.md`
- Configuration: `docs/CONFIG.md`
- Operations / Runbook: `docs/OPERATIONS.md`

## Notes

- Database: SQLite by default; file path via DB_DSN.
- VOD processing job runs hourly; discovery uses Twitch Helix if client id/secret provided.
- Downloader requires yt-dlp available in PATH.
  - Resumable downloads are enabled (yt-dlp --continue with infinite retries and fragment retries).
  - Optional: install aria2c for faster and more robust downloads.
  - ffmpeg is recommended for muxing and may be required by yt-dlp.

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
