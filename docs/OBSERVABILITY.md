# Observability Guide

This document describes the comprehensive observability features in vod-tender, including distributed tracing, metrics, alerting, dashboards, and profiling.

## Overview

vod-tender provides production-grade observability through:

- **Distributed Tracing** (OpenTelemetry + Jaeger)
- **Metrics** (Prometheus)
- **Alerting** (Prometheus Alertmanager)
- **Dashboards** (Grafana)
- **Performance Profiling** (pprof)
- **Structured Logging** (slog with correlation IDs)

## Distributed Tracing

### Setup

Tracing is enabled by setting the `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable. In Docker Compose, Jaeger is automatically configured:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317
```

### Accessing Jaeger UI

Jaeger UI is available at `http://localhost:16686` when running via Docker Compose.

### Instrumented Operations

The following operations are automatically traced:

1. **HTTP Requests**
   - Span: `http-server`
   - Attributes: `http.method`, `http.route`, `http.url`, `http.status_code`, `correlation_id`

2. **VOD Processing**
   - Span: `processOnce`
   - Attributes: `vod.id`, `vod.title`, `vod.date`, `queue_depth`, `circuit.state`
   - Child spans: `download`, `upload`

3. **Download Operations**
   - Span: `download`
   - Attributes: `vod.id`, `vod.title`, `download.duration_ms`, `download.path`

4. **Upload Operations**
   - Span: `upload`
   - Attributes: `vod.id`, `vod.title`, `upload.path`, `upload.duration_ms`, `upload.youtube_url`

### Trace Correlation

All traces include correlation IDs (`correlation_id` attribute) that match the `X-Correlation-ID` HTTP header and the `corr` field in structured logs. This enables end-to-end request tracing across logs and traces.

### Example Queries

In Jaeger UI:

- **Find slow downloads**: Service=`vod-tender`, Operation=`download`, Min Duration=`30m`
- **Trace specific VOD**: Service=`vod-tender`, Tags=`vod.id=123456789`
- **Find failed uploads**: Service=`vod-tender`, Operation=`upload`, Tags=`error=true`

## Metrics

### Prometheus Setup

vod-tender exposes Prometheus-compatible metrics at the `/metrics` endpoint. An example Prometheus configuration is provided in `prometheus/prometheus.yml`.

#### Quick Start with Prometheus

1. **Using the provided configuration:**

   ```bash
   # Start Prometheus with the provided config
   prometheus --config.file=prometheus/prometheus.yml
   ```

2. **Docker Compose setup:**

   Add Prometheus to your `docker-compose.yml`:

   ```yaml
   prometheus:
     image: prom/prometheus:latest
     container_name: vod-prometheus
     restart: unless-stopped
     ports:
       - "9090:9090"
     volumes:
       - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
       - ./prometheus/alerts.yml:/etc/prometheus/alerts.yml:ro
       - prometheus_data:/prometheus
     command:
       - '--config.file=/etc/prometheus/prometheus.yml'
       - '--storage.tsdb.path=/prometheus'
       - '--web.console.libraries=/usr/share/prometheus/console_libraries'
       - '--web.console.templates=/usr/share/prometheus/consoles'
     networks:
       - web
   ```

3. **Configure the scrape target:**

   Edit `prometheus/prometheus.yml` and update the `targets` under `scrape_configs`:

   - **Local development:** `localhost:8080`
   - **Docker Compose:** `vod-api:8080` (use the service name)
   - **Production:** Your actual API hostname and port

4. **Verify metrics collection:**

   Access Prometheus UI at `http://localhost:9090` and check:
   - **Status → Targets** to verify vod-tender API is being scraped
   - **Graph** tab to query metrics like `vod_queue_depth`

#### Configuration Files

- **`prometheus/prometheus.yml`**: Main Prometheus configuration with scrape targets
- **`prometheus/alerts.yml`**: Alerting rules for common failure scenarios

### Available Metrics

#### VOD Processing

- `vod_downloads_started_total` - Total downloads started
- `vod_downloads_succeeded_total` - Successful downloads
- `vod_downloads_failed_total` - Failed downloads
- `vod_uploads_succeeded_total` - Successful uploads
- `vod_uploads_failed_total` - Failed uploads
- `vod_processing_cycles_total` - Processing cycles executed
- `vod_queue_depth` - Current unprocessed VOD count
- `vod_circuit_open` - Circuit breaker state (1=open, 0=closed)

#### Duration Metrics

- `vod_download_duration_seconds` - Download duration histogram
- `vod_upload_duration_seconds` - Upload duration histogram
- `vod_processing_total_duration_seconds` - Total processing duration histogram
- `vod_processing_step_duration_seconds{step="download|upload|total"}` - Step-level durations

#### Chat Metrics

- `chat_messages_recorded_total{channel="..."}` - Chat messages recorded per channel
- `chat_reconnections_total` - Total chat reconnections

#### OAuth Metrics

- `oauth_token_refresh_total{provider="twitch|youtube",status="success|failure"}` - Token refresh attempts

#### API Metrics

- `helix_api_calls_total{endpoint="...",status="..."}` - Twitch Helix API call counts

#### Circuit Breaker Metrics

- `circuit_breaker_state_changes_total{from="...",to="..."}` - State transitions

#### Database Metrics

- `database_connection_pool_size` - Maximum pool size
- `database_connection_pool_in_use` - Active connections

### Histogram Buckets

Duration histograms use realistic buckets for VOD operations:

- **Downloads**: 1m, 5m, 10m, 30m, 1h, 2h
- **Uploads**: 30s, 1m, 2m, 5m, 10m, 30m
- **Total Processing**: 1m, 5m, 15m, 30m, 1h, 2h

### PromQL Query Examples

Duration metrics are exposed as Prometheus histograms, enabling rich statistical queries. The `/status` endpoint also retains backward-compatible EMA fields (`avg_download_ms`, `avg_upload_ms`, `avg_total_ms`) for legacy integrations.

#### Percentiles

Calculate percentile durations over the last hour:

```promql
# 95th percentile download duration
histogram_quantile(0.95, rate(vod_download_duration_seconds_bucket[1h]))

# 50th percentile (median) upload duration
histogram_quantile(0.50, rate(vod_upload_duration_seconds_bucket[1h]))

# 99th percentile total processing duration
histogram_quantile(0.99, rate(vod_processing_total_duration_seconds_bucket[1h]))
```

#### Average Duration

Calculate average duration over time windows:

```promql
# Average download duration (last 5m)
rate(vod_download_duration_seconds_sum[5m]) / rate(vod_download_duration_seconds_count[5m])

# Average upload duration (last 1h)
rate(vod_upload_duration_seconds_sum[1h]) / rate(vod_upload_duration_seconds_count[1h])

# Average total processing duration (last 30m)
rate(vod_processing_total_duration_seconds_sum[30m]) / rate(vod_processing_total_duration_seconds_count[30m])
```

#### Distribution Analysis

Analyze how durations are distributed across buckets:

```promql
# Percentage of downloads completing within 10 minutes
sum(rate(vod_download_duration_seconds_bucket{le="600"}[1h])) / sum(rate(vod_download_duration_seconds_bucket{le="+Inf"}[1h])) * 100

# Percentage of uploads completing within 2 minutes
sum(rate(vod_upload_duration_seconds_bucket{le="120"}[1h])) / sum(rate(vod_upload_duration_seconds_bucket{le="+Inf"}[1h])) * 100

# Count of processing cycles exceeding 1 hour
sum(increase(vod_processing_total_duration_seconds_bucket{le="+Inf"}[1h])) - sum(increase(vod_processing_total_duration_seconds_bucket{le="3600"}[1h]))
```

#### Trend Detection

Detect performance degradation over time:

```promql
# Download duration trend (compare current hour vs previous hour)
histogram_quantile(0.95, rate(vod_download_duration_seconds_bucket[1h])) 
  / 
histogram_quantile(0.95, rate(vod_download_duration_seconds_bucket[1h] offset 1h))

# Upload throughput (operations per minute)
rate(vod_upload_duration_seconds_count[5m]) * 60
```

#### Processing Rate

Calculate processing throughput:

```promql
# Downloads per hour
rate(vod_downloads_started_total[1h]) * 3600

# Successful uploads per minute
rate(vod_uploads_succeeded_total[5m]) * 60

# Processing cycles per hour
rate(vod_processing_cycles_total[1h]) * 3600
```

#### Success Rate

Monitor operation success rates:

```promql
# Download success rate (percentage)
sum(rate(vod_downloads_succeeded_total[5m])) / sum(rate(vod_downloads_started_total[5m])) * 100

# Upload success rate (percentage)
sum(rate(vod_uploads_succeeded_total[5m])) / (sum(rate(vod_uploads_succeeded_total[5m])) + sum(rate(vod_uploads_failed_total[5m]))) * 100
```

#### Step-Level Durations

Analyze individual processing steps:

```promql
# 95th percentile by step
histogram_quantile(0.95, rate(vod_processing_step_duration_seconds_bucket[1h]))

# Average duration by step (grouped)
rate(vod_processing_step_duration_seconds_sum[30m]) / rate(vod_processing_step_duration_seconds_count[30m])
```

#### Grafana Variables

Use these queries in Grafana for dashboard variables:

```promql
# Auto-populate step types
label_values(vod_processing_step_duration_seconds_bucket, step)

# Note: For percentile dropdowns, use a static list (0.50, 0.95, 0.99) rather than
# querying bucket boundaries, as label_values() includes '+Inf' which is not suitable
# for percentile calculations.
```

### Migration from EMA to Histograms

**Legacy EMA Fields**: The `/status` endpoint continues to return `avg_download_ms`, `avg_upload_ms`, and `avg_total_ms` for backward compatibility. These are exponential moving averages stored in the `kv` table.

**Recommended Migration**:
- **New dashboards and alerts** should use histogram-based PromQL queries for richer insights (percentiles, distribution analysis)
- **Existing integrations** relying on `/status` fields will continue to work without changes
- **Future deprecation**: EMA fields may be deprecated in a future major version once all integrations migrate to histogram queries

**Advantages of Histograms**:
- Percentile calculation (p50, p95, p99)
- Distribution analysis across custom time windows
- Aggregation across multiple instances
- Standard Prometheus ecosystem integration (Grafana, alerting)

## Alerting

### Alert Rules

Alert rules are defined in `prometheus/alerts.yml`. Configure Prometheus to load them:

```yaml
# prometheus.yml
rule_files:
  - /etc/prometheus/alerts.yml
```

### Available Alerts

| Alert | Severity | Threshold | Description |
|-------|----------|-----------|-------------|
| `HighDownloadFailureRate` | warning | >50% failures for 10m | High VOD download failure rate |
| `CircuitBreakerOpen` | critical | Circuit open for 5m | Processing halted due to failures |
| `HighVODQueueDepth` | warning | >50 VODs for 30m | Large processing backlog |
| `DatabaseConnectionPoolExhausted` | warning | >90% for 5m | Connection pool near capacity |
| `VODProcessingStalled` | warning | 0 cycles for 15m | No processing activity |
| `OAuthTokenRefreshFailures` | warning | Failures for 10m | OAuth refresh issues |
| `HighChatReconnectionRate` | warning | >0.1/s for 10m | Frequent chat disconnections |
| `SlowVODDownloads` | info | p95 >1h for 30m | Unusually slow downloads |

### Configuring Alertmanager

Example Alertmanager configuration:

```yaml
# alertmanager.yml
route:
  receiver: 'team-vod-tender'
  group_by: ['alertname', 'severity']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h

receivers:
  - name: 'team-vod-tender'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
        channel: '#vod-alerts'
        title: '{{ .GroupLabels.severity | toUpper }}: {{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

## Dashboards

### Grafana Setup

The Grafana dashboard is located at `grafana/dashboards/vod-tender.json` and provides comprehensive visualization of vod-tender metrics.

#### Quick Start with Docker Compose

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
```

The dashboard will be automatically provisioned at startup. Access Grafana at `http://localhost:3000` (default credentials: `admin/admin`).

#### Manual Import to Existing Grafana

If you have an existing Grafana instance:

1. **Add Prometheus datasource:**
   - Navigate to **Configuration → Data Sources**
   - Click **Add data source**
   - Select **Prometheus**
   - Set URL to your Prometheus instance (e.g., `http://localhost:9090`)
   - Click **Save & Test**

2. **Import the dashboard:**
   - Navigate to **Dashboards → Import**
   - Click **Upload JSON file**
   - Select `grafana/dashboards/vod-tender.json`
   - Choose your Prometheus datasource
   - Click **Import**

3. **Configure dashboard settings:**
   - Set **Refresh interval** to `30s` (recommended)
   - Adjust time range as needed (default: Last 6 hours)

### Dashboard Panels

The vod-tender dashboard includes the following panels:

1. **VOD Queue Depth** (Gauge)
   - Real-time count of unprocessed VODs
   - Metric: `vod_queue_depth`
   - Thresholds: Green (<50), Yellow (50-100), Red (>100)

2. **Circuit Breaker State** (Gauge)
   - Current circuit breaker status
   - Metric: `vod_circuit_breaker_state`
   - Values: 0=closed (green), 1=half-open (yellow), 2=open (red)

3. **Download Rate** (Time Series)
   - Downloads started, succeeded, and failed per second
   - Metrics: `rate(vod_downloads_started_total[5m])`, `rate(vod_downloads_succeeded_total[5m])`, `rate(vod_downloads_failed_total[5m])`

4. **Download Duration Percentiles** (Time Series)
   - p50, p95, p99 download times over 1-hour windows
   - Metric: `histogram_quantile(0.95, rate(vod_download_duration_seconds_bucket[1h]))`

5. **Upload Rate** (Time Series)
   - Upload success and failure rates
   - Metrics: `rate(vod_uploads_succeeded_total[5m])`, `rate(vod_uploads_failed_total[5m])`

6. **Upload Duration Percentiles** (Time Series)
   - p50, p95, p99 upload times
   - Metric: `histogram_quantile(0.95, rate(vod_upload_duration_seconds_bucket[1h]))`

7. **Database Connection Pool** (Time Series)
   - Pool size vs. active connections
   - Metrics: `database_connection_pool_size`, `database_connection_pool_in_use`

8. **Chat Messages Recorded Rate** (Time Series)
   - Messages recorded per second by channel
   - Metric: `rate(chat_messages_recorded_total[5m])`

9. **OAuth Token Refresh Rate** (Time Series)
   - Token refresh attempts by provider and status
   - Metric: `rate(oauth_token_refresh_total[5m])`

10. **Helix API Call Rate** (Time Series)
    - Twitch API calls by endpoint and status
    - Metric: `rate(helix_api_calls_total[5m])`

### Expected Metrics

The dashboard expects the following metrics to be available from the `/metrics` endpoint:

| Metric | Type | Description |
|--------|------|-------------|
| `vod_queue_depth` | Gauge | Current unprocessed VOD count |
| `vod_circuit_breaker_state` | Gauge | Circuit breaker state (0=closed, 1=half-open, 2=open) |
| `vod_downloads_started_total` | Counter | Total downloads started |
| `vod_downloads_succeeded_total` | Counter | Total successful downloads |
| `vod_downloads_failed_total` | Counter | Total failed downloads |
| `vod_download_duration_seconds` | Histogram | Download duration distribution |
| `vod_uploads_succeeded_total` | Counter | Total successful uploads |
| `vod_uploads_failed_total` | Counter | Total failed uploads |
| `vod_upload_duration_seconds` | Histogram | Upload duration distribution |
| `database_connection_pool_size` | Gauge | Maximum connection pool size |
| `database_connection_pool_in_use` | Gauge | Active database connections |
| `chat_messages_recorded_total` | Counter (with `channel` label) | Chat messages recorded |
| `oauth_token_refresh_total` | Counter (with `provider`, `status` labels) | OAuth refresh attempts |
| `helix_api_calls_total` | Counter (with `endpoint`, `status` labels) | Helix API calls |

All metrics are implemented in `backend/telemetry/metrics.go` and automatically exposed at the `/metrics` endpoint when the API server starts.

## Performance Profiling

### Enabling pprof

Set the `ENABLE_PPROF` environment variable:

```bash
ENABLE_PPROF=1
PPROF_ADDR=localhost:6060  # Optional, default: localhost:6060
```

⚠️ **Security Warning**: Only enable pprof in development or controlled environments. Do not expose pprof endpoints publicly.

### Available Profiles

pprof endpoints are available at the configured address:

- `http://localhost:6060/debug/pprof/` - Index of available profiles
- `http://localhost:6060/debug/pprof/heap` - Memory allocation profile
- `http://localhost:6060/debug/pprof/goroutine` - Goroutine stack traces
- `http://localhost:6060/debug/pprof/threadcreate` - Thread creation profile
- `http://localhost:6060/debug/pprof/block` - Blocking profile
- `http://localhost:6060/debug/pprof/mutex` - Mutex contention profile

### Using pprof

#### CPU Profile

Capture a 30-second CPU profile:

```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

In the interactive pprof shell:

```
(pprof) top10        # Show top 10 functions by CPU time
(pprof) list funcName # Show source code with CPU time annotations
(pprof) web          # Generate and open a graph visualization
```

#### Memory Profile

Analyze heap allocations:

```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

Common commands:

```
(pprof) top10 -cum   # Top 10 by cumulative memory
(pprof) list funcName # Memory allocations in function
(pprof) alloc_objects # Sort by number of objects
```

#### Goroutine Profile

Check for goroutine leaks:

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

#### Flame Graphs

Generate flame graphs for better visualization:

```bash
# Install go-torch (if not already installed)
go install github.com/uber/go-torch@latest

# Generate flame graph
go-torch --url http://localhost:6060/debug/pprof/profile --seconds 30
```

### Docker Compose Profiling

When profiling in Docker Compose, forward the pprof port:

```yaml
# docker-compose.yml
api:
  ports:
    - "6060:6060"
  environment:
    ENABLE_PPROF: "1"
    PPROF_ADDR: "0.0.0.0:6060"
```

Then access from host:

```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

## Health Endpoints

### Liveness Probe (`/healthz`)

Checks if the process is alive and database is reachable:

```bash
curl http://localhost:8080/healthz
# Response: ok (200) or unhealthy (503)
```

Use for Kubernetes liveness probes.

### Readiness Probe (`/readyz`)

Checks if the service can handle traffic:

```bash
curl http://localhost:8080/readyz
# Response: {"status":"ready"} (200)
# or {"status":"not_ready","failed_check":"...","error":"..."} (503)
```

Readiness checks:

1. Database connectivity
2. Circuit breaker state (not open)
3. OAuth credentials present

Use for Kubernetes readiness probes and load balancer health checks.

## Log Correlation

### Correlation IDs

Every HTTP request gets a unique correlation ID:

- **HTTP Header**: `X-Correlation-ID`
- **Log Field**: `corr`
- **Trace Attribute**: `correlation_id`

### Searching Logs

Find all logs for a specific request:

```bash
# With structured logging (JSON)
cat logs.json | jq 'select(.corr == "abc-123-def")'

# With grep
grep 'corr=abc-123-def' logs.txt
```

### Tracing from Logs

1. Extract correlation ID from log entry
2. Search Jaeger UI with tag: `correlation_id=abc-123-def`
3. View full request trace with spans

## Best Practices

### Development

1. **Enable tracing locally** to understand request flows
2. **Use correlation IDs** when reporting issues
3. **Check metrics** before and after code changes
4. **Profile suspicious code** with pprof

### Production

1. **Set up alerting** via Alertmanager to Slack/PagerDuty
2. **Monitor dashboards** for trends and anomalies
3. **Sample traces** (adjust `AlwaysSample` to probabilistic sampling for high traffic)
4. **Rotate logs** to prevent disk exhaustion
5. **Secure pprof** endpoints or disable entirely

### Troubleshooting

| Symptom | Check | Action |
|---------|-------|--------|
| Slow processing | Jaeger traces, download duration metrics | Investigate slow downloads/uploads |
| High error rate | `vod_downloads_failed_total` metric | Check processing logs, circuit breaker |
| Circuit breaker open | `/status` endpoint, circuit alerts | Review recent failures, check credentials |
| Memory leak | pprof heap profile | Identify allocation hotspots |
| High CPU | pprof CPU profile | Find CPU-intensive functions |

## Configuration Reference

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | - | OpenTelemetry collector endpoint (e.g., `jaeger:4317`) |
| `ENABLE_PPROF` | `0` | Enable pprof profiling endpoints (`1` to enable) |
| `PPROF_ADDR` | `localhost:6060` | Address for pprof server |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | Log format: `text` or `json` |

See [CONFIG.md](CONFIG.md) for full configuration documentation.
