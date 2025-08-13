# Configuration Reference

All configuration is via environment variables. When running locally with `make run`, place them in `backend/.env` (auto-loaded by `main.go`). Defaults are applied where sensible; absence of optional variables typically disables related features.

## Core Twitch & Chat

| Variable             | Default               | Required?           | Description                                                  |
| -------------------- | --------------------- | ------------------- | ------------------------------------------------------------ |
| TWITCH_CHANNEL       | (none)                | Yes (chat, catalog) | Channel login name to monitor.                               |
| TWITCH_BOT_USERNAME  | (none)                | Yes (chat)          | Bot username for IRC.                                        |
| TWITCH_OAUTH_TOKEN   | (none)                | Yes (chat)          | OAuth token (prefixed with `oauth:` if using Twitch format). |
| TWITCH_CLIENT_ID     | (none)                | Strongly            | Needed for Helix (discovery, auto chat live polling).        |
| TWITCH_CLIENT_SECRET | (none)                | Strongly            | Needed to fetch app access tokens.                           |
| TWITCH_REDIRECT_URI  | (none)                | No                  | For future Twitch OAuth flows.                               |
| TWITCH_SCOPES        | `chat:read chat:edit` | No                  | Scope list for chat-related authorization.                   |

### VOD Identification (Manual Chat Mode)

| Variable         | Default       | Description                                                                                      |
| ---------------- | ------------- | ------------------------------------------------------------------------------------------------ |
| TWITCH_VOD_ID    | `demo-vod-id` | Fixed VOD id used when CHAT_AUTO_START is not set and chat recording should bind to a known VOD. |
| TWITCH_VOD_START | now()         | RFC3339 start time; used to compute relative timestamps.                                         |

### Auto Chat Recorder

| Variable                     | Default | Description                                                       |
| ---------------------------- | ------- | ----------------------------------------------------------------- |
| CHAT_AUTO_START              | (unset) | If `1`, enables automatic live detection + placeholder VOD logic. |
| CHAT_AUTO_POLL_INTERVAL      | `30s`   | Poll frequency for live status.                                   |
| VOD_RECONCILE_DELAY          | `1m`    | Wait before starting reconciliation after stream ends.            |
| (hardcoded) reconcile window | 15m     | Time after offline to keep attempting reconciliation.             |

### Catalog Backfill

| Variable                      | Default            | Description                                                 |
| ----------------------------- | ------------------ | ----------------------------------------------------------- |
| VOD_CATALOG_BACKFILL_INTERVAL | `6h`               | Interval between backfill runs.                             |
| VOD_CATALOG_MAX               | (0 = unlimited)    | Maximum VODs to fetch per run.                              |
| VOD_CATALOG_MAX_AGE_DAYS      | (0 = no age limit) | Stop paging when VOD older than this many days encountered. |

### Download & Processing

| Variable                  | Default | Description                                                         |
| ------------------------- | ------- | ------------------------------------------------------------------- |
| DATA_DIR                  | `data`  | Directory for downloaded media files.                               |
| DOWNLOAD_MAX_ATTEMPTS     | `5`     | Wrapper attempts around yt-dlp process (each may retry internally). |
| DOWNLOAD_BACKOFF_BASE     | `2s`    | Base for exponential backoff (2^n scaling + jitter up to base).     |
| CIRCUIT_FAILURE_THRESHOLD | (unset) | Number of consecutive failures before opening breaker.              |
| CIRCUIT_OPEN_COOLDOWN     | `5m`    | Cooldown duration while breaker open.                               |

### YouTube Upload

| Variable         | Default                                          | Description                          |
| ---------------- | ------------------------------------------------ | ------------------------------------ |
| YT_CLIENT_ID     | (none)                                           | OAuth Client ID for YouTube uploads. |
| YT_CLIENT_SECRET | (none)                                           | OAuth Client Secret.                 |
| YT_REDIRECT_URI  | (none)                                           | Redirect URI for OAuth dance.        |
| YT_SCOPES        | `https://www.googleapis.com/auth/youtube.upload` | Space or comma separated scopes.     |

Token JSON / credential file indirection is handled externally: populate database using an auth endpoint (future) or manual insert; current code refreshes tokens found in `oauth_tokens` table.

### Database

| Variable | Default                                                | Description                                                                                                       |
| -------- | ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| DB_DSN   | `postgres://vod:vod@postgres:5432/vod?sslmode=disable` | Postgres connection string (pgx format). Override to point at managed Postgres or local instance (ssl as needed). |

Example DSNs:

```text
postgres://user:pass@localhost:5432/vod?sslmode=disable
postgres://user:pass@prod-host:5432/vod?sslmode=require
```

Minimum required privileges: ability to create tables & indices on first run (idempotent migrations).

### HTTP Server

| Variable  | Default | Description                              |
| --------- | ------- | ---------------------------------------- |
| HTTP_ADDR | `:8080` | Listen address for API/health endpoints. |

### Miscellaneous / Logging

| Variable   | Default | Description                                                         |
| ---------- | ------- | ------------------------------------------------------------------- |
| LOG_LEVEL  | info    | Logging verbosity: debug, info, warn, error.                        |
| LOG_FORMAT | text    | Log output format: text (human) or json (structured for ingestion). |

### Derived / Internal Keys (kv table)

| Key                  | Purpose                                                                    |
| -------------------- | -------------------------------------------------------------------------- |
| catalog_after        | Cursor for next Helix page during catalog ingestion (when unlimited mode). |
| circuit_state        | `open` or `closed`.                                                        |
| circuit_failures     | Count of consecutive failures.                                             |
| circuit_open_until   | RFC3339 timestamp when breaker can close.                                  |
| avg_download_ms      | Exponential moving average of recent download durations (milliseconds).    |
| avg_upload_ms        | Exponential moving average of recent upload durations (milliseconds).      |
| avg_total_ms         | Exponential moving average of end-to-end processing durations (ms).        |
| job_vod_process_last | RFC3339 timestamp of last processing cycle (success or attempt).           |

---

If a variable is absent above it is either deprecated or internal to implementation details.
