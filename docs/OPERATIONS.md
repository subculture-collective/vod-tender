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

### Monitoring & Observability

Logging: Uses Go `slog` with configurable level (`LOG_LEVEL`) and format (`LOG_FORMAT=text|json`). JSON mode is ideal for shipping to centralized log systems (e.g., Loki, ELK); each log line includes structured fields like `component=vod_process` or `component=vod_download` plus timing (`dl_ms`, `upl_ms`, `total_ms` where applicable) and queue depth snapshots.

Endpoints:

- `/healthz` – liveness (DB ping only). Returns 200 OK or 503.
- `/status` – lightweight JSON summary: pending / errored / processed counts, circuit breaker state, moving averages, last process run timestamp.
- `/metrics` – Prometheus exposition format metrics (see Metrics section below).
- `/admin/monitor` – extended internal stats (job timestamps, circuit) – may evolve or be merged later.

Moving Averages (EMAs) stored in `kv`:

- `avg_download_ms` – recent download duration trend.
- `avg_upload_ms` – recent upload duration trend.
- `avg_total_ms` – overall processing time trend.

Interpretation: Rising `avg_download_ms` may indicate network or Twitch CDN slowness; rising `avg_upload_ms` could be YouTube API throttling; high `avg_total_ms` vs sum of others suggests local queuing or CPU bottlenecks.

Metrics Exposed (Prometheus):

- `vod_downloads_started_total` / `vod_downloads_succeeded_total` / `vod_downloads_failed_total`
- `vod_uploads_succeeded_total` / `vod_uploads_failed_total`
- `vod_processing_cycles_total`
- `vod_download_duration_seconds` (histogram)
- `vod_upload_duration_seconds` (histogram)
- `vod_processing_total_duration_seconds` (histogram)
- `vod_queue_depth` (gauge) – unprocessed VOD count
- `vod_circuit_open` (gauge 1/0)

Correlation IDs:

- Each HTTP request gets an `X-Correlation-ID` header (reused if supplied) added to logs as `corr`. Propagated into processing and download logs for traceability across lifecycle events.

Suggested next steps:

- Add readiness endpoint ensuring circuit not open and required credentials present.
- Add histogram buckets tuning if needed for long VOD durations.

### Common Operational Scenarios

| Scenario                   | Symptoms                                | Action                                                                                                     |
| -------------------------- | --------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Download stuck / slow      | Progress percent not updating           | Check network; consider installing `aria2c`; verify disk space.                                            |
| Circuit breaker open       | Processing halts, log: `circuit opened` | Investigate root error (credentials, API outage). Adjust `CIRCUIT_FAILURE_THRESHOLD` / cooldown if needed. |
| Chat not recording         | Log: `chat recorder disabled`           | Ensure `CHAT_AUTO_START=1` or provide `TWITCH_CHANNEL`,`TWITCH_BOT_USERNAME`,`TWITCH_OAUTH_TOKEN`.         |
| Auto chat never reconciles | Placeholder VOD persists >15m           | Increase window or inspect Helix API credentials; ensure `TWITCH_CLIENT_ID/SECRET` valid.                  |
| YouTube uploads missing    | `youtube_url` empty                     | Provide valid YouTube OAuth token & client creds; check token expiry refresh logs.                         |

### Data Management

- To reset processing state (force reprocess a VOD): `UPDATE vods SET processed=0, processing_error=NULL, youtube_url=NULL WHERE twitch_vod_id='...'`.
- To clear circuit breaker: delete its keys: `DELETE FROM kv WHERE key IN ('circuit_state','circuit_failures','circuit_open_until');`.
- Backup strategy: use `pg_dump` (logical) or base backups (e.g., `pg_basebackup`) plus the `data/` directory (video files). For small hobby deployments a daily `pg_dump > backup.sql` is usually sufficient.

### Security Notes

- OAuth tokens stored plaintext in `oauth_tokens`; for production consider application‑level encryption (envelope + KMS) or a dedicated secrets store.
- Limit scope of Twitch & YouTube tokens to necessary permissions.
- Avoid mounting the `data/` directory with overly broad permissions (use user-owned paths, not world-writable).
- Use least‑privilege Postgres role (revoking CREATEDB, SUPERUSER if not needed). Restrict network access (security groups / firewalls).

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

### Maintenance Tasks

- Postgres routine maintenance: autovacuum should suffice; consider manual `VACUUM ANALYZE` only if bloat observed.
- Regular backups (`pg_dump` or WAL archiving) and periodic restore tests.
- Rotate logs via process manager (if not using Docker log drivers with retention).
- Periodically prune old completed downloads if space constrained (after confirming upload). Add retention policy script / cron.

---

For architectural details see `ARCHITECTURE.md`. For configuration specifics see `CONFIG.md`.
