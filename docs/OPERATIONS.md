## Operations & Runbook

### Local Development

1. Copy `backend/.env.example` (or create) and fill required Twitch + optional YouTube variables.
2. Ensure dependencies installed:
   - `go` (matching module toolchain)
   - `yt-dlp` (required for VOD downloads)
   - `ffmpeg` (recommended; used by yt-dlp for muxing)
   - `aria2c` (optional performance / reliability boost)
3. Run: `make run` (loads `backend/.env`).

### Docker

Build backend & frontend images via existing Dockerfiles or compose:

```
make docker-build
docker compose up
```

Pass environment variables (see CONFIG.md) using an env file or compose `environment:` section.

### Monitoring (Manual Guidance)

Current implementation logs to stdout using `slog` at Info/Warn levels. Suggested next steps:

- Add metrics (Prometheus) for download throughput, queue length (#unprocessed), breaker state.
- Export health: existing server exposes a basic endpoint (see `server` package) – extend to include readiness checks (DB reachable, breaker state closed).

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
- Backup strategy: snapshot SQLite DB (`vodtender.db`) and `data/` directory (video files) periodically.

### Security Notes

- OAuth tokens stored plaintext in SQLite for simplicity; for production consider encryption (e.g., envelope encryption + KMS-managed KEK) or secrets manager integration.
- Limit scope of Twitch & YouTube tokens to necessary permissions.
- Avoid mounting the `data/` directory with overly broad permissions (use user-owned paths, not world-writable).

### Scaling Considerations

| Axis        | Current Approach            | Scaling Path                                                                            |
| ----------- | --------------------------- | --------------------------------------------------------------------------------------- |
| DB          | Single SQLite file          | Migrate to Postgres; abstract queries behind interfaces; add migrations tooling         |
| Parallelism | Single processing goroutine | Add worker pool; enforce rate limiting per provider                                     |
| Chat        | Single channel              | Add channel column to tables; run per-channel goroutines with supervisor                |
| Downloads   | Single active per process   | Queue + concurrency limit; distributed locking (e.g., advisory locks) if multi-instance |

### Troubleshooting Checklist

1. Validate environment variables (print selectively or use an admin endpoint – avoid dumping secrets).
2. Confirm Helix app token retrieval succeeded (startup log with masked tail).
3. Query DB for pending VODs: `SELECT twitch_vod_id, processed, processing_error FROM vods ORDER BY date;`.
4. Inspect `download_state` and retry counters for stuck items.
5. Check system resources: disk IO, free space, network throughput.

### Maintenance Tasks

- Vacuum / integrity check (SQLite): `PRAGMA integrity_check;` and `VACUUM;` during low-traffic windows.
- Rotate logs via process manager (if not using Docker log drivers with retention).
- Periodically prune old completed downloads if space constrained (after confirming upload). Add retention policy script / cron.

---

For architectural details see `ARCHITECTURE.md`. For configuration specifics see `CONFIG.md`.
