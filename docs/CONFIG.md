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

| Variable                    | Default | Description                                                                                        |
| --------------------------- | ------- | -------------------------------------------------------------------------------------------------- |
| DATA_DIR                    | `data`  | Directory for downloaded media files.                                                              |
| YTDLP_COOKIES_PATH          | (unset) | Absolute path to a Netscape-format cookies file (inside container) used for Twitch auth.           |
| YTDLP_ARGS                  | (unset) | Extra yt-dlp flags injected before the default ones.                                               |
| YTDLP_VERBOSE               | `0`     | When `1`, enables yt-dlp `-v` debug output (avoid when passing cookies to prevent secret leakage). |
| DOWNLOAD_MAX_ATTEMPTS       | `5`     | Wrapper attempts around yt-dlp process (each may retry internally).                                |
| DOWNLOAD_BACKOFF_BASE       | `2s`    | Base for exponential backoff (2^n scaling + jitter up to base).                                    |
| CIRCUIT_FAILURE_THRESHOLD   | (unset) | Number of consecutive failures before opening breaker.                                             |
| CIRCUIT_OPEN_COOLDOWN       | `5m`    | Cooldown duration while breaker open.                                                              |
| BACKFILL_AUTOCLEAN          | `1`     | If not `0`, remove local file after successful upload for older VODs (back catalog).               |
| RETAIN_KEEP_NEWER_THAN_DAYS | `7`     | VODs newer than this many days are considered "new" and retained.                                  |
| VOD_PROCESS_INTERVAL        | `1m`    | Interval between processing cycles.                                                                |
| PROCESSING_RETRY_COOLDOWN   | `600s`  | Minimum seconds before a failed item is retried.                                                   |
| UPLOAD_MAX_ATTEMPTS         | `5`     | Attempts for YouTube upload step.                                                                  |
| UPLOAD_BACKOFF_BASE         | `2s`    | Base for exponential backoff on upload retries.                                                    |
| BACKFILL_UPLOAD_DAILY_LIMIT | `10`    | Maximum number of back-catalog uploads allowed per 24h window.                                     |

Notes:

- The current downloader stores the original file; post-processing/transcoding is not enabled in this revision. ffmpeg may still be required by yt-dlp for muxing.

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

Tokens are stored in the `oauth_tokens` table after you complete the OAuth dance using the built-in endpoints. The refresher renews them automatically ahead of expiry.

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

### OAuth Token Encryption (Security)

| Variable       | Default | Required?                    | Description                                                                                           |
| -------------- | ------- | ---------------------------- | ----------------------------------------------------------------------------------------------------- |
| ENCRYPTION_KEY | (unset) | **Required for production** | Base64-encoded 32-byte key for AES-256-GCM encryption of OAuth tokens at rest in the database.        |

**Security Notice**: OAuth tokens (Twitch, YouTube) grant full API access. When `ENCRYPTION_KEY` is not set, tokens are stored in **plaintext** in the `oauth_tokens` table. This is **only acceptable for local development**.

#### Encryption Key Generation

Generate a secure 256-bit (32-byte) encryption key:

```bash
openssl rand -base64 32
```

Example output:

```
REPLACE_WITH_YOUR_GENERATED_KEY_base64_32bytes
```

#### Key Storage Best Practices

**DO NOT** commit encryption keys to version control. Use one of these approaches:

1. **Local Development**: Store in `backend/.env` (gitignored)
2. **Docker Secrets**: Mount as `/run/secrets/encryption_key`
3. **AWS**: Use AWS Secrets Manager + IAM roles
4. **Kubernetes**: Use Kubernetes Secrets with RBAC
5. **HashiCorp Vault**: Inject via Vault agent sidecar

Example Docker Compose with secrets:

```yaml
services:
  api:
    environment:
      ENCRYPTION_KEY: ${ENCRYPTION_KEY}  # From .env or shell
    secrets:
      - encryption_key

secrets:
  encryption_key:
    file: ./secrets/encryption_key.txt
```

#### Encryption Metadata

The `oauth_tokens` table includes metadata for encryption management:

- `encryption_version`: 0 = plaintext (legacy), 1 = AES-256-GCM encrypted
- `encryption_key_id`: Identifier for key used (currently "default", future: rotation support)

#### Migration from Plaintext

Existing deployments with plaintext tokens will **automatically migrate to encrypted storage** on the next token refresh (OAuth renewal). No manual intervention required.

Migration process:

1. Set `ENCRYPTION_KEY` environment variable
2. Restart backend service
3. Existing tokens (version=0) are read as plaintext
4. On next token refresh/update, tokens are re-encrypted (version=1)
5. Monitor logs for "OAuth token encryption enabled (AES-256-GCM)" message

#### Key Rotation Procedure

To rotate encryption keys (recommended annually or on suspected compromise):

1. Generate new key: `openssl rand -base64 32`
2. **Keep old key accessible** during rotation
3. Deploy new key as `ENCRYPTION_KEY_NEW` (future enhancement)
4. Run migration script to re-encrypt all tokens with new key
5. Update `ENCRYPTION_KEY` to new value
6. Remove old key after all tokens migrated

**Current limitation**: Key rotation requires brief maintenance window to re-encrypt tokens. Future versions will support dual-key operation for zero-downtime rotation.

#### Backup Security

Database backups contain encrypted tokens (when encryption enabled), but encryption keys must be backed up separately using secure secrets management.

**Warning**: Losing the encryption key makes existing tokens **permanently unrecoverable**. Users must re-authenticate via OAuth flows.

### HTTP Server

| Variable  | Default | Description                              |
| --------- | ------- | ---------------------------------------- |
| HTTP_ADDR | `:8080` | Listen address for API/health endpoints. |

### Admin Authentication & Security

| Variable                    | Default | Required?                    | Description                                                                                                             |
| --------------------------- | ------- | ---------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| ADMIN_USERNAME              | (unset) | Recommended for production   | Username for Basic Auth on admin endpoints (e.g., `/admin/*`). Must be set with `ADMIN_PASSWORD`.                       |
| ADMIN_PASSWORD              | (unset) | Recommended for production   | Password for Basic Auth on admin endpoints. Must be set with `ADMIN_USERNAME`.                                          |
| ADMIN_TOKEN                 | (unset) | Recommended for production   | Token for header-based auth on admin endpoints (via `X-Admin-Token` header). Can be used instead of or alongside Basic Auth. |
| RATE_LIMIT_ENABLED          | `1`     | No                           | Enable rate limiting on admin and sensitive endpoints. Set to `0` to disable (not recommended for production).          |
| RATE_LIMIT_REQUESTS_PER_IP  | `10`    | No                           | Maximum requests per IP per time window for rate-limited endpoints.                                                     |
| RATE_LIMIT_WINDOW_SECONDS   | `60`    | No                           | Time window in seconds for rate limiting.                                                                               |

**Security Notice**: When `ADMIN_USERNAME`/`ADMIN_PASSWORD` or `ADMIN_TOKEN` are not set, admin endpoints are **UNPROTECTED**. This is acceptable for local development but **not recommended for production**.

#### Admin Authentication Methods

1. **Basic Auth**: Set both `ADMIN_USERNAME` and `ADMIN_PASSWORD`. Clients must provide credentials via HTTP Basic Authentication.

   ```bash
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=$(openssl rand -base64 24)  # Generate secure password
   ```

   Example request:
   ```bash
   curl -u admin:secret123 https://vod-api.example.com/admin/vod/scan
   ```

2. **Token-Based Auth**: Set `ADMIN_TOKEN`. Clients must provide token via `X-Admin-Token` header.

   ```bash
   ADMIN_TOKEN=$(openssl rand -hex 32)  # Generate secure token
   ```

   Example request:
   ```bash
   curl -H "X-Admin-Token: abc123xyz" https://vod-api.example.com/admin/vod/scan
   ```

3. **Both**: You can configure both methods. Token auth takes precedence when both credentials are provided.

#### Protected Endpoints

The following endpoints require authentication when admin auth is configured:

- `/admin/*` - All admin endpoints (catalog, monitoring, manual triggers)
- `/vods/*/cancel` - VOD download cancellation
- `/vods/*/reprocess` - VOD reprocessing trigger

#### Rate Limiting

Rate limiting is **enabled by default** for the following endpoints:

- All `/admin/*` endpoints
- `/vods/*/cancel`
- `/vods/*/reprocess`

Default limits: **10 requests per IP per 60 seconds**

When rate limit is exceeded, the API returns:
- HTTP Status: `429 Too Many Requests`
- Header: `Retry-After: 60` (seconds)

**Recommendation**: Keep rate limiting enabled in production. Disable only for testing/debugging.

### CORS (Cross-Origin Resource Sharing)

| Variable                | Default           | Description                                                                                                      |
| ----------------------- | ----------------- | ---------------------------------------------------------------------------------------------------------------- |
| ENV                     | `dev`             | Environment mode: `dev`/`development` (permissive CORS) or `production`/`prod` (restricted CORS).                |
| CORS_PERMISSIVE         | (auto from ENV)   | Explicit CORS mode override: `1` or `true` for permissive (allow all origins), `0` or `false` for restricted.   |
| CORS_ALLOWED_ORIGINS    | (empty)           | Comma-separated list of allowed origins for production mode (e.g., `https://vod.example.com,https://app.example.com`). Supports wildcards (e.g., `*.example.com`). |

**CORS Behavior**:

- **Development Mode** (default): Permissive CORS with `Access-Control-Allow-Origin: *`. Accepts requests from any origin.
- **Production Mode**: Restricted CORS. Only requests from origins listed in `CORS_ALLOWED_ORIGINS` are allowed.

**Production Example**:

```bash
ENV=production
CORS_ALLOWED_ORIGINS=https://vod.example.com,https://vod-admin.example.com,*.apps.example.com
```

**Wildcard Support**:

- `*.example.com` matches `https://app.example.com`, `https://api.example.com`, etc.
- Wildcards also match the parent domain (e.g., `https://example.com`)

**Security Best Practice**: Always set `ENV=production` and configure `CORS_ALLOWED_ORIGINS` for production deployments.

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
