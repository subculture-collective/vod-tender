# Grafana Dashboards for vod-tender

This directory contains Grafana dashboard configurations and provisioning files for vod-tender.

## Directory Structure

```
grafana/
├── dashboards/
│   └── vod-tender.json       # Main operations dashboard
├── provisioning/
│   └── dashboards/
│       └── dashboard.yml     # Dashboard provisioning config
└── README.md                 # This file
```

## Quick Start

### Using Docker Compose

Add Grafana to your `docker-compose.yml`:

```yaml
grafana:
  image: grafana/grafana:latest
  container_name: vod-grafana
  restart: unless-stopped
  ports:
    - "3000:3000"
  volumes:
    - ./grafana/dashboards:/etc/grafana/dashboards:ro
    - ./grafana/provisioning:/etc/grafana/provisioning:ro
    - grafana_data:/var/lib/grafana
  environment:
    - GF_SECURITY_ADMIN_PASSWORD=admin  # Change in production!
    - GF_USERS_ALLOW_SIGN_UP=false
  networks:
    - web

volumes:
  grafana_data:
```

The dashboard will be automatically provisioned on startup. Access at `http://localhost:3000` (default: `admin/admin`).

## Manual Import

If you have an existing Grafana instance:

### Step 1: Add Prometheus Datasource

1. Navigate to **Configuration → Data Sources**
2. Click **Add data source**
3. Select **Prometheus**
4. Configure:
   - **Name**: `Prometheus` (must match dashboard datasource reference)
   - **URL**: `http://localhost:9090` (or your Prometheus URL)
5. Click **Save & Test**

### Step 2: Import Dashboard

1. Navigate to **Dashboards → Import**
2. Click **Upload JSON file**
3. Select `grafana/dashboards/vod-tender.json`
4. Select your Prometheus datasource from the dropdown
5. Click **Import**

### Step 3: Configure Settings

- Set **Refresh interval**: `30s` (recommended)
- Set **Time range**: Last 6 hours (or as needed)

## Dashboard Panels

The vod-tender dashboard provides 10 comprehensive panels:

### 1. VOD Queue Depth (Gauge)
- **Metric**: `vod_queue_depth`
- **Description**: Real-time count of unprocessed VODs
- **Thresholds**: Green (<50), Yellow (50-100), Red (>100)

### 2. Circuit Breaker State (Gauge)
- **Metric**: `vod_circuit_breaker_state`
- **Description**: Current circuit breaker status
- **States**: 0=closed (green), 1=half-open (yellow), 2=open (red)

### 3. Download Rate (Time Series)
- **Metrics**: 
  - `rate(vod_downloads_started_total[5m])`
  - `rate(vod_downloads_succeeded_total[5m])`
  - `rate(vod_downloads_failed_total[5m])`
- **Description**: Downloads per second by status

### 4. Download Duration Percentiles (Time Series)
- **Metrics**: `histogram_quantile(0.50|0.95|0.99, rate(vod_download_duration_seconds_bucket[1h]))`
- **Description**: p50, p95, p99 download times over 1-hour windows

### 5. Upload Rate (Time Series)
- **Metrics**:
  - `rate(vod_uploads_succeeded_total[5m])`
  - `rate(vod_uploads_failed_total[5m])`
- **Description**: Upload operations per second

### 6. Upload Duration Percentiles (Time Series)
- **Metrics**: `histogram_quantile(0.50|0.95|0.99, rate(vod_upload_duration_seconds_bucket[1h]))`
- **Description**: p50, p95, p99 upload times

### 7. Database Connection Pool (Time Series)
- **Metrics**:
  - `database_connection_pool_size`
  - `database_connection_pool_in_use`
- **Description**: Pool capacity vs. active connections

### 8. Chat Messages Recorded Rate (Time Series)
- **Metric**: `rate(chat_messages_recorded_total[5m])`
- **Description**: Chat messages per second by channel

### 9. OAuth Token Refresh Rate (Time Series)
- **Metric**: `rate(oauth_token_refresh_total[5m])`
- **Description**: Token refresh attempts by provider and status

### 10. Helix API Call Rate (Time Series)
- **Metric**: `rate(helix_api_calls_total[5m])`
- **Description**: Twitch Helix API calls by endpoint and status

## Required Metrics

The dashboard expects these metrics from the vod-tender `/metrics` endpoint:

| Metric | Type | Labels |
|--------|------|--------|
| `vod_queue_depth` | Gauge | - |
| `vod_circuit_breaker_state` | Gauge | - |
| `vod_downloads_started_total` | Counter | - |
| `vod_downloads_succeeded_total` | Counter | - |
| `vod_downloads_failed_total` | Counter | - |
| `vod_download_duration_seconds` | Histogram | - |
| `vod_uploads_succeeded_total` | Counter | - |
| `vod_uploads_failed_total` | Counter | - |
| `vod_upload_duration_seconds` | Histogram | - |
| `database_connection_pool_size` | Gauge | - |
| `database_connection_pool_in_use` | Gauge | - |
| `chat_messages_recorded_total` | Counter | `channel` |
| `oauth_token_refresh_total` | Counter | `provider`, `status` |
| `helix_api_calls_total` | Counter | `endpoint`, `status` |

All metrics are implemented in `backend/telemetry/metrics.go`.

## Customization

### Changing Time Windows

The dashboard uses these default time windows:

- **Rate calculations**: 5 minutes (`[5m]`)
- **Percentile calculations**: 1 hour (`[1h]`)

To change these, edit the dashboard JSON or modify queries in the Grafana UI.

### Adding Variables

You can add Grafana variables for filtering:

1. **Dashboard Settings → Variables → Add variable**
2. Common variables:
   - `channel`: For multi-channel deployments
   - `instance`: For multi-instance monitoring
   - `percentile`: For dynamic percentile selection (0.50, 0.95, 0.99)

### Creating Additional Panels

Common additional panels:

**Circuit Breaker Failure Count:**
```promql
vod_circuit_breaker_failures_total
```

**Processing Cycle Rate:**
```promql
rate(vod_processing_cycles_total[5m])
```

**Download Success Rate:**
```promql
sum(rate(vod_downloads_succeeded_total[5m])) / sum(rate(vod_downloads_started_total[5m])) * 100
```

## Troubleshooting

### "No data" on panels

1. **Check Prometheus datasource**:
   - Go to **Configuration → Data Sources**
   - Click on your Prometheus datasource
   - Click **Save & Test**
   - Verify "Data source is working"

2. **Verify metrics are being scraped**:
   - Open Prometheus UI at `http://localhost:9090`
   - Go to **Status → Targets**
   - Verify vod-tender target is "UP"
   - Query a metric like `vod_queue_depth` in the Graph tab

3. **Check time range**:
   - Ensure the dashboard time range includes recent data
   - Try "Last 15 minutes" or "Last 1 hour"

4. **Verify API is running**:
   ```bash
   curl http://localhost:8080/metrics
   ```

### Dashboard not auto-provisioned

If using Docker Compose and the dashboard doesn't appear:

1. Check container logs: `docker logs vod-grafana`
2. Verify volume mounts are correct in `docker-compose.yml`
3. Ensure `provisioning/dashboards/dashboard.yml` is present
4. Restart Grafana: `docker restart vod-grafana`

### Panels show "N/A"

This usually means the metric doesn't exist or has no data points:

1. Check if the vod-tender API has started processing VODs (some metrics only appear after activity)
2. Verify the metric name in the panel query matches the actual metric name
3. Check Prometheus for the metric: `http://localhost:9090/graph`

## Multi-Instance Monitoring

For monitoring multiple vod-tender instances (different channels):

1. **Label your Prometheus scrape configs** with `channel` label
2. **Use dashboard variables** to filter by channel
3. **Clone panels** and filter by specific instances if needed

Example variable query for channels:
```promql
label_values(vod_queue_depth, channel)
```

## See Also

- [OBSERVABILITY.md](../docs/OBSERVABILITY.md): Complete observability guide
- [Prometheus configuration](../prometheus/): Prometheus setup
- [Grafana documentation](https://grafana.com/docs/)
