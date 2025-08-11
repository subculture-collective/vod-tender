# vod-tender

Small service to record Twitch chat tied to VODs and process VOD metadata.

## Quick start

-   Copy `.env.example` to `backend/.env` and fill in values.
-   Build and run locally:

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

-   TWITCH_CHANNEL
-   TWITCH_BOT_USERNAME
-   TWITCH_OAUTH_TOKEN
-   TWITCH_VOD_ID (optional; default demo-vod-id)
-   TWITCH_VOD_START (RFC3339; optional; default now)
-   DB_DSN (optional; default vodtender.db)

Chat recorder starts only when Twitch creds are present.

## Notes

-   Database: SQLite by default; file path via DB_DSN.
-   VOD processing job runs hourly (placeholder for now).
