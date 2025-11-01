# Logging Guide

This guide covers vod-tender's structured logging capabilities and how to integrate with common log aggregation stacks (Loki, ELK) for production monitoring and debugging.

## Table of Contents

- [Overview](#overview)
- [Log Format and Configuration](#log-format-and-configuration)
- [Log Fields Reference](#log-fields-reference)
- [Loki Integration](#loki-integration)
- [ELK Stack Integration](#elk-stack-integration)
- [Useful Queries](#useful-queries)
- [Docker Compose Examples](#docker-compose-examples)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Troubleshooting](#troubleshooting)

## Overview

vod-tender uses Go's structured logging library (`log/slog`) to produce JSON-formatted logs suitable for ingestion by modern observability platforms. Key features:

- **Structured JSON output**: Machine-parseable logs with consistent field names
- **Correlation IDs**: Every HTTP request gets a unique `X-Correlation-ID` that appears in all related logs
- **Component tagging**: Each log entry includes a `component` field for filtering (e.g., `vod_download`, `chat_recorder`, `http_server`)
- **Multiple log levels**: `debug`, `info`, `warn`, `error` for granular filtering
- **OpenTelemetry integration**: Correlation IDs also appear in distributed traces for end-to-end visibility

## Log Format and Configuration

### Environment Variables

Configure logging behavior via environment variables:

```bash
# Set log format (default: text)
LOG_FORMAT=json    # Options: text, json

# Set log level (default: info)
LOG_LEVEL=info     # Options: debug, info, warn, error
```

**Important**: Always use `LOG_FORMAT=json` in production for machine-parseable logs suitable for log aggregation systems.

### Text Format (Development)

Text format is human-readable and suitable for local development:

```
time=2025-11-01T12:34:56.789Z level=INFO msg="vod processing job starting" interval=30s channel=examplechannel
time=2025-11-01T12:35:01.234Z level=INFO msg="processing vod" vod_id=1234567890 title="Example Stream VOD"
time=2025-11-01T12:35:15.678Z level=INFO msg="download completed" vod_id=1234567890 duration_ms=14223 path=/data/1234567890.mp4
```

### JSON Format (Production)

JSON format provides structured data for log ingestion:

```json
{"time":"2025-11-01T12:34:56.789Z","level":"INFO","msg":"vod processing job starting","interval":"30s","channel":"examplechannel"}
{"time":"2025-11-01T12:35:01.234Z","level":"INFO","msg":"processing vod","vod_id":"1234567890","title":"Example Stream VOD","component":"vod_processor"}
{"time":"2025-11-01T12:35:15.678Z","level":"INFO","msg":"download completed","vod_id":"1234567890","duration_ms":14223,"path":"/data/1234567890.mp4","component":"vod_download"}
{"time":"2025-11-01T12:36:00.123Z","level":"ERROR","msg":"download failed","vod_id":"1234567891","err":"network timeout","component":"vod_download"}
```

## Log Fields Reference

### Common Fields

All log entries include these base fields:

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `time` | string (RFC3339) | Timestamp in UTC | `2025-11-01T12:34:56.789Z` |
| `level` | string | Log level | `INFO`, `WARN`, `ERROR`, `DEBUG` |
| `msg` | string | Human-readable message | `"processing vod"` |

### Component-Specific Fields

Different components add contextual fields:

#### HTTP Server

| Field | Description | Example |
|-------|-------------|---------|
| `corr` | Correlation ID for request tracing | `"abc-123-def-456"` |
| `method` | HTTP method | `"GET"`, `"POST"` |
| `path` | Request path | `"/api/vods"` |
| `status` | HTTP status code | `200`, `404`, `500` |
| `duration_ms` | Request duration in milliseconds | `45` |

Example:
```json
{"time":"2025-11-01T12:34:56Z","level":"INFO","msg":"http request","corr":"abc-123","method":"GET","path":"/api/vods","status":200,"duration_ms":45}
```

#### VOD Processing

| Field | Description | Example |
|-------|-------------|---------|
| `component` | Processing component | `"vod_processor"`, `"vod_download"`, `"vod_upload"` |
| `vod_id` | Twitch VOD ID | `"1234567890"` |
| `title` | VOD title | `"Example Stream"` |
| `channel` | Twitch channel | `"examplechannel"` |
| `duration_ms` | Operation duration | `14223` |
| `path` | File path | `"/data/1234567890.mp4"` |
| `youtube_url` | Uploaded YouTube URL | `"https://youtu.be/abc123"` |

Example:
```json
{"time":"2025-11-01T12:35:15Z","level":"INFO","msg":"download completed","component":"vod_download","vod_id":"1234567890","title":"Example Stream","duration_ms":14223,"path":"/data/1234567890.mp4"}
```

#### Chat Recorder

| Field | Description | Example |
|-------|-------------|---------|
| `component` | Chat component | `"chat_recorder"`, `"chat_auto"` |
| `channel` | Twitch channel | `"examplechannel"` |
| `vod_id` | Associated VOD ID | `"1234567890"` |
| `message_count` | Number of messages | `1523` |

Example:
```json
{"time":"2025-11-01T12:40:00Z","level":"INFO","msg":"chat recording stopped","component":"chat_recorder","channel":"examplechannel","vod_id":"1234567890","message_count":1523}
```

#### Circuit Breaker

| Field | Description | Example |
|-------|-------------|---------|
| `circuit_state` | Circuit breaker state | `"open"`, `"closed"`, `"half-open"` |
| `failures` | Failure count | `5` |
| `until` | Open until timestamp | `"2025-11-01T13:00:00Z"` |

Example:
```json
{"time":"2025-11-01T12:45:00Z","level":"WARN","msg":"circuit breaker opened","component":"vod_processor","circuit_state":"open","failures":5,"until":"2025-11-01T13:00:00Z"}
```

#### OAuth

| Field | Description | Example |
|-------|-------------|---------|
| `provider` | OAuth provider | `"twitch"`, `"youtube"` |
| `component` | OAuth component | `"oauth_refresh"` |
| `tail` | Last 6 chars of token (masked) | `"***abc123"` |

Example:
```json
{"time":"2025-11-01T12:50:00Z","level":"INFO","msg":"token refreshed","component":"oauth_refresh","provider":"twitch","tail":"***abc123"}
```

## Loki Integration

[Grafana Loki](https://grafana.com/oss/loki/) is a horizontally-scalable log aggregation system inspired by Prometheus. It's lightweight and cost-effective for Kubernetes environments.

### Architecture

```
vod-tender → stdout (JSON) → Promtail → Loki → Grafana (LogQL queries)
```

### Promtail Configuration

Promtail is the log shipper that reads container logs and forwards them to Loki.

#### Docker Compose Setup

Create `promtail/promtail.yml`:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  # Scrape logs from Docker containers
  - job_name: vod-tender
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 5s
    relabel_configs:
      # Only scrape vod-tender containers
      - source_labels: ['__meta_docker_container_label_com_docker_compose_service']
        regex: '(api|frontend)'
        action: keep
      
      # Extract service name
      - source_labels: ['__meta_docker_container_label_com_docker_compose_service']
        target_label: service
      
      # Extract container name
      - source_labels: ['__meta_docker_container_name']
        target_label: container
      
    pipeline_stages:
      # Parse JSON logs
      - json:
          expressions:
            level: level
            component: component
            msg: msg
            corr: corr
            vod_id: vod_id
            channel: channel
      
      # Convert level to lowercase for consistency
      - labels:
          level:
          component:
          service:
          container:
      
      # Extract additional fields as labels (be selective to avoid high cardinality)
      - labels:
          corr:
          channel:
```

Add Promtail and Loki to `docker-compose.yml`:

```yaml
services:
  # ... existing services ...

  loki:
    image: grafana/loki:2.9.3
    container_name: vod-loki
    restart: unless-stopped
    ports:
      - "3100:3100"
    volumes:
      - ./loki/loki.yml:/etc/loki/local-config.yaml:ro
      - loki_data:/loki
    command: -config.file=/etc/loki/local-config.yaml
    networks:
      - web

  promtail:
    image: grafana/promtail:2.9.3
    container_name: vod-promtail
    restart: unless-stopped
    volumes:
      - ./promtail/promtail.yml:/etc/promtail/config.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - promtail_positions:/tmp
    command: -config.file=/etc/promtail/config.yml
    depends_on:
      - loki
    networks:
      - web

volumes:
  loki_data:
  promtail_positions:
```

Create minimal `loki/loki.yml`:

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: 744h  # 31 days
```

#### Kubernetes Setup

Create `k8s/promtail-configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: promtail-config
  namespace: vod-tender
data:
  promtail.yaml: |
    server:
      http_listen_port: 9080
      grpc_listen_port: 0

    positions:
      filename: /tmp/positions.yaml

    clients:
      - url: http://loki:3100/loki/api/v1/push

    scrape_configs:
      - job_name: kubernetes-pods
        kubernetes_sd_configs:
        - role: pod
          namespaces:
            names:
            - vod-tender
        
        relabel_configs:
        # Only scrape pods with specific labels
        - source_labels: [__meta_kubernetes_pod_label_app]
          regex: vod-(api|frontend)
          action: keep
        
        # Extract pod name
        - source_labels: [__meta_kubernetes_pod_name]
          target_label: pod
        
        # Extract namespace
        - source_labels: [__meta_kubernetes_namespace]
          target_label: namespace
        
        # Extract container name
        - source_labels: [__meta_kubernetes_pod_container_name]
          target_label: container
        
        pipeline_stages:
        # Parse JSON logs
        - json:
            expressions:
              level: level
              component: component
              msg: msg
              corr: corr
              vod_id: vod_id
              channel: channel
              time: time
        
        # Use log timestamp if available
        - timestamp:
            source: time
            format: RFC3339Nano
        
        # Extract labels
        - labels:
            level:
            component:
            container:
```

Deploy Promtail as a DaemonSet:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: promtail
  namespace: vod-tender
spec:
  selector:
    matchLabels:
      app: promtail
  template:
    metadata:
      labels:
        app: promtail
    spec:
      serviceAccountName: promtail
      containers:
      - name: promtail
        image: grafana/promtail:2.9.3
        args:
          - -config.file=/etc/promtail/config.yaml
        volumeMounts:
        - name: config
          mountPath: /etc/promtail
        - name: varlog
          mountPath: /var/log
        - name: varlibdockercontainers
          mountPath: /var/lib/docker/containers
          readOnly: true
        - name: positions
          mountPath: /tmp
      volumes:
      - name: config
        configMap:
          name: promtail-config
      - name: varlog
        hostPath:
          path: /var/log
      - name: varlibdockercontainers
        hostPath:
          path: /var/lib/docker/containers
      - name: positions
        emptyDir: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promtail
  namespace: vod-tender
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: promtail
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/proxy
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: promtail
subjects:
- kind: ServiceAccount
  name: promtail
  namespace: vod-tender
roleRef:
  kind: ClusterRole
  name: promtail
  apiGroup: rbac.authorization.k8s.io
```

### Configuring Loki in Grafana

1. **Add Loki data source**:
   - Navigate to **Configuration → Data Sources**
   - Click **Add data source**
   - Select **Loki**
   - Set URL:
     - Docker Compose: `http://loki:3100`
     - Kubernetes: `http://loki.vod-tender.svc.cluster.local:3100`
   - Click **Save & Test**

2. **Verify log ingestion**:
   - Navigate to **Explore**
   - Select Loki data source
   - Run query: `{service="api"}`
   - You should see JSON-formatted logs

## ELK Stack Integration

The ELK stack (Elasticsearch, Logstash, Kibana) provides powerful log search, analysis, and visualization capabilities.

### Architecture

```
vod-tender → stdout (JSON) → Filebeat → Elasticsearch → Kibana (KQL queries)
```

### Filebeat Configuration

Filebeat is a lightweight shipper for forwarding logs to Elasticsearch.

#### Docker Compose Setup

Create `filebeat/filebeat.yml`:

```yaml
filebeat.inputs:
  # Collect Docker container logs
  - type: container
    paths:
      - '/var/lib/docker/containers/*/*.log'
    
    # Only process vod-tender containers
    processors:
      - decode_json_fields:
          fields: ["message"]
          target: ""
          overwrite_keys: true
      
      - drop_event:
          when:
            not:
              contains:
                container.name: "vod-"
      
      - add_docker_metadata:
          host: "unix:///var/run/docker.sock"
      
      # Add custom fields
      - add_fields:
          target: ''
          fields:
            service: vod-tender

# Parse JSON logs
processors:
  - decode_json_fields:
      fields: ["message"]
      target: ""
      overwrite_keys: true
      max_depth: 2
  
  # Rename fields for consistency
  - rename:
      fields:
        - from: "level"
          to: "log.level"
        - from: "msg"
          to: "message"
      ignore_missing: true

# Output to Elasticsearch
output.elasticsearch:
  hosts: ["elasticsearch:9200"]
  index: "vod-tender-%{+yyyy.MM.dd}"
  
  # Optional: authentication
  # username: "elastic"
  # password: "changeme"

# Index template settings
setup.template.name: "vod-tender"
setup.template.pattern: "vod-tender-*"
setup.template.settings:
  index.number_of_shards: 1
  index.number_of_replicas: 0

# Kibana configuration
setup.kibana:
  host: "kibana:5601"
```

Add Filebeat, Elasticsearch, and Kibana to `docker-compose.yml`:

```yaml
services:
  # ... existing services ...

  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.0
    container_name: vod-elasticsearch
    restart: unless-stopped
    environment:
      - discovery.type=single-node
      - xpack.security.enabled=false
      - "ES_JAVA_OPTS=-Xms512m -Xmx512m"
    ports:
      - "9200:9200"
    volumes:
      - elasticsearch_data:/usr/share/elasticsearch/data
    networks:
      - web

  kibana:
    image: docker.elastic.co/kibana/kibana:8.11.0
    container_name: vod-kibana
    restart: unless-stopped
    ports:
      - "5601:5601"
    environment:
      - ELASTICSEARCH_HOSTS=http://elasticsearch:9200
    depends_on:
      - elasticsearch
    networks:
      - web

  filebeat:
    image: docker.elastic.co/beats/filebeat:8.11.0
    container_name: vod-filebeat
    restart: unless-stopped
    user: root
    volumes:
      - ./filebeat/filebeat.yml:/usr/share/filebeat/filebeat.yml:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - filebeat_data:/usr/share/filebeat/data
    command: filebeat -e -strict.perms=false
    depends_on:
      - elasticsearch
    networks:
      - web

volumes:
  elasticsearch_data:
  filebeat_data:
```

#### Kubernetes Setup

Create `k8s/filebeat-configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: filebeat-config
  namespace: vod-tender
data:
  filebeat.yml: |
    filebeat.inputs:
    - type: container
      paths:
        - /var/log/containers/vod-api-*.log
        - /var/log/containers/vod-frontend-*.log
      
      processors:
      - decode_json_fields:
          fields: ["message"]
          target: ""
          overwrite_keys: true
          max_depth: 2
      
      - add_kubernetes_metadata:
          host: ${NODE_NAME}
          matchers:
          - logs_path:
              logs_path: "/var/log/containers/"

    processors:
    - rename:
        fields:
        - from: "level"
          to: "log.level"
        - from: "msg"
          to: "message"
        ignore_missing: true

    output.elasticsearch:
      hosts: ['${ELASTICSEARCH_HOST:elasticsearch}:${ELASTICSEARCH_PORT:9200}']
      index: "vod-tender-%{+yyyy.MM.dd}"

    setup.template.name: "vod-tender"
    setup.template.pattern: "vod-tender-*"
    
    setup.kibana:
      host: "${KIBANA_HOST:kibana}:${KIBANA_PORT:5601}"
```

Deploy Filebeat as a DaemonSet:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: filebeat
  namespace: vod-tender
spec:
  selector:
    matchLabels:
      app: filebeat
  template:
    metadata:
      labels:
        app: filebeat
    spec:
      serviceAccountName: filebeat
      terminationGracePeriodSeconds: 30
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
      - name: filebeat
        image: docker.elastic.co/beats/filebeat:8.11.0
        args: [
          "-c", "/etc/filebeat.yml",
          "-e",
        ]
        env:
        - name: ELASTICSEARCH_HOST
          value: elasticsearch
        - name: ELASTICSEARCH_PORT
          value: "9200"
        - name: KIBANA_HOST
          value: kibana
        - name: KIBANA_PORT
          value: "5601"
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        securityContext:
          runAsUser: 0
        resources:
          limits:
            memory: 200Mi
          requests:
            cpu: 100m
            memory: 100Mi
        volumeMounts:
        - name: config
          mountPath: /etc/filebeat.yml
          readOnly: true
          subPath: filebeat.yml
        - name: data
          mountPath: /usr/share/filebeat/data
        - name: varlibdockercontainers
          mountPath: /var/lib/docker/containers
          readOnly: true
        - name: varlog
          mountPath: /var/log
          readOnly: true
      volumes:
      - name: config
        configMap:
          defaultMode: 0640
          name: filebeat-config
      - name: varlibdockercontainers
        hostPath:
          path: /var/lib/docker/containers
      - name: varlog
        hostPath:
          path: /var/log
      - name: data
        emptyDir: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: filebeat
  namespace: vod-tender
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: filebeat
rules:
- apiGroups: [""]
  resources:
  - namespaces
  - pods
  - nodes
  verbs:
  - get
  - watch
  - list
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: filebeat
subjects:
- kind: ServiceAccount
  name: filebeat
  namespace: vod-tender
roleRef:
  kind: ClusterRole
  name: filebeat
  apiGroup: rbac.authorization.k8s.io
```

### Kibana Index Pattern Setup

1. **Access Kibana**: Navigate to `http://localhost:5601`

2. **Create Index Pattern**:
   - Go to **Stack Management → Index Patterns**
   - Click **Create index pattern**
   - Enter pattern: `vod-tender-*`
   - Select time field: `@timestamp` or `time`
   - Click **Create index pattern**

3. **Verify log ingestion**:
   - Navigate to **Discover**
   - Select `vod-tender-*` index pattern
   - You should see logs with parsed JSON fields

## Useful Queries

### Loki (LogQL) Queries

LogQL is Loki's query language. It uses label matchers and log stream selectors.

#### Basic Filtering

```logql
# All logs from API service
{service="api"}

# Logs with ERROR level
{service="api"} |= "ERROR"

# Logs from VOD processor component
{service="api", component="vod_processor"}

# Logs for specific VOD ID
{service="api"} | json | vod_id="1234567890"
```

#### Advanced Filtering

```logql
# All download errors
{service="api", component="vod_download", level="ERROR"}

# Circuit breaker events
{service="api"} | json | msg =~ "circuit.*"

# Logs with correlation ID (for request tracing)
{service="api"} | json | corr="abc-123-def"

# Upload completions with duration
{service="api", component="vod_upload"} | json | msg="upload completed" | duration_ms > 60000
```

#### Aggregations and Metrics

```logql
# Count errors per minute
sum(count_over_time({service="api", level="ERROR"}[1m]))

# Rate of download failures
rate({service="api", component="vod_download", level="ERROR"}[5m])

# 95th percentile download duration
quantile_over_time(0.95, {service="api", component="vod_download"} | json | unwrap duration_ms [5m])

# Top 10 most common error messages
topk(10, sum by (msg) (count_over_time({service="api", level="ERROR"}[1h])))
```

#### Correlation Tracking

```logql
# Find all logs for a specific HTTP request
{service="api"} | json | corr="abc-123-def-456"

# Trace processing pipeline for a VOD
{service="api"} | json | vod_id="1234567890" | line_format "{{.time}} [{{.level}}] {{.component}}: {{.msg}}"
```

### Kibana (KQL) Queries

KQL (Kibana Query Language) provides powerful search capabilities in Kibana.

#### Basic Filtering

```kql
# All logs with ERROR level
log.level: "ERROR"

# Logs from VOD download component
component: "vod_download"

# Logs for specific VOD ID
vod_id: "1234567890"

# Logs with specific message
message: "download completed"
```

#### Advanced Filtering

```kql
# Download errors with duration over 5 minutes
component: "vod_download" AND log.level: "ERROR" AND duration_ms > 300000

# Circuit breaker state changes
component: "vod_processor" AND message: (*circuit*)

# Failed uploads with error details
component: "vod_upload" AND log.level: "ERROR" AND err: *

# All logs for a specific channel
channel: "examplechannel"
```

#### Time-Based Queries

```kql
# Errors in last hour
log.level: "ERROR" AND @timestamp >= now-1h

# Downloads completed today
component: "vod_download" AND message: "download completed" AND @timestamp >= now/d

# OAuth refresh failures in last 24 hours
component: "oauth_refresh" AND log.level: "ERROR" AND @timestamp >= now-24h
```

#### Correlation Tracking

```kql
# All logs for specific HTTP request
corr: "abc-123-def-456"

# Trace complete VOD processing pipeline
vod_id: "1234567890" AND (component: "vod_processor" OR component: "vod_download" OR component: "vod_upload")
```

### Common Debugging Scenarios

#### Scenario 1: Why did VOD download fail?

**Loki**:
```logql
{service="api"} | json | vod_id="1234567890" | component="vod_download"
```

**Kibana**:
```kql
vod_id: "1234567890" AND component: "vod_download"
```

#### Scenario 2: Circuit breaker history

**Loki**:
```logql
{service="api", component="vod_processor"} | json | msg =~ "circuit.*" | line_format "{{.time}} {{.msg}} state={{.circuit_state}}"
```

**Kibana**:
```kql
component: "vod_processor" AND message: (*circuit*)
```

#### Scenario 3: Slow request investigation

**Loki**:
```logql
{service="api"} | json | corr="abc-123-def" | line_format "{{.time}} [{{.component}}] {{.msg}} {{.duration_ms}}ms"
```

**Kibana**:
```kql
corr: "abc-123-def"
```

#### Scenario 4: Error rate spike analysis

**Loki**:
```logql
sum by (component) (count_over_time({service="api", level="ERROR"}[5m]))
```

**Kibana**:
- Use **Lens** visualization
- Metric: Count
- Filter: `log.level: "ERROR"`
- Break down by: `component.keyword`
- Time range: Last 24 hours

## Docker Compose Examples

### Complete Loki Stack

Create `docker-compose.loki.yml`:

```yaml
version: '3.8'

services:
  # vod-tender API with JSON logging
  api:
    image: vod-tender:latest
    environment:
      - LOG_FORMAT=json
      - LOG_LEVEL=info
    # ... other config ...

  # Loki for log storage
  loki:
    image: grafana/loki:2.9.3
    ports:
      - "3100:3100"
    volumes:
      - ./loki/loki.yml:/etc/loki/local-config.yaml:ro
      - loki_data:/loki
    command: -config.file=/etc/loki/local-config.yaml

  # Promtail for log collection
  promtail:
    image: grafana/promtail:2.9.3
    volumes:
      - ./promtail/promtail.yml:/etc/promtail/config.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
    command: -config.file=/etc/promtail/config.yml
    depends_on:
      - loki

  # Grafana for visualization
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
    volumes:
      - grafana_data:/var/lib/grafana
    depends_on:
      - loki

volumes:
  loki_data:
  grafana_data:
```

### Complete ELK Stack

Create `docker-compose.elk.yml`:

```yaml
version: '3.8'

services:
  # vod-tender API with JSON logging
  api:
    image: vod-tender:latest
    environment:
      - LOG_FORMAT=json
      - LOG_LEVEL=info
    # ... other config ...

  # Elasticsearch for log storage
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.11.0
    environment:
      - discovery.type=single-node
      - xpack.security.enabled=false
      - "ES_JAVA_OPTS=-Xms1g -Xmx1g"
    ports:
      - "9200:9200"
    volumes:
      - elasticsearch_data:/usr/share/elasticsearch/data

  # Kibana for visualization
  kibana:
    image: docker.elastic.co/kibana/kibana:8.11.0
    ports:
      - "5601:5601"
    environment:
      - ELASTICSEARCH_HOSTS=http://elasticsearch:9200
    depends_on:
      - elasticsearch

  # Filebeat for log collection
  filebeat:
    image: docker.elastic.co/beats/filebeat:8.11.0
    user: root
    volumes:
      - ./filebeat/filebeat.yml:/usr/share/filebeat/filebeat.yml:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    command: filebeat -e -strict.perms=false
    depends_on:
      - elasticsearch

volumes:
  elasticsearch_data:
```

### Usage

```bash
# Start Loki stack
docker compose -f docker-compose.yml -f docker-compose.loki.yml up -d

# Or start ELK stack
docker compose -f docker-compose.yml -f docker-compose.elk.yml up -d

# View logs
docker compose logs -f api

# Access dashboards
# Grafana: http://localhost:3000 (with Loki)
# Kibana: http://localhost:5601 (with ELK)
```

## Kubernetes Deployment

### Loki Stack with Helm

```bash
# Add Grafana Helm repo
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Install Loki
helm install loki grafana/loki-stack \
  --namespace vod-tender \
  --set loki.enabled=true \
  --set promtail.enabled=true \
  --set grafana.enabled=true \
  --set grafana.sidecar.datasources.enabled=true

# Get Grafana admin password
kubectl get secret --namespace vod-tender loki-grafana \
  -o jsonpath="{.data.admin-password}" | base64 --decode

# Port forward to access Grafana
kubectl port-forward --namespace vod-tender service/loki-grafana 3000:80
```

### ELK Stack with Helm

```bash
# Add Elastic Helm repo
helm repo add elastic https://helm.elastic.co
helm repo update

# Install Elasticsearch
helm install elasticsearch elastic/elasticsearch \
  --namespace vod-tender \
  --set replicas=1 \
  --set minimumMasterNodes=1

# Install Kibana
helm install kibana elastic/kibana \
  --namespace vod-tender \
  --set elasticsearchHosts=http://elasticsearch-master:9200

# Install Filebeat
helm install filebeat elastic/filebeat \
  --namespace vod-tender \
  --set daemonset.enabled=true

# Port forward to access Kibana
kubectl port-forward --namespace vod-tender service/kibana-kibana 5601:5601
```

## Troubleshooting

### No Logs Appearing in Loki

**Symptoms**: Grafana shows no logs from vod-tender

**Diagnosis**:

```bash
# Check Promtail is running
docker ps | grep promtail

# Check Promtail logs
docker logs vod-promtail

# Verify Promtail can reach Loki
docker exec vod-promtail wget -O- http://loki:3100/ready

# Check if logs are being scraped
docker exec vod-promtail cat /tmp/positions.yaml
```

**Solutions**:

1. **Verify JSON format**: Ensure `LOG_FORMAT=json` is set in vod-tender
2. **Check container labels**: Promtail filters by container labels; verify your `promtail.yml` matches your container names
3. **Check Loki connectivity**: Ensure Promtail can reach Loki URL
4. **Review Promtail config**: Verify `pipeline_stages` correctly parse JSON

### Kibana Not Showing Logs

**Symptoms**: No documents in Kibana index pattern

**Diagnosis**:

```bash
# Check Filebeat is running
docker ps | grep filebeat

# Check Filebeat logs
docker logs vod-filebeat

# Verify Elasticsearch connectivity
docker exec vod-filebeat curl http://elasticsearch:9200

# Check if index exists
curl http://localhost:9200/_cat/indices?v | grep vod-tender
```

**Solutions**:

1. **Check Elasticsearch index**: Run `curl http://localhost:9200/vod-tender-*/_search?size=1`
2. **Verify JSON parsing**: Check Filebeat `decode_json_fields` processor
3. **Review Filebeat config**: Ensure container paths are correct
4. **Recreate index pattern**: Delete and recreate in Kibana if field mappings are wrong

### High Memory Usage (Elasticsearch)

**Symptoms**: Elasticsearch consuming excessive RAM

**Solutions**:

1. **Set JVM heap size**:
   ```yaml
   environment:
     - "ES_JAVA_OPTS=-Xms512m -Xmx512m"  # Adjust based on available memory
   ```

2. **Enable index lifecycle management**:
   ```bash
   # Delete old indices automatically
   curl -X PUT "localhost:9200/_ilm/policy/vod-tender-policy" -H 'Content-Type: application/json' -d'
   {
     "policy": {
       "phases": {
         "delete": {
           "min_age": "30d",
           "actions": {
             "delete": {}
           }
         }
       }
     }
   }'
   ```

3. **Reduce replica count**: Set `index.number_of_replicas: 0` for single-node setups

### Logs Missing Fields After Ingestion

**Symptoms**: JSON fields not appearing as searchable fields

**Loki Solution**: Verify `pipeline_stages` includes all desired fields:

```yaml
pipeline_stages:
  - json:
      expressions:
        level: level
        component: component
        msg: msg
        vod_id: vod_id  # Add missing fields here
```

**Kibana Solution**: Refresh field list in index pattern or recreate index template:

```bash
# Refresh Kibana index pattern
curl -X POST "localhost:5601/api/index_patterns/index_pattern/<pattern-id>/refresh_fields"

# Or recreate index template
curl -X DELETE "localhost:9200/vod-tender-*"
# Restart Filebeat to recreate with new template
```

### Log Retention Issues

**Loki**: Configure retention in `loki.yml`:

```yaml
limits_config:
  retention_period: 744h  # 31 days
```

**Elasticsearch**: Use Index Lifecycle Management (ILM) or curator to delete old indices:

```bash
# Manual cleanup
curl -X DELETE "localhost:9200/vod-tender-2024.10.*"
```

## Best Practices

### Development

1. **Use text format locally**: `LOG_FORMAT=text` for easier debugging
2. **Enable debug level**: `LOG_LEVEL=debug` when troubleshooting specific issues
3. **Use correlation IDs**: Include `X-Correlation-ID` header in requests to trace them through logs

### Production

1. **Always use JSON format**: `LOG_FORMAT=json` for structured logging
2. **Set appropriate log level**: `LOG_LEVEL=info` balances verbosity and debuggability
3. **Implement log retention**: Configure Loki/Elasticsearch retention policies to manage disk usage
4. **Monitor log volume**: Alert on sudden spikes in log volume (may indicate errors or attacks)
5. **Index critical fields**: Make frequently-queried fields (component, level, vod_id) indexed labels/fields
6. **Avoid high-cardinality labels**: Don't index fields with many unique values (e.g., timestamps, full error messages)
7. **Regular cleanup**: Archive or delete old logs based on compliance requirements

### Security

1. **Sanitize sensitive data**: vod-tender already masks OAuth tokens in logs; verify no secrets leak
2. **Restrict access**: Use authentication for Grafana/Kibana in production
3. **Audit log access**: Track who queries logs for sensitive operations
4. **Encrypt in transit**: Use TLS for log shipper → aggregator connections in production

## See Also

- [OBSERVABILITY.md](OBSERVABILITY.md) - Metrics, tracing, and alerting
- [RUNBOOKS.md](RUNBOOKS.md) - Operational procedures and troubleshooting
- [CONFIG.md](CONFIG.md) - Environment variable reference including LOG_FORMAT and LOG_LEVEL
- [OPERATIONS.md](OPERATIONS.md) - Deployment and maintenance procedures
