# Operational Runbooks

Comprehensive procedures for monitoring, troubleshooting, and maintaining vod-tender in production.

## Table of Contents

- [Incident Response Procedures](#incident-response-procedures)
- [Monitoring Setup Guide](#monitoring-setup-guide)
- [Backup and Restore Procedures](#backup-and-restore-procedures)
- [Maintenance Procedures](#maintenance-procedures)
- [Emergency Procedures](#emergency-procedures)

## Incident Response Procedures

### High CPU Usage

**Symptoms**:
- System load average > number of cores
- Application slowness or timeouts
- High CPU percentage in monitoring dashboard

**Diagnosis**:

```bash
# Check current CPU usage
top -bn1 | head -20

# Kubernetes
kubectl top pods -n vod-tender

# Identify process
ps aux | sort -nrk 3,3 | head -5

# Check download queue
curl https://vod-api.example.com/status | jq '.queue_depth'

# View active downloads
docker exec vod-api ps aux | grep yt-dlp
```

**Resolution**:

1. **Immediate Mitigation**:
   ```bash
   # Check if multiple downloads running
   # Current design should allow only 1 concurrent download
   
   # If circuit breaker is closed but queue is large
   # Temporarily pause processing
   docker exec vod-api kill -STOP $(pidof vod-tender)
   
   # Scale horizontally (if multi-instance capable)
   kubectl scale deployment/vod-api --replicas=2 -n vod-tender
   ```

2. **Root Cause Investigation**:
   - Check logs for runaway goroutines
   - Review download sizes and formats
   - Check if FFmpeg is consuming excessive CPU during muxing
   - Verify `yt-dlp` not stuck in retry loop

3. **Long-term Fix**:
   - Adjust `VOD_PROCESS_INTERVAL` to reduce processing frequency
   - Implement download size limits
   - Add CPU resource limits in container spec
   - Consider transcoding to lower quality

**Prevention**:
- Set CPU limits in container: `resources.limits.cpu: "2"`
- Alert on sustained high CPU: > 80% for 5 minutes
- Implement download time limits

---

### Out of Disk Space

**Symptoms**:
- Logs show "no space left on device"
- Downloads failing with disk I/O errors
- Database writes failing

**Diagnosis**:

```bash
# Check disk usage
df -h

# Find large files
du -h /opt/vod-tender/data | sort -rh | head -20

# Kubernetes
kubectl exec -it deployment/vod-api -n vod-tender -- df -h
kubectl exec -it deployment/vod-api -n vod-tender -- du -sh /data/*

# Check database size
psql -U vod -d vod -c "SELECT pg_size_pretty(pg_database_size('vod'));"
```

**Resolution**:

1. **Immediate Mitigation**:
   ```bash
   # Clean up old VOD files (if BACKFILL_AUTOCLEAN enabled)
   # Manually delete old files
   find /opt/vod-tender/data -name "*.mp4" -mtime +30 -delete
   
   # Or move to archive storage
   find /opt/vod-tender/data -name "*.mp4" -mtime +7 \
     -exec mv {} /mnt/archive/ \;
   
   # Clean Docker unused images/volumes
   docker system prune -a --volumes
   
   # Kubernetes: Expand PVC (if storage class supports it)
   kubectl patch pvc vod-data -n vod-tender \
     -p '{"spec":{"resources":{"requests":{"storage":"500Gi"}}}}'
   ```

2. **Root Cause Investigation**:
   - Check if `BACKFILL_AUTOCLEAN` is enabled
   - Verify `RETAIN_KEEP_NEWER_THAN_DAYS` is appropriate
   - Review VOD file sizes (large streams can be 5-10GB+)
   - Check for log file bloat

3. **Long-term Fix**:
   - Enable auto-cleanup: `BACKFILL_AUTOCLEAN=1`
   - Implement lifecycle policy to move old VODs to cheaper storage
   - Increase volume size
   - Set up disk usage alerts (> 80% used)

**Prevention**:
- Alert on disk usage: > 80% capacity
- Enable auto-cleanup after upload
- Implement retention policies
- Monitor growth trends

---

### Database Connection Errors

**Symptoms**:
- Logs show "connection refused" or "too many connections"
- Application unable to start
- API returns 500 errors

**Diagnosis**:

```bash
# Check if database is running
docker ps | grep postgres
kubectl get pods -n vod-tender | grep postgres

# Check connection from application
docker exec vod-api nc -zv postgres 5432
kubectl exec deployment/vod-api -n vod-tender -- nc -zv postgres 5432

# Check PostgreSQL logs
docker logs vod-postgres
kubectl logs statefulset/postgres -n vod-tender

# Check active connections
psql -U vod -d vod -c "SELECT count(*) FROM pg_stat_activity;"

# Check max connections
psql -U vod -d vod -c "SHOW max_connections;"
```

**Resolution**:

1. **Immediate Mitigation**:
   ```bash
   # Restart database (if safe)
   docker restart vod-postgres
   kubectl rollout restart statefulset/postgres -n vod-tender
   
   # Kill idle connections
   psql -U vod -d vod -c "
     SELECT pg_terminate_backend(pid) 
     FROM pg_stat_activity 
     WHERE state = 'idle' 
     AND state_change < NOW() - INTERVAL '5 minutes';
   "
   
   # Restart application
   docker restart vod-api
   kubectl rollout restart deployment/vod-api -n vod-tender
   ```

2. **Root Cause Investigation**:
   - Check if connection pool exhausted
   - Review `DB_MAX_OPEN_CONNS` and `DB_MAX_IDLE_CONNS` settings
   - Look for connection leaks in application
   - Verify network connectivity

3. **Long-term Fix**:
   - Increase `max_connections` in PostgreSQL
   - Tune connection pool settings
   - Implement connection retry logic
   - Add connection pool metrics

**Prevention**:
- Monitor active connections
- Alert on connection pool saturation
- Implement circuit breaker for DB connections
- Regular connection pool analysis

---

### Circuit Breaker Stuck Open

**Symptoms**:
- Logs show "circuit breaker open, skipping processing"
- No VODs being processed
- Status endpoint shows `circuit_state: open`

**Diagnosis**:

```bash
# Check circuit breaker state
curl https://vod-api.example.com/status | jq '.circuit_breaker'

# Query database
docker exec vod-postgres psql -U vod -d vod -c "
  SELECT key, value FROM kv WHERE key LIKE 'circuit_%';
"

# Check processing errors
docker exec vod-postgres psql -U vod -d vod -c "
  SELECT twitch_vod_id, processing_error, updated_at 
  FROM vods 
  WHERE processing_error IS NOT NULL 
  ORDER BY updated_at DESC 
  LIMIT 10;
"

# Review logs for failure patterns
docker logs vod-api | grep -i error | tail -50
```

**Resolution**:

1. **Investigate Root Cause**:
   - Review processing errors in database
   - Check if credentials expired
   - Verify network connectivity to Twitch/YouTube
   - Check if Twitch API quota exhausted

2. **Fix Underlying Issue**:
   ```bash
   # Example: Refresh expired OAuth token
   curl -X POST https://vod-api.example.com/auth/twitch/refresh
   
   # Example: Update cookies
   kubectl create secret generic vod-cookies \
     --from-file=twitch-cookies.txt=./new-cookies.txt \
     --dry-run=client -o yaml | kubectl apply -f -
   
   # Restart application
   kubectl rollout restart deployment/vod-api -n vod-tender
   ```

3. **Manual Reset** (only after fixing root cause):
   ```bash
   # Reset circuit breaker
   docker exec vod-postgres psql -U vod -d vod -c "
     DELETE FROM kv WHERE key IN (
       'circuit_state', 
       'circuit_failures', 
       'circuit_open_until'
     );
   "
   
   # Or update to closed state
   docker exec vod-postgres psql -U vod -d vod -c "
     UPDATE kv SET value='closed' WHERE key='circuit_state';
     DELETE FROM kv WHERE key IN ('circuit_failures', 'circuit_open_until');
   "
   ```

4. **Verify Recovery**:
   ```bash
   # Check status
   curl https://vod-api.example.com/status
   
   # Monitor logs
   docker logs -f vod-api
   ```

**Prevention**:
- Adjust `CIRCUIT_FAILURE_THRESHOLD` if too sensitive
- Implement better error handling for transient failures
- Add retry logic with exponential backoff
- Alert on circuit breaker state changes

---

### Chat Recorder Disconnected

**Symptoms**:
- No new chat messages in database
- Logs show "chat recorder stopped" or "IRC disconnected"
- Auto mode not detecting live streams

**Diagnosis**:

```bash
# Check chat recorder status in logs
docker logs vod-api | grep -i chat

# Verify IRC credentials
docker exec vod-api env | grep TWITCH

# Test IRC connection manually
docker exec -it vod-api telnet irc.chat.twitch.tv 6667

# Check Helix API access
curl -H "Authorization: Bearer <token>" \
  -H "Client-Id: <client_id>" \
  "https://api.twitch.tv/helix/streams?user_login=<channel>"

# Check database for recent chat
docker exec vod-postgres psql -U vod -d vod -c "
  SELECT vod_id, COUNT(*), MAX(timestamp) 
  FROM chat_messages 
  GROUP BY vod_id 
  ORDER BY MAX(timestamp) DESC 
  LIMIT 5;
"
```

**Resolution**:

1. **Verify Credentials**:
   ```bash
   # Test Twitch OAuth token
   curl -H "Authorization: Bearer <token>" \
     https://id.twitch.tv/oauth2/validate
   
   # Check required scopes: chat:read, chat:edit
   
   # If expired, refresh or regenerate
   # Update secret and restart
   ```

2. **Check Auto Mode Configuration**:
   ```bash
   # Verify environment variables
   docker exec vod-api env | grep CHAT_AUTO
   # Should show CHAT_AUTO_START=1
   
   # Check Helix credentials
   docker exec vod-api env | grep TWITCH_CLIENT
   ```

3. **Restart Chat Recorder**:
   ```bash
   # Full application restart
   docker restart vod-api
   kubectl rollout restart deployment/vod-api -n vod-tender
   
   # Monitor startup
   docker logs -f vod-api | grep chat
   ```

4. **Manual Chat Recording** (if auto mode fails):
   ```bash
   # Set manual mode temporarily
   # Update .env:
   # CHAT_AUTO_START=0
   # TWITCH_VOD_ID=<current-vod-id>
   # TWITCH_VOD_START=<stream-start-time>
   
   # Restart application
   ```

**Prevention**:
- Monitor chat message rate (should be > 0 when live)
- Alert on IRC disconnections
- Implement automatic reconnection logic
- Rotate OAuth tokens before expiry

---

### Failed Downloads

**Symptoms**:
- VODs stuck in processing state
- Logs show yt-dlp errors
- Download progress not updating

**Diagnosis**:

```bash
# Check VOD status
curl https://vod-api.example.com/status

# Query failed downloads
docker exec vod-postgres psql -U vod -d vod -c "
  SELECT twitch_vod_id, download_state, processing_error 
  FROM vods 
  WHERE processing_error IS NOT NULL 
  ORDER BY updated_at DESC 
  LIMIT 10;
"

# Check yt-dlp logs
docker logs vod-api | grep yt-dlp | tail -50

# Test yt-dlp manually
docker exec vod-api yt-dlp \
  --cookies /run/cookies/twitch-cookies.txt \
  https://www.twitch.tv/videos/<vod-id>

# Check network connectivity
docker exec vod-api curl -I https://www.twitch.tv
```

**Resolution**:

1. **Authentication Issues**:
   ```bash
   # Verify cookies file exists and is valid
   docker exec vod-api ls -l /run/cookies/
   
   # Test with fresh cookies
   # Export cookies from browser (logged in to Twitch)
   # Update secret
   kubectl create secret generic vod-cookies \
     --from-file=twitch-cookies.txt=./fresh-cookies.txt \
     --dry-run=client -o yaml | kubectl apply -f -
   
   # Restart pod
   kubectl rollout restart deployment/vod-api -n vod-tender
   ```

2. **Network Issues**:
   ```bash
   # Check DNS resolution
   docker exec vod-api nslookup www.twitch.tv
   
   # Check firewall/egress rules
   # Verify outbound HTTPS (443) allowed
   
   # Test with aria2c (if installed)
   docker exec vod-api which aria2c
   ```

3. **VOD Availability**:
   ```bash
   # Check if VOD still exists
   # Some VODs are deleted or made sub-only
   
   # Mark as permanently failed
   docker exec vod-postgres psql -U vod -d vod -c "
     UPDATE vods 
     SET processed=true, processing_error='VOD unavailable' 
     WHERE twitch_vod_id='<vod-id>';
   "
   ```

4. **Retry Failed Download**:
   ```bash
   # Clear error to allow retry
   docker exec vod-postgres psql -U vod -d vod -c "
     UPDATE vods 
     SET processing_error=NULL, download_state='pending' 
     WHERE twitch_vod_id='<vod-id>';
   "
   ```

**Prevention**:
- Keep cookies fresh (< 30 days old)
- Monitor download success rate
- Implement better error categorization (transient vs permanent)
- Alert on consecutive download failures

---

### Upload Quota Exceeded

**Symptoms**:
- Logs show "quota exceeded" or "uploadLimitExceeded"
- Uploads failing to YouTube
- Processing completes but no YouTube URL

**Diagnosis**:

```bash
# Check recent upload errors
docker logs vod-api | grep -i upload | grep -i error

# Query uploads
docker exec vod-postgres psql -U vod -d vod -c "
  SELECT twitch_vod_id, youtube_url, processing_error 
  FROM vods 
  WHERE processed=true AND youtube_url IS NULL 
  ORDER BY updated_at DESC 
  LIMIT 10;
"

# Check YouTube API quota usage
# Visit: https://console.cloud.google.com/apis/api/youtube.googleapis.com/quotas
```

**Resolution**:

1. **Immediate Mitigation**:
   ```bash
   # Disable uploads temporarily
   # Update environment variable
   # YT_ENABLED=0
   
   # Or remove YouTube credentials
   # This will skip upload step
   ```

2. **Request Quota Increase**:
   - Visit Google Cloud Console
   - Navigate to YouTube Data API v3 quotas
   - Request quota increase (justification required)
   - Typical default: 10,000 units/day
   - Upload video: ~1,600 units

3. **Implement Rate Limiting**:
   ```bash
   # Set daily upload limit
   BACKFILL_UPLOAD_DAILY_LIMIT=5
   
   # Spread uploads across 24 hours
   # Limit: 5 uploads = 8,000 units (fits in 10k quota)
   ```

4. **Retry After Quota Reset**:
   ```bash
   # Quota resets at midnight Pacific Time
   # Clear errors to allow retry
   docker exec vod-postgres psql -U vod -d vod -c "
     UPDATE vods 
     SET processing_error=NULL 
     WHERE processing_error LIKE '%quota%';
   "
   ```

**Prevention**:
- Monitor quota usage daily
- Implement upload scheduling
- Alert at 80% quota consumption
- Consider multiple YouTube channels for distribution

---

## Monitoring Setup Guide

### Prometheus Configuration

**Scrape Config** (`prometheus.yml`):

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'vod-tender'
    static_configs:
      - targets: ['vod-api.vod-tender.svc.cluster.local:8080']
    metrics_path: '/metrics'
    scheme: http
```

**Kubernetes ServiceMonitor**:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: vod-tender
  namespace: vod-tender
  labels:
    app: vod-tender
spec:
  selector:
    matchLabels:
      app: vod-api
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
```

### Grafana Dashboard

Import the dashboard from `docs/grafana-dashboard.json` or create manually:

**Key Panels**:

1. **VOD Queue Depth** (Gauge)
   ```promql
   vod_queue_depth
   ```

2. **Download Throughput** (Graph)
   ```promql
   rate(vod_downloads_succeeded_total[5m])
   ```

3. **Circuit Breaker State** (Stat)
   ```promql
   vod_circuit_open
   ```

4. **Error Rate** (Graph)
   ```promql
   rate(vod_downloads_failed_total[5m]) + 
   rate(vod_uploads_failed_total[5m])
   ```

5. **Download Duration** (Heatmap)
   ```promql
   vod_download_duration_seconds_bucket
   ```

6. **API Request Latency** (Heatmap)
   ```promql
   http_request_duration_seconds_bucket{job="vod-tender"}
   ```

**Dashboard JSON** (see `docs/grafana-dashboard.json` below)

### Alert Rule Examples

Create Prometheus alert rules:

```yaml
# /etc/prometheus/rules/vod-tender.yml
groups:
- name: vod_tender_alerts
  interval: 30s
  rules:
  
  - alert: VODTenderDown
    expr: up{job="vod-tender"} == 0
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "VOD Tender service is down"
      description: "VOD Tender has been down for more than 2 minutes"
      
  - alert: HighQueueDepth
    expr: vod_queue_depth > 100
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "High VOD queue depth"
      description: "Queue has {{ $value }} unprocessed VODs"
      
  - alert: CircuitBreakerOpen
    expr: vod_circuit_open == 1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Circuit breaker is open"
      description: "Processing is halted due to repeated failures"
      
  - alert: HighFailureRate
    expr: |
      rate(vod_downloads_failed_total[10m]) / 
      rate(vod_downloads_started_total[10m]) > 0.5
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "High download failure rate"
      description: "{{ $value | humanizePercentage }} of downloads are failing"
      
  - alert: DatabaseConnectionIssues
    expr: |
      rate(vod_db_errors_total[5m]) > 0.1
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Database connection errors detected"
      
  - alert: DiskSpaceRunningLow
    expr: |
      (node_filesystem_avail_bytes{mountpoint="/data"} / 
       node_filesystem_size_bytes{mountpoint="/data"}) < 0.2
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Disk space below 20%"
      description: "Only {{ $value | humanizePercentage }} space remaining"
      
  - alert: SSLCertificateExpiringSoon
    expr: |
      (ssl_cert_not_after - time()) / 86400 < 30
    for: 1d
    labels:
      severity: warning
    annotations:
      summary: "SSL certificate expires in {{ $value }} days"
```

### Alertmanager Configuration

Route alerts to appropriate channels:

```yaml
# /etc/alertmanager/config.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10m
  repeat_interval: 12h
  receiver: 'default'
  routes:
  - match:
      severity: critical
    receiver: 'pagerduty'
  - match:
      severity: warning
    receiver: 'slack'

receivers:
- name: 'default'
  email_configs:
  - to: 'ops@example.com'
    from: 'alertmanager@example.com'
    smarthost: 'smtp.example.com:587'
    auth_username: 'alertmanager'
    auth_password: '<password>'
    
- name: 'slack'
  slack_configs:
  - api_url: 'https://hooks.slack.com/services/XXX/YYY/ZZZ'
    channel: '#vod-tender-alerts'
    title: '{{ .GroupLabels.alertname }}'
    text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
    
- name: 'pagerduty'
  pagerduty_configs:
  - service_key: '<pagerduty-integration-key>'
```

### Log Aggregation

**Loki Setup** (Kubernetes):

```yaml
# promtail-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: promtail-config
  namespace: vod-tender
data:
  promtail.yaml: |
    server:
      http_listen_port: 9080
    positions:
      filename: /tmp/positions.yaml
    clients:
      - url: http://loki:3100/loki/api/v1/push
    scrape_configs:
      - job_name: vod-tender
        kubernetes_sd_configs:
        - role: pod
          namespaces:
            names:
            - vod-tender
        relabel_configs:
        - source_labels: [__meta_kubernetes_pod_label_app]
          target_label: app
        - source_labels: [__meta_kubernetes_pod_name]
          target_label: pod
        pipeline_stages:
        - json:
            expressions:
              level: level
              component: component
              message: msg
        - labels:
            level:
            component:
```

**ELK Setup** (Elasticsearch, Logstash, Kibana):

```yaml
# filebeat-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: filebeat-config
data:
  filebeat.yml: |
    filebeat.inputs:
    - type: container
      paths:
        - /var/log/containers/*vod-tender*.log
      json.keys_under_root: true
      json.overwrite_keys: true
    
    output.elasticsearch:
      hosts: ["elasticsearch:9200"]
      index: "vod-tender-%{+yyyy.MM.dd}"
    
    setup.kibana:
      host: "kibana:5601"
```

## Backup and Restore Procedures

### Database Backup

**Automated Daily Backup**:

```bash
#!/bin/bash
# /opt/vod-tender/bin/backup-db.sh

set -euo pipefail

BACKUP_DIR=/opt/vod-tender/backups
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RETENTION_DAYS=30

# Create backup
pg_dump -U vod -h localhost vod | gzip > "${BACKUP_DIR}/vod_${TIMESTAMP}.sql.gz"

# Upload to S3
aws s3 cp "${BACKUP_DIR}/vod_${TIMESTAMP}.sql.gz" \
  s3://vod-tender-backups/database/ \
  --storage-class STANDARD_IA

# Remove old local backups
find "${BACKUP_DIR}" -name "vod_*.sql.gz" -mtime +${RETENTION_DAYS} -delete

# Remove old S3 backups (lifecycle policy preferred)
aws s3 ls s3://vod-tender-backups/database/ \
  | awk -v date="$(date -d "${RETENTION_DAYS} days ago" +%Y-%m-%d)" '$1 < date {print $4}' \
  | xargs -I {} aws s3 rm s3://vod-tender-backups/database/{}

echo "Backup completed: vod_${TIMESTAMP}.sql.gz"
```

**Kubernetes CronJob**:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: vod-db-backup
  namespace: vod-tender
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:16-alpine
            env:
            - name: PGHOST
              value: postgres
            - name: PGUSER
              value: vod
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: vod-postgres-creds
                  key: DB_PASSWORD
            - name: PGDATABASE
              value: vod
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: aws-creds
                  key: access_key_id
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: aws-creds
                  key: secret_access_key
            command:
            - /bin/sh
            - -c
            - |
              apk add --no-cache aws-cli
              TIMESTAMP=$(date +%Y%m%d_%H%M%S)
              pg_dump | gzip | aws s3 cp - s3://vod-tender-backups/database/vod_${TIMESTAMP}.sql.gz
          restartPolicy: OnFailure
```

### Database Restore

**From local backup**:

```bash
# Stop application
docker stop vod-api
kubectl scale deployment/vod-api --replicas=0 -n vod-tender

# Restore database
zcat /opt/vod-tender/backups/vod_20251020_020000.sql.gz | \
  psql -U vod -h localhost vod

# Verify restoration
psql -U vod -h localhost vod -c "SELECT COUNT(*) FROM vods;"

# Start application
docker start vod-api
kubectl scale deployment/vod-api --replicas=1 -n vod-tender
```

**From S3 backup**:

```bash
# Download backup
aws s3 cp s3://vod-tender-backups/database/vod_20251020_020000.sql.gz /tmp/

# Restore
zcat /tmp/vod_20251020_020000.sql.gz | psql -U vod -h localhost vod
```

**Point-in-Time Recovery** (if WAL archiving enabled):

```bash
# Stop PostgreSQL
systemctl stop postgresql

# Restore base backup
rm -rf /var/lib/postgresql/data/*
tar -xzf /backups/base-backup.tar.gz -C /var/lib/postgresql/data/

# Create recovery.signal
touch /var/lib/postgresql/data/recovery.signal

# Configure recovery target
cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
restore_command = 'cp /backups/wal/%f %p'
recovery_target_time = '2025-10-20 12:00:00'
EOF

# Start PostgreSQL (will replay WAL logs)
systemctl start postgresql

# Verify
psql -U vod -d vod -c "SELECT NOW();"
```

### VOD File Backup

**Backup Strategy**:

- Keep recent VODs (< 7 days) on fast local storage
- Move older VODs to archive storage (S3 Glacier, GCS Archive)
- Retain uploaded VODs for 30 days before deletion

**Automated Archival**:

```bash
#!/bin/bash
# /opt/vod-tender/bin/archive-vods.sh

set -euo pipefail

DATA_DIR=/opt/vod-tender/data
ARCHIVE_AGE_DAYS=7

# Find VODs older than 7 days with successful YouTube upload
docker exec vod-postgres psql -U vod -d vod -At -c "
  SELECT twitch_vod_id 
  FROM vods 
  WHERE processed=true 
  AND youtube_url IS NOT NULL 
  AND created_at < NOW() - INTERVAL '${ARCHIVE_AGE_DAYS} days';
" | while read VOD_ID; do
  # Find file
  VOD_FILE=$(find "${DATA_DIR}" -name "*${VOD_ID}*" -type f)
  
  if [ -n "$VOD_FILE" ]; then
    # Upload to S3 Glacier
    aws s3 cp "${VOD_FILE}" \
      s3://vod-tender-archive/vods/ \
      --storage-class GLACIER
    
    # Remove local file
    rm "${VOD_FILE}"
    
    echo "Archived: ${VOD_FILE}"
  fi
done
```

### Restore Testing

**Monthly Restore Test**:

```bash
#!/bin/bash
# /opt/vod-tender/bin/test-restore.sh

set -euo pipefail

# Download latest backup
LATEST_BACKUP=$(aws s3 ls s3://vod-tender-backups/database/ | sort | tail -1 | awk '{print $4}')
aws s3 cp "s3://vod-tender-backups/database/${LATEST_BACKUP}" /tmp/

# Create test database
psql -U postgres -c "CREATE DATABASE vod_restore_test;"

# Restore
zcat "/tmp/${LATEST_BACKUP}" | psql -U postgres vod_restore_test

# Verify data integrity
RECORD_COUNT=$(psql -U postgres vod_restore_test -At -c "SELECT COUNT(*) FROM vods;")

if [ "$RECORD_COUNT" -gt 0 ]; then
  echo "✅ Restore test PASSED: ${RECORD_COUNT} records"
  psql -U postgres -c "DROP DATABASE vod_restore_test;"
  exit 0
else
  echo "❌ Restore test FAILED: No records found"
  exit 1
fi
```

**RTO/RPO Targets**:

- **RTO (Recovery Time Objective)**: 1 hour
- **RPO (Recovery Point Objective)**: 24 hours (daily backups)
- **Restore Test Frequency**: Monthly
- **Backup Retention**: 30 days (online), 1 year (archive)

## Maintenance Procedures

### Routine Maintenance Tasks

**Daily**:
- [ ] Review dashboard for anomalies
- [ ] Check disk space usage
- [ ] Verify backups completed successfully

**Weekly**:
- [ ] Review and clear old logs
- [ ] Check for security updates
- [ ] Review error logs and trends
- [ ] Verify monitoring alerts are working

**Monthly**:
- [ ] Test backup restoration
- [ ] Review and optimize database queries
- [ ] Update dependencies
- [ ] Review capacity and scaling needs
- [ ] Rotate non-critical credentials

**Quarterly**:
- [ ] Rotate database passwords
- [ ] Rotate OAuth tokens
- [ ] Review and update runbooks
- [ ] Conduct security audit
- [ ] Review and update disaster recovery plan

**Annually**:
- [ ] Conduct penetration testing
- [ ] Review and update SLAs
- [ ] Conduct disaster recovery drill
- [ ] Review and optimize costs
- [ ] Update documentation

### Updating the Application

**Zero-Downtime Update**:

```bash
# 1. Backup current state
/opt/vod-tender/bin/backup-db.sh

# 2. Pull new image
docker pull ghcr.io/subculture-collective/vod-tender:v2.0.0

# 3. Update service
docker service update \
  --image ghcr.io/subculture-collective/vod-tender:v2.0.0 \
  --update-parallelism 1 \
  --update-delay 10s \
  vod_api

# 4. Monitor rollout
docker service ps vod_api

# 5. Verify health
curl https://vod-api.example.com/healthz
```

### Database Maintenance

**Vacuum and Analyze**:

```bash
# Kubernetes
kubectl exec -it statefulset/postgres -n vod-tender -- \
  psql -U vod -d vod -c "VACUUM ANALYZE;"

# Docker
docker exec vod-postgres psql -U vod -d vod -c "VACUUM ANALYZE;"
```

**Reindex**:

```bash
psql -U vod -d vod -c "REINDEX DATABASE vod;"
```

**Check Table Sizes**:

```sql
SELECT 
  schemaname,
  tablename,
  pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

## Emergency Procedures

### Complete Service Outage

**Recovery Steps**:

1. **Assess Situation**:
   ```bash
   # Check all components
   kubectl get pods -n vod-tender
   docker ps
   systemctl status vod-tender
   
   # Check logs
   kubectl logs deployment/vod-api -n vod-tender --tail=100
   docker logs vod-api --tail=100
   journalctl -u vod-tender -n 100
   ```

2. **Restore Database** (if corrupted):
   ```bash
   # Stop application
   kubectl scale deployment/vod-api --replicas=0 -n vod-tender
   
   # Restore from latest backup
   aws s3 cp s3://vod-tender-backups/database/$(aws s3 ls s3://vod-tender-backups/database/ | sort | tail -1 | awk '{print $4}') - | \
     gunzip | \
     kubectl exec -i statefulset/postgres -n vod-tender -- psql -U vod vod
   
   # Start application
   kubectl scale deployment/vod-api --replicas=1 -n vod-tender
   ```

3. **Verify Recovery**:
   ```bash
   curl https://vod-api.example.com/healthz
   curl https://vod-api.example.com/status
   ```

### Data Corruption

**Symptoms**:
- Database integrity errors
- Inconsistent data
- Application crashes on queries

**Recovery**:

1. **Stop Application**:
   ```bash
   kubectl scale deployment/vod-api --replicas=0 -n vod-tender
   ```

2. **Backup Current State** (even if corrupted):
   ```bash
   pg_dump -U vod vod > /tmp/corrupted_backup.sql
   ```

3. **Check Corruption**:
   ```sql
   -- Check table integrity
   SELECT * FROM vods WHERE twitch_vod_id IS NULL;
   
   -- Check for duplicate IDs
   SELECT twitch_vod_id, COUNT(*) 
   FROM vods 
   GROUP BY twitch_vod_id 
   HAVING COUNT(*) > 1;
   
   -- Verify foreign key constraints
   SELECT conname, conrelid::regclass, confrelid::regclass
   FROM pg_constraint
   WHERE contype = 'f';
   ```

4. **Restore from Last Known Good Backup**:
   ```bash
   # Restore database
   aws s3 cp s3://vod-tender-backups/database/vod_20251019_020000.sql.gz - | \
     gunzip | psql -U vod vod
   ```

5. **Reprocess Recent Data**:
   ```bash
   # Mark recent VODs for reprocessing
   psql -U vod -d vod -c "
     UPDATE vods 
     SET processed=false, processing_error=NULL 
     WHERE created_at > NOW() - INTERVAL '24 hours';
   "
   ```

### Security Incident

**If Breach Suspected**:

1. **Isolate System**:
   ```bash
   # Block all incoming traffic
   kubectl apply -f - <<EOF
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: deny-all
     namespace: vod-tender
   spec:
     podSelector: {}
     policyTypes:
     - Ingress
     - Egress
   EOF
   ```

2. **Preserve Evidence**:
   ```bash
   # Capture logs
   kubectl logs deployment/vod-api -n vod-tender > /forensics/api-logs.txt
   
   # Capture network traffic (if tcpdump available)
   kubectl exec deployment/vod-api -n vod-tender -- tcpdump -w /tmp/traffic.pcap
   
   # Database dump
   pg_dump -U vod vod > /forensics/db-dump.sql
   ```

3. **Rotate All Credentials**:
   ```bash
   # Generate new passwords
   NEW_DB_PASS=$(openssl rand -base64 32)
   NEW_OAUTH=$(openssl rand -hex 32)
   
   # Update database
   psql -U postgres -c "ALTER ROLE vod PASSWORD '${NEW_DB_PASS}';"
   
   # Update secrets
   kubectl create secret generic vod-creds \
     --from-literal=DB_PASSWORD="${NEW_DB_PASS}" \
     --dry-run=client -o yaml | kubectl apply -f -
   
   # Restart application
   kubectl rollout restart deployment/vod-api -n vod-tender
   ```

4. **Notify Stakeholders**:
   - Security team
   - Management
   - Users (if PII exposed)
   - Regulatory authorities (if required)

5. **Post-Incident Review**:
   - Document timeline
   - Identify root cause
   - Implement fixes
   - Update runbooks

---

**Last Updated**: 2025-10-20  
**Maintained By**: Platform Team  
**Next Review**: 2025-11-20
