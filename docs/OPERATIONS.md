# Operations & Runbook

## Local Development

1. Copy `backend/.env.example` (or create) and fill required Twitch + optional YouTube variables.
2. Ensure dependencies installed:
    - `go` (matching module toolchain)
    - `yt-dlp` (required for VOD downloads)
    - `ffmpeg` (recommended; used by yt-dlp for muxing)
    - `aria2c` (optional performance / reliability boost)
3. Run: `make run` (loads `backend/.env`).

### Docker

Build backend & frontend images via existing Dockerfiles or compose:

```bash
make docker-build
docker compose up
```

Pass environment variables (see CONFIG.md) using an env file or compose `environment:` section.

#### Multi-instance (per-channel) runs

Compose is parameterized via a root `.env` file. Copy `.env.example` to `.env` and set:

-   `STACK_NAME` – instance name (used in container names/labels)
-   `TWITCH_CHANNEL` – for identification and defaults
-   `API_PORT`, `FRONTEND_PORT` – host ports
-   `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DB_HOST` – database settings
-   `SECRETS_DIR`, `YTDLP_COOKIES_PATH` – cookies mount for this instance

You can spin up multiple instances by using separate directories each with its own `.env` and backend `.env` files, sharing an external `WEB_NETWORK`:

```bash
cp .env.example .env
echo "STACK_NAME=vod-ch1" >> .env
echo "TWITCH_CHANNEL=channel1" >> .env
echo "API_PORT=18080" >> .env
echo "FRONTEND_PORT=18081" >> .env
docker compose --env-file .env up -d --build

# In another working dir or with another env-file
cp .env.example .env
echo "STACK_NAME=vod-ch2" >> .env
echo "TWITCH_CHANNEL=channel2" >> .env
echo "API_PORT=28080" >> .env
echo "FRONTEND_PORT=28081" >> .env
docker compose --env-file .env up -d --build
```

Each instance uses its own Postgres database name (via `DB_NAME`) inside its containerized Postgres. If pointing multiple API instances at a shared/managed Postgres, ensure `DB_NAME` is unique per channel.

### Monitoring & Observability

Logging: Uses Go `slog` with configurable level (`LOG_LEVEL`) and format (`LOG_FORMAT=text|json`). JSON mode is ideal for shipping to centralized log systems (e.g., Loki, ELK); each log line includes structured fields like `component=vod_process` or `component=vod_download` plus timing (`dl_ms`, `upl_ms`, `total_ms` where applicable) and queue depth snapshots.

Endpoints:

-   `/healthz` – liveness (DB ping only). Returns 200 OK or 503.
-   `/status` – lightweight JSON summary: pending / errored / processed counts, circuit breaker state, moving averages, last process run timestamp.
-   `/metrics` – Prometheus exposition format metrics (see Metrics section below).
-   `/admin/monitor` – extended internal stats (job timestamps, circuit).

Moving Averages (EMAs) stored in `kv`:

-   `avg_download_ms` – recent download duration trend.
-   `avg_upload_ms` – recent upload duration trend.
-   `avg_total_ms` – overall processing time trend.

Interpretation: Rising `avg_download_ms` may indicate network or Twitch CDN slowness; rising `avg_upload_ms` could be YouTube API throttling; high `avg_total_ms` vs sum of others suggests local queuing or CPU bottlenecks.

Metrics Exposed (Prometheus):

-   `vod_downloads_started_total` / `vod_downloads_succeeded_total` / `vod_downloads_failed_total`
-   `vod_uploads_succeeded_total` / `vod_uploads_failed_total`
-   `vod_processing_cycles_total`
-   `vod_download_duration_seconds` (histogram)
-   `vod_upload_duration_seconds` (histogram)
-   `vod_processing_total_duration_seconds` (histogram)
-   `vod_queue_depth` (gauge) – unprocessed VOD count
-   `vod_circuit_open` (gauge 1/0) – DEPRECATED: use `vod_circuit_breaker_state`
-   `vod_circuit_breaker_state` (gauge) – current circuit breaker state: 0=closed, 1=half-open, 2=open
-   `vod_circuit_breaker_failures_total` (counter) – total number of circuit breaker failures
-   `circuit_breaker_state_changes_total{from,to}` (counter) – tracks state transitions

Correlation IDs:

-   Each HTTP request gets an `X-Correlation-ID` header (reused if supplied) added to logs as `corr`. It is propagated into processing and download logs for traceability.

Suggested next steps:

-   Add readiness endpoint ensuring circuit not open and required credentials present.
-   Add histogram buckets tuning if needed for long VOD durations.

### Circuit Breaker

The circuit breaker prevents hot-looping on systemic failures (e.g., API outages, auth issues). It has three states:

#### States

1. **Closed** (normal operation): Downloads proceed normally. Failures increment the failure counter.
2. **Open** (failing): After reaching `CIRCUIT_FAILURE_THRESHOLD` consecutive failures, the circuit opens. All processing is skipped for `CIRCUIT_OPEN_COOLDOWN` duration (default 5 minutes).
3. **Half-Open** (probing): After cooldown expires, the circuit transitions to half-open and allows one request to probe system health.
    - **Success**: Circuit closes immediately, failure counter resets to 0, normal processing resumes.
    - **Failure**: Circuit reopens immediately for another cooldown period.

#### Configuration

-   `CIRCUIT_FAILURE_THRESHOLD` – number of consecutive failures before opening (default: disabled). Example: `2`
-   `CIRCUIT_OPEN_COOLDOWN` – duration to keep circuit open before transitioning to half-open (default: `5m`). Example: `10m`

#### Monitoring

-   Monitor `vod_circuit_breaker_state` gauge: 0=closed (healthy), 1=half-open (probing), 2=open (degraded)
-   Monitor `vod_circuit_breaker_failures_total` counter for failure rate trends
-   Monitor `circuit_breaker_state_changes_total` for transition frequency

### Common Operational Scenarios

| Scenario                   | Symptoms                                | Action                                                                                                                                                                   |
| -------------------------- | --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Download stuck / slow      | Progress percent not updating           | Check network; consider installing `aria2c`; verify disk space.                                                                                                          |
| Circuit breaker open       | Processing halts, log: `circuit opened` | Investigate root error (credentials, API outage). Adjust `CIRCUIT_FAILURE_THRESHOLD` / cooldown if needed. Circuit will auto-recover via half-open probe after cooldown. |
| Chat not recording         | Log: `chat recorder disabled`           | Ensure `CHAT_AUTO_START=1` or provide `TWITCH_CHANNEL`,`TWITCH_BOT_USERNAME`,`TWITCH_OAUTH_TOKEN`.                                                                       |
| Auto chat never reconciles | Placeholder VOD persists >15m           | Increase window or inspect Helix API credentials; ensure `TWITCH_CLIENT_ID/SECRET` valid.                                                                                |
| YouTube uploads missing    | `youtube_url` empty                     | Provide valid YouTube OAuth token & client creds; check token expiry refresh logs.                                                                                       |

### Data Management

-   To reset processing state (force reprocess a VOD): `UPDATE vods SET processed=0, processing_error=NULL, youtube_url=NULL WHERE twitch_vod_id='...'`.
-   To clear circuit breaker: delete its keys: `DELETE FROM kv WHERE key IN ('circuit_state','circuit_failures','circuit_open_until');`.
-   Backup strategy: use `pg_dump` (logical) or base backups (e.g., `pg_basebackup`) plus the `data/` directory (video files). For small hobby deployments a daily `pg_dump > backup.sql` is usually sufficient.

### Security Notes

-   OAuth tokens are encrypted at rest using AES-256-GCM when `ENCRYPTION_KEY` is set. See "OAuth Token Encryption Migration" section below for migration from plaintext to encrypted storage.
-   Limit scope of Twitch & YouTube tokens to necessary permissions.
-   Avoid mounting the `data/` directory with overly broad permissions (use user-owned paths, not world-writable).
-   Use least‑privilege Postgres role (revoking CREATEDB, SUPERUSER if not needed). Restrict network access (security groups / firewalls).

#### OAuth Token Encryption Migration

vod-tender supports encrypted storage of OAuth tokens at rest using AES-256-GCM. The system maintains backward compatibility with plaintext tokens during migration.

**Encryption Versions**:

-   **Version 0**: Plaintext (legacy, not recommended for production)
-   **Version 1**: AES-256-GCM encrypted (current standard)

**Setting Up Encryption for New Deployments**:

1. Generate a secure 32-byte encryption key:

    ```bash
    openssl rand -base64 32
    ```

2. Set the `ENCRYPTION_KEY` environment variable in `backend/.env`:

    ```bash
    ENCRYPTION_KEY=your-base64-encoded-32-byte-key-here
    ```

3. Store the key securely:

    - For production: Use AWS Secrets Manager, HashiCorp Vault, or similar
    - For Docker Compose: Mount as secret or use Docker secrets
    - For Kubernetes: Use Sealed Secrets or external secrets operator
    - **Never commit the key to version control**

4. Start the application. All new tokens will be automatically encrypted (version=1).

**Migrating Existing Plaintext Tokens**:

If you have existing deployments with plaintext tokens (encryption_version=0), use the migration tool to encrypt them:

1. **Prerequisites**:

    - Database must be accessible via `DB_DSN`
    - `ENCRYPTION_KEY` must be set and valid
    - Application can remain running during migration (it handles both formats)

2. **Dry-Run (Recommended First Step)**:

    ```bash
    # Inside the backend container or with Go installed locally
    export DB_DSN="postgres://vod:vod@postgres:5432/vod?sslmode=disable"
    export ENCRYPTION_KEY="your-base64-key"
    ./migrate-tokens --dry-run
    ```

    This shows what would be migrated without making changes.

3. **Run Migration**:

    ```bash
    # Migrate all tokens
    ./migrate-tokens

    # Or migrate specific channel only
    ./migrate-tokens --channel "your-channel-name"
    ```

4. **Verify Migration**:

    ```bash
    # Check encryption status of all tokens
    psql -U vod -d vod -c "SELECT provider, channel, encryption_version, encryption_key_id FROM oauth_tokens;"
    ```

    All tokens should show `encryption_version = 1` after successful migration.

**Docker Compose Example**:

```bash
# Build the migration tool
docker compose exec api go build -o /app/migrate-tokens ./cmd/migrate-tokens

# Run dry-run
docker compose exec api /app/migrate-tokens --dry-run

# Run actual migration
docker compose exec api /app/migrate-tokens
```

**Kubernetes Example**:

```bash
# Build and run migration in a pod
kubectl exec -it deployment/vod-tender-backend -- sh
cd /app
go build -o migrate-tokens ./cmd/migrate-tokens
./migrate-tokens --dry-run
./migrate-tokens
```

**Migration Output**:

```
level=INFO msg="found plaintext tokens to migrate" count=3 dry_run=false
level=INFO msg="migrated token successfully" provider=twitch channel= index=1 total=3
level=INFO msg="migrated token successfully" provider=youtube channel= index=2 total=3
level=INFO msg="migrated token successfully" provider=twitch channel=channel-a index=3 total=3
level=INFO msg="migration summary" total=3 migrated=3 errors=0 dry_run=false
level=INFO msg="migration completed successfully"
```

**Important Notes**:

-   The migration is **idempotent** - safe to run multiple times
-   Each token update is **atomic** (uses database transaction)
-   Tokens are migrated one at a time with progress logging
-   Failed migrations are logged but don't stop the process
-   The application continues to work with mixed encryption versions during migration
-   **After migration**, you can optionally enforce encryption by requiring `ENCRYPTION_KEY` at startup

**Backward Compatibility**:

The system supports reading tokens in any encryption version:

-   Version 0 (plaintext): Read directly without decryption
-   Version 1 (encrypted): Automatically decrypt using `ENCRYPTION_KEY`

New tokens are always written with the highest supported version when `ENCRYPTION_KEY` is set.

**Disabling Plaintext Fallback** (After Migration):

Once all tokens are migrated to version 1, you can disable plaintext storage by:

1. Verifying all tokens are encrypted:

    ```sql
    SELECT COUNT(*) FROM oauth_tokens WHERE encryption_version = 0;
    -- Should return 0
    ```

2. Adding application logic to reject version 0 tokens if needed (optional)

3. Ensuring `ENCRYPTION_KEY` is always set in production environments

**Key Rotation** (Future Enhancement):

Currently, the system uses a single encryption key identified as "default". For key rotation:

1. Keep old `ENCRYPTION_KEY` accessible for reading existing tokens
2. Set new key and key ID (requires code update for multi-key support)
3. Re-encrypt all tokens with new key using migration tool

For now, protect your encryption key carefully and rotate by:

-   Generating new key
-   Running migration with new key to re-encrypt all tokens
-   Update key in all environments

### Scaling Considerations

| Axis        | Current Approach            | Scaling Path                                                                            |
| ----------- | --------------------------- | --------------------------------------------------------------------------------------- |
| DB          | Single Postgres instance    | Add connection pooling (pgbouncer), tune indices, partition large tables, read replicas |
| Parallelism | Single processing goroutine | Add worker pool; rate limit per provider; sharded consumers via advisory locks          |
| Chat        | Single channel              | Add channel column to tables; run per-channel goroutines with supervisor                |
| Downloads   | Single active per process   | Queue + concurrency limit; distributed coordination (advisory locks or leader election) |

### Troubleshooting Checklist

1. Validate environment variables (print selectively or use an admin endpoint – avoid dumping secrets).
2. Confirm Helix app token retrieval succeeded (startup log with masked tail). In JSON mode filter by `"msg":"twitch app token acquired"`.
3. Query DB for pending VODs: `SELECT twitch_vod_id, processed, processing_error FROM vods ORDER BY date;`.
4. Inspect `download_state` and retry counters for stuck items.
5. Check system resources: disk IO, free space, network throughput.

### Security Scanning

The CI pipeline includes automated container security scanning using Trivy:

-   **Backend image**: Scanned for OS and library vulnerabilities in `vod-tender-backend`
-   **Frontend image**: Scanned for OS and library vulnerabilities in `vod-tender-frontend`
-   **Severity threshold**: Build fails on CRITICAL or HIGH severity vulnerabilities
-   **Reports**: Available as CI artifacts (SARIF and JSON formats) with 30-day retention
-   **GitHub Security**: SARIF results automatically uploaded to GitHub Security tab for tracking
-   **Baseline allowlist**: Optional `.trivyignore` file available for suppressing reviewed/accepted vulnerabilities

To manually scan images locally:

```bash
# Install Trivy
curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin

# Scan backend image
trivy image vod-tender-backend:latest --severity CRITICAL,HIGH

# Scan frontend image
trivy image vod-tender-frontend:latest --severity CRITICAL,HIGH

# Generate detailed JSON report
trivy image vod-tender-backend:latest --format json --output backend-scan.json
```

### Maintenance Tasks

-   Postgres routine maintenance: autovacuum should suffice; consider manual `VACUUM ANALYZE` only if bloat observed.
-   Regular backups (`pg_dump` or WAL archiving) and periodic restore tests.
-   Rotate logs via process manager (if not using Docker log drivers with retention).

#### Storage Retention and Cleanup

The retention job manages disk usage by automatically cleaning up old downloaded VOD files while preserving database records and metadata.

**Configuration**

Set retention policies via environment variables (see `CONFIG.md` for details):

```bash
# Keep only VODs from last 30 days
RETENTION_KEEP_DAYS=30

# Keep only the 100 most recent VODs
RETENTION_KEEP_COUNT=100

# Use both (VODs retained if they match either policy)
RETENTION_KEEP_DAYS=30
RETENTION_KEEP_COUNT=100

# Run cleanup every 12 hours (default: 6h)
RETENTION_INTERVAL=12h

# Enable dry-run mode for testing
RETENTION_DRY_RUN=1
```

**Testing with Dry-Run Mode**

Before enabling retention cleanup in production, **always test with dry-run mode first**:

1. Set `RETENTION_DRY_RUN=1` in your environment
2. Set your desired retention policies (e.g., `RETENTION_KEEP_DAYS=30`)
3. Restart the backend service
4. Monitor logs for "dry-run: would delete file" messages
5. Review the list of files that would be deleted
6. Once satisfied, set `RETENTION_DRY_RUN=0` (or remove it) and restart

**What Gets Deleted**

-   **Files deleted**: Local video files (`*.mp4`, `*.mkv`, `*.webm`) in the `DATA_DIR` that exceed retention policy
-   **Database preserved**: VOD metadata, dates, titles, YouTube URLs, and chat logs remain in the database
-   **Protected from deletion**:
    -   VODs currently being downloaded or processed
    -   VODs updated within the last hour (may be uploading)
    -   VODs with active `download_state` (downloading, processing)
    -   VODs matching retention policies (by date or count)

**Monitoring**

Watch for these log messages from the retention job:

```
level=INFO msg="retention job starting" channel=mystream keep_days=30 keep_count=100 dry_run=false interval=6h
level=INFO msg="retention cleanup completed" mode=cleanup cleaned=15 skipped=85 errors=0 bytes_freed=25769803776
level=INFO msg="deleted old vod file" path=data/vod123.mp4 vod_id=123 title="Old Stream" size_bytes=1073741824
```

Key metrics to monitor:

-   `cleaned`: Number of files deleted
-   `skipped`: Number of files retained (should match your policy expectations)
-   `errors`: Should typically be 0; investigate if non-zero
-   `bytes_freed`: Total disk space reclaimed

**Common Retention Strategies**

| Use Case                   | Configuration                               | Notes                                                 |
| -------------------------- | ------------------------------------------- | ----------------------------------------------------- |
| Short-term local cache     | `RETENTION_KEEP_DAYS=7`                     | Keep last week only; rely on YouTube for long-term    |
| Fixed-size archive         | `RETENTION_KEEP_COUNT=50`                   | Always keep exactly 50 most recent VODs               |
| Hybrid approach            | `RETENTION_KEEP_DAYS=14` + `KEEP_COUNT=100` | Keep 2 weeks OR top 100, whichever is more permissive |
| High-volume streamer       | `RETENTION_KEEP_DAYS=3`                     | Short retention for daily/multi-daily streams         |
| Archive everything locally | (leave both unset)                          | No automatic cleanup; manage manually                 |

**Manual Cleanup**

To manually clean up specific VODs without waiting for the retention job:

```bash
# Clear downloaded_path for a specific VOD (keeps metadata)
psql -U vod -d vod -c "UPDATE vods SET downloaded_path=NULL WHERE twitch_vod_id='123456789';"

# Then manually remove the file
rm /path/to/data/vod_123456789.mp4
```

**Per-Channel Retention (Multi-Channel Mode)**

When running multiple channels, each channel's retention policy runs independently. The policies apply globally via environment variables, so all channels use the same settings. For per-channel retention policies, consider running separate instances with different configurations.

**Troubleshooting**

| Issue                     | Cause                   | Solution                                                     |
| ------------------------- | ----------------------- | ------------------------------------------------------------ |
| Retention job not running | No policy configured    | Set at least `RETENTION_KEEP_DAYS` or `RETENTION_KEEP_COUNT` |
| Files not being deleted   | Dry-run mode enabled    | Set `RETENTION_DRY_RUN=0` or remove the variable             |
| Too many files deleted    | Policy too aggressive   | Increase `KEEP_DAYS` or `KEEP_COUNT` values                  |
| Active downloads deleted  | Bug (should not happen) | Check logs and report issue; safety checks should prevent    |
| Disk still full           | Policy too permissive   | Reduce retention values or clean up manually                 |

## CI/CD & Security

### Security Scanning

The CI pipeline includes automated security scans:

-   **Gitleaks** – Scans commits and PRs for secrets (API keys, tokens, passwords). Fails on any findings. Use `.gitleaks.toml` to suppress false positives if needed.
-   **govulncheck** – Checks Go dependencies for known vulnerabilities from the official Go vulnerability database. Fails on any exploitable vulnerabilities affecting the codebase.

Both tools run automatically on every push to `main` and on all pull requests. The build will fail if security issues are detected.

---

For architectural details see `ARCHITECTURE.md`. For configuration specifics see `CONFIG.md`.
