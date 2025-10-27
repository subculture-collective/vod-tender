# Retention Policy Feature

The retention policy feature automatically manages disk usage by cleaning up old downloaded VOD files while preserving database records and metadata.

## Quick Start

1. **Choose a retention policy:**

   ```bash
   # In backend/.env
   RETENTION_KEEP_DAYS=30        # Keep last 30 days
   # OR
   RETENTION_KEEP_COUNT=100      # Keep last 100 VODs
   # OR use both (VODs retained if they match either)
   ```

2. **Test with dry-run first:**

   ```bash
   RETENTION_DRY_RUN=1
   ```

3. **Monitor the logs:**

   ```
   level=INFO msg="retention job starting" channel=mystream keep_days=30 dry_run=true
   level=INFO msg="dry-run: would delete file" path=data/old_vod.mp4 vod_id=123
   level=INFO msg="retention cleanup completed" mode=dry-run cleaned=15 skipped=85
   ```

4. **Enable cleanup:**

   ```bash
   RETENTION_DRY_RUN=0  # or remove the variable
   ```

## Features

- ✅ **Days-based policy**: Keep VODs newer than N days
- ✅ **Count-based policy**: Keep only the N most recent VODs
- ✅ **Hybrid policies**: Use both (union of matching VODs)
- ✅ **Dry-run mode**: Preview deletions without actually deleting
- ✅ **Safety checks**: Automatically protects active downloads/uploads
- ✅ **Per-channel support**: Works with multi-channel deployments
- ✅ **Configurable interval**: Default 6h, customizable via `RETENTION_INTERVAL`
- ✅ **Detailed logging**: Track what's deleted, skipped, and any errors

## What Gets Deleted

- ❌ Local video files (`*.mp4`, `*.mkv`, `*.webm`) exceeding retention policy
- ✅ VOD metadata preserved in database
- ✅ Chat logs preserved
- ✅ YouTube URLs preserved
- ✅ All dates, titles, and descriptions preserved

## Safety Guarantees

The retention job automatically protects:
- VODs currently being downloaded
- VODs in active processing state
- VODs updated within the last hour (likely uploading)
- VODs with download_state = 'downloading' or 'processing'
- VODs matching your retention policies

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `RETENTION_KEEP_DAYS` | (unset) | Keep VODs newer than N days |
| `RETENTION_KEEP_COUNT` | (unset) | Keep only N most recent VODs |
| `RETENTION_DRY_RUN` | `0` | When `1`, log actions without deleting |
| `RETENTION_INTERVAL` | `6h` | How often cleanup runs |

**Note:** At least one of `RETENTION_KEEP_DAYS` or `RETENTION_KEEP_COUNT` must be set for the retention job to start.

## Example Configurations

### Short-term cache (1 week)
```bash
RETENTION_KEEP_DAYS=7
RETENTION_INTERVAL=12h
```

### Fixed-size archive (100 VODs)
```bash
RETENTION_KEEP_COUNT=100
RETENTION_INTERVAL=6h
```

### Hybrid (2 weeks OR top 50)
```bash
RETENTION_KEEP_DAYS=14
RETENTION_KEEP_COUNT=50
```

### High-volume streamer
```bash
RETENTION_KEEP_DAYS=3
RETENTION_INTERVAL=3h
```

## Monitoring

Watch for these log entries:

```
# Job starting
level=INFO msg="retention job starting" channel=mystream keep_days=30 keep_count=100 dry_run=false interval=6h

# File deletion
level=INFO msg="deleted old vod file" path=data/vod123.mp4 vod_id=123 title="Old Stream" date=2024-01-01 size_bytes=1073741824

# Completion summary
level=INFO msg="retention cleanup completed" mode=cleanup cleaned=15 skipped=85 errors=0 bytes_freed=16106127360
```

Monitor these metrics:
- **cleaned**: Files successfully deleted
- **skipped**: Files retained (should match your policies)
- **errors**: Should be 0; investigate if non-zero
- **bytes_freed**: Disk space reclaimed

## Troubleshooting

| Issue | Cause | Solution |
|-------|-------|----------|
| Retention not running | No policy configured | Set `RETENTION_KEEP_DAYS` or `RETENTION_KEEP_COUNT` |
| Files not deleted | Dry-run enabled | Set `RETENTION_DRY_RUN=0` or remove it |
| Too aggressive cleanup | Policy too strict | Increase days/count values |
| Disk still full | Policy too permissive | Reduce retention values |

## Manual Cleanup

To manually clean up a specific VOD:

```bash
# Clear DB reference
psql -U vod -d vod -c "UPDATE vods SET downloaded_path=NULL WHERE twitch_vod_id='123';"

# Remove file
rm /path/to/data/vod_123.mp4
```

## Implementation Details

- **Job file**: `backend/vod/retention.go`
- **Tests**: `backend/vod/retention_test.go`
- **Started from**: `backend/main.go` (one job per channel)
- **Runs in**: Background goroutine with ticker
- **Database impact**: Minimal (few SELECT queries, UPDATE on deletion)

## Related Documentation

- Configuration reference: `docs/CONFIG.md`
- Operations guide: `docs/OPERATIONS.md`
- Architecture overview: `docs/ARCHITECTURE.md`
