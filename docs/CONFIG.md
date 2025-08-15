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

| Variable                    | Default    | Description                                                                                         |
| --------------------------- | ---------- | --------------------------------------------------------------------------------------------------- |
| DATA_DIR                    | `data`     | Directory for downloaded media files.                                                               |
| YTDLP_COOKIES_PATH          | (unset)    | Absolute path to a Netscape-format cookies file (inside container) used for Twitch auth.            |
| YTDLP_ARGS                  | (unset)    | Extra yt-dlp flags injected before the default ones.                                                |
| YTDLP_VERBOSE               | `0`        | When `1`, enables yt-dlp `-v` debug output (avoid when passing cookies to prevent secret leakage).  |
| DOWNLOAD_MAX_ATTEMPTS       | `5`        | Wrapper attempts around yt-dlp process (each may retry internally).                                 |
| DOWNLOAD_BACKOFF_BASE       | `2s`       | Base for exponential backoff (2^n scaling + jitter up to base).                                     |
| CIRCUIT_FAILURE_THRESHOLD   | (unset)    | Number of consecutive failures before opening breaker.                                              |
| CIRCUIT_OPEN_COOLDOWN       | `5m`       | Cooldown duration while breaker open.                                                               |
| BACKFILL_AUTOCLEAN          | `1`        | If not `0`, remove local file after successful upload for older VODs (back catalog).                |
| RETAIN_KEEP_NEWER_THAN_DAYS | `7`        | VODs newer than this many days are considered "new" and retained.                                   |
| NEW_VOD_STORE_MODE          | `original` | Storage for new VODs: `original`, `hevc`, `lossless-hevc`, or `av1`.                                |
| NEW_VOD_PRESET              | `medium`   | Encoder preset. For `hevc` (libx265): ultrafast..placebo. For `av1` (libsvtav1): 0..13 (0 fastest). |
| NEW_VOD_CRF                 | `23`       | Quality CRF. HEVC typical 18-28. AV1 typical 30-40. Ignored for `lossless-hevc`.                    |
| NEW_VOD_AUDIO               | `copy`     | Audio handling: `copy` to keep source or `aac128` to re-encode to AAC 128k.                         |

Notes:

- Recompression requires ffmpeg with the respective encoders (libx265 for HEVC; libsvtav1 for AV1). If unavailable, the transcode step is skipped and logged.
- When `NEW_VOD_STORE_MODE=av1` and `NEW_VOD_PRESET` is not set, an internal default preset of `6` is used.

#### Twitch authentication (subscriber-only/private VODs)

To download subscriber-only or otherwise private Twitch VODs, provide browser cookies in Netscape format and set `YTDLP_COOKIES_PATH` to that file path (inside the container). The runtime copies the cookies to a private temp file and invokes yt-dlp with `--cookies <temp-file>` so the source file remains untouched. Example with Docker Compose:

- Mount `./secrets/twitch-cookies.txt` to `/run/cookies/twitch-cookies.txt`
- Set `YTDLP_COOKIES_PATH=/run/cookies/twitch-cookies.txt`

Tips:

- Regenerate the cookies periodically from your browser; Twitch sessions expire. Use the Netscape format.
- Keep `LOG_LEVEL` at `info` when cookies are configured; `-v` is automatically disabled to avoid echoing sensitive data.
- Verify in logs that yt-dlp is invoked with `--cookies` (not a raw Cookie header).

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
