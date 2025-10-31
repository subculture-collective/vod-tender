# Prometheus Configuration for vod-tender

This directory contains Prometheus configuration files for monitoring vod-tender.

## Files

- **`prometheus.yml`**: Main Prometheus configuration with scrape targets and storage settings
- **`alerts.yml`**: Alerting rules for common failure scenarios

## Quick Start

### Using Docker Compose

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
  networks:
    - web

volumes:
  prometheus_data:
```

### Standalone Prometheus

```bash
# Start Prometheus with this config
prometheus --config.file=prometheus.yml
```

## Configuration

### Scrape Target

The default configuration scrapes metrics from `localhost:8080`. Update the `targets` in `prometheus.yml` based on your deployment:

- **Local development**: `localhost:8080`
- **Docker Compose**: `vod-api:8080` (use service name)
- **Production**: Your actual hostname and port

### Multi-Instance Setup

For monitoring multiple vod-tender instances (multi-channel deployments), add additional job configurations:

```yaml
scrape_configs:
  - job_name: 'vod-tender-channel-a'
    static_configs:
      - targets: ['vod-api-channel-a:8080']
        labels:
          channel: 'channelA'

  - job_name: 'vod-tender-channel-b'
    static_configs:
      - targets: ['vod-api-channel-b:8080']
        labels:
          channel: 'channelB'
```

## Metrics Endpoint

vod-tender exposes Prometheus metrics at:

```
http://<host>:<port>/metrics
```

Default: `http://localhost:8080/metrics`

## Alert Rules

The `alerts.yml` file includes pre-configured alerts for:

- **HighDownloadFailureRate**: >50% download failures
- **CircuitBreakerOpen**: Processing circuit breaker triggered
- **HighVODQueueDepth**: Large backlog (>50 VODs)
- **DatabaseConnectionPoolExhausted**: >90% connections in use
- **VODProcessingStalled**: No processing activity despite pending VODs
- **OAuthTokenRefreshFailures**: Token refresh issues
- **HighChatReconnectionRate**: Frequent chat disconnections
- **SlowVODDownloads**: Unusually slow download times

## Verification

After starting Prometheus:

1. Access Prometheus UI at `http://localhost:9090`
2. Go to **Status → Targets** to verify vod-tender is being scraped
3. Go to **Graph** tab and query: `vod_queue_depth` to test metrics

## Integration with Alertmanager

To receive alert notifications, configure Alertmanager in `prometheus.yml`:

```yaml
alerting:
  alertmanagers:
    - static_configs:
        - targets:
          - alertmanager:9093
```

Then create an `alertmanager.yml` configuration. Example:

```yaml
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
```

## Troubleshooting

### Target Down

If Prometheus shows the vod-tender target as "DOWN":

1. Verify the API is running: `curl http://localhost:8080/healthz`
2. Check the metrics endpoint: `curl http://localhost:8080/metrics`
3. Verify network connectivity (especially in Docker networks)
4. Check Prometheus logs: `docker logs vod-prometheus`

### No Data in Grafana

1. Verify Prometheus is scraping: Check **Status → Targets** in Prometheus UI
2. Verify datasource in Grafana: **Configuration → Data Sources → Test**
3. Check dashboard queries are correct
4. Ensure time range is appropriate (recent data)

## See Also

- [OBSERVABILITY.md](../docs/OBSERVABILITY.md): Complete observability guide
- [Grafana dashboards](../grafana/dashboards/): Pre-built dashboards
- [Prometheus documentation](https://prometheus.io/docs/)
