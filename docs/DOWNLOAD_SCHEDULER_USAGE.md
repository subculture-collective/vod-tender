# Download Scheduler Usage Examples

This document provides practical examples for using the enhanced download scheduler features.

## Configuration

### Basic Concurrency Control

```bash
# Allow up to 3 concurrent downloads (default is 1)
MAX_CONCURRENT_DOWNLOADS=3

# Apply a global bandwidth limit of 2MB/s per download
DOWNLOAD_RATE_LIMIT=2M
```

### Advanced Configuration

```bash
# backend/.env or docker compose environment
MAX_CONCURRENT_DOWNLOADS=5          # 5 parallel downloads
DOWNLOAD_RATE_LIMIT=1.5M            # 1.5 MB/s per download
DOWNLOAD_MAX_ATTEMPTS=10            # More retries for flaky networks
DOWNLOAD_BACKOFF_BASE=5s            # Longer backoff between retries
PROCESSING_RETRY_COOLDOWN=300s      # 5 minute cooldown on errors
```

## Priority Management

### View Current Queue Status

```bash
# Check queue depth and priority breakdown
curl http://localhost:8080/status | jq '.'

# Expected output includes:
# {
#   "pending": 42,
#   "queue_by_priority": [
#     {"priority": 100, "count": 2},
#     {"priority": 10, "count": 5},
#     {"priority": 0, "count": 35}
#   ],
#   "active_downloads": 2,
#   "max_concurrent_downloads": 3,
#   "retry_config": {
#     "download_max_attempts": 5,
#     "download_backoff_base": "2s",
#     ...
#   }
# }
```

### Prioritize Specific VODs

```bash
# Bump a VOD to high priority (process next)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{
    "vod_id": "123456789",
    "priority": 100
  }'

# Set medium priority
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{
    "vod_id": "987654321",
    "priority": 50
  }'

# Reset to default priority (back of queue)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{
    "vod_id": "111222333",
    "priority": 0
  }'

# Deprioritize (process last)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{
    "vod_id": "444555666",
    "priority": -10
  }'
```

### With Authentication

```bash
# If ADMIN_USERNAME/ADMIN_PASSWORD configured
curl -u admin:secret123 -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"123456789","priority":100}'

# If ADMIN_TOKEN configured
curl -H "X-Admin-Token: your-secret-token" \
  -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"123456789","priority":100}'
```

## Monitoring

### Check Active Downloads

```bash
# Quick check of concurrency
curl http://localhost:8080/status | jq '{
  active: .active_downloads,
  max: .max_concurrent_downloads,
  queue: .pending
}'

# Example output:
# {
#   "active": 2,
#   "max": 3,
#   "queue": 42
# }
```

### Monitor Queue by Priority

```bash
# See priority distribution
curl http://localhost:8080/status | jq '.queue_by_priority'

# Example output:
# [
#   {"priority": 100, "count": 2},
#   {"priority": 10, "count": 5},
#   {"priority": 0, "count": 35}
# ]
```

### View Retry Configuration

```bash
# Check retry/backoff settings
curl http://localhost:8080/status | jq '.retry_config'

# Example output:
# {
#   "download_max_attempts": 5,
#   "download_backoff_base": "2s",
#   "upload_max_attempts": 5,
#   "upload_backoff_base": "2s",
#   "processing_retry_cooldown": "600s"
# }
```

## Batch Operations

### Prioritize Multiple VODs

```bash
# Bump a batch of VODs to high priority
for vod_id in 123456789 987654321 111222333; do
  curl -X POST http://localhost:8080/admin/vod/priority \
    -H "Content-Type: application/json" \
    -d "{\"vod_id\":\"${vod_id}\",\"priority\":100}" \
    -s | jq .
done
```

### Query and Prioritize from Database

```bash
# Find and prioritize VODs matching criteria
# (requires database access)

# Example: Prioritize all VODs from a specific date range
psql -U vod -d vod -c "
  UPDATE vods 
  SET priority = 50 
  WHERE date >= '2024-10-01' 
    AND date < '2024-11-01' 
    AND processed = false
"
```

## Use Cases

### Emergency Processing

```bash
# Scenario: Need to process a specific VOD ASAP

# 1. Check current queue
curl http://localhost:8080/status | jq '{pending, active_downloads}'

# 2. Bump VOD to highest priority
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"EMERGENCY_VOD_ID","priority":1000}'

# 3. Monitor until processed
watch -n 5 'curl -s http://localhost:8080/vods/EMERGENCY_VOD_ID | jq .processed'
```

### Batch Processing Optimization

```bash
# Scenario: Maximize throughput for large backlog

# 1. Increase concurrency
# Set MAX_CONCURRENT_DOWNLOADS=5 in backend/.env

# 2. Add bandwidth limit to prevent saturation
# Set DOWNLOAD_RATE_LIMIT=2M in backend/.env

# 3. Restart service to apply changes
docker compose restart api

# 4. Monitor throughput
curl http://localhost:8080/status | jq '{
  active: .active_downloads,
  max: .max_concurrent_downloads,
  pending: .pending,
  avg_download_ms: .avg_download_ms
}'
```

### Priority Tiers

```bash
# Scenario: Implement priority tiers for different content types

# Tier 1: Live stream archives (priority 100)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"LIVE_ARCHIVE_ID","priority":100}'

# Tier 2: Scheduled content (priority 50)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"SCHEDULED_ID","priority":50}'

# Tier 3: Back-catalog (priority 0, default)
# No action needed - default priority

# Tier 4: Optional content (priority -10)
curl -X POST http://localhost:8080/admin/vod/priority \
  -H "Content-Type: application/json" \
  -d '{"vod_id":"OPTIONAL_ID","priority":-10}'
```

## Troubleshooting

### Check if Downloads are Stalled

```bash
# Check active downloads and circuit breaker
curl http://localhost:8080/status | jq '{
  active_downloads,
  max_concurrent_downloads,
  circuit_state,
  circuit_failures,
  last_process_run
}'
```

### Reset Circuit Breaker

```bash
# If circuit breaker is stuck open, reset it manually
docker compose exec postgres psql -U vod -d vod -c "
  UPDATE kv SET value='closed' WHERE key='circuit_state';
  UPDATE kv SET value='0' WHERE key='circuit_failures';
  DELETE FROM kv WHERE key='circuit_open_until';
"
```

### Clear Priority for All VODs

```bash
# Reset all priorities to default
docker compose exec postgres psql -U vod -d vod -c "
  UPDATE vods SET priority=0 WHERE priority != 0;
"
```

## Metrics and Observability

### Prometheus Metrics

The `/metrics` endpoint exposes download scheduler metrics:

```bash
# View metrics
curl http://localhost:8080/metrics | grep vod_

# Key metrics:
# - vod_downloads_started_total: Total downloads attempted
# - vod_downloads_succeeded_total: Successful downloads
# - vod_downloads_failed_total: Failed downloads
# - vod_queue_depth: Current queue size (gauge)
# - vod_download_duration_seconds: Download duration histogram
```

### Grafana Dashboard

Add these panels to your Grafana dashboard:

1. **Active Downloads**: Query `vod_active_downloads` (from /status endpoint)
2. **Queue Depth by Priority**: Query database or parse /status JSON
3. **Download Success Rate**: `rate(vod_downloads_succeeded_total[5m]) / rate(vod_downloads_started_total[5m])`
4. **Average Download Time**: `rate(vod_download_duration_seconds_sum[5m]) / rate(vod_download_duration_seconds_count[5m])`

## Best Practices

1. **Start Conservative**: Begin with `MAX_CONCURRENT_DOWNLOADS=1` and increase gradually
2. **Monitor Bandwidth**: Use `DOWNLOAD_RATE_LIMIT` to prevent network saturation
3. **Use Priority Tiers**: Establish consistent priority levels (e.g., 100, 50, 0, -10)
4. **Watch Circuit Breaker**: If it opens frequently, investigate root cause before increasing threshold
5. **Regular Monitoring**: Check `/status` endpoint regularly to catch issues early
6. **Document Priority Schema**: Maintain documentation of what each priority level means for your use case
