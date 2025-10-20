# Production Deployment Guide

This guide covers deploying vod-tender in production environments using Kubernetes, Docker Swarm, bare metal, and cloud platforms.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Docker Swarm Deployment](#docker-swarm-deployment)
- [Bare Metal / VM Deployment](#bare-metal--vm-deployment)
- [Cloud Platform Guides](#cloud-platform-guides)
- [Zero-Downtime Upgrades](#zero-downtime-upgrades)
- [Post-Deployment Checklist](#post-deployment-checklist)

## Prerequisites

### Required Services

- **PostgreSQL 14+** - Primary data store
- **Persistent Storage** - For downloaded VOD files (100GB+ recommended per channel)
- **External Dependencies** - yt-dlp and ffmpeg (included in Docker images)

### Required Credentials

- **Twitch OAuth** - Bot account credentials for chat recording
- **Twitch API** - Client ID/Secret for Helix API access
- **YouTube OAuth** (optional) - For automatic upload to YouTube
- **Twitch Cookies** (optional) - For subscriber-only VODs

### Resource Requirements

Minimum per channel:

- **CPU**: 2 cores (4+ recommended for encoding)
- **Memory**: 2GB RAM (4GB+ for concurrent processing)
- **Storage**: 100GB+ for VOD files
- **Database**: 10GB+ for chat history and metadata

## Kubernetes Deployment

### Namespace Setup

```bash
kubectl create namespace vod-tender
kubectl label namespace vod-tender name=vod-tender
```

### Secret Management

Using **Sealed Secrets** (recommended):

```bash
# Install sealed-secrets controller
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/controller.yaml

# Create sealed secret for Twitch credentials
kubectl create secret generic vod-twitch-creds \
  --from-literal=TWITCH_CLIENT_ID=your_client_id \
  --from-literal=TWITCH_CLIENT_SECRET=your_client_secret \
  --from-literal=TWITCH_OAUTH_TOKEN=oauth:your_token \
  --dry-run=client -o yaml | \
  kubeseal -o yaml > twitch-creds-sealed.yaml

kubectl apply -f twitch-creds-sealed.yaml -n vod-tender
```

Using **External Secrets Operator**:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-secrets-manager
  namespace: vod-tender
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        jwt:
          serviceAccountRef:
            name: vod-tender
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: vod-twitch-creds
  namespace: vod-tender
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: vod-twitch-creds
  data:
    - secretKey: TWITCH_CLIENT_ID
      remoteRef:
        key: vod-tender/twitch
        property: client_id
    - secretKey: TWITCH_CLIENT_SECRET
      remoteRef:
        key: vod-tender/twitch
        property: client_secret
```

### PostgreSQL Setup

Using a managed service (recommended):

```yaml
# postgres-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vod-postgres-config
  namespace: vod-tender
data:
  DB_HOST: "your-rds-endpoint.amazonaws.com"
  DB_PORT: "5432"
  DB_NAME: "vod"
  DB_SSLMODE: "require"
---
apiVersion: v1
kind: Secret
metadata:
  name: vod-postgres-creds
  namespace: vod-tender
type: Opaque
stringData:
  DB_USER: "vod_admin"
  DB_PASSWORD: "secure-random-password"
```

Or deploy PostgreSQL in-cluster:

```yaml
# postgres-statefulset.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data
  namespace: vod-tender
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi
  storageClassName: fast-ssd
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: vod-tender
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:16-alpine
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_DB
          value: vod
        - name: POSTGRES_USER
          value: vod
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vod-postgres-creds
              key: DB_PASSWORD
        volumeMounts:
        - name: postgres-data
          mountPath: /var/lib/postgresql/data
        resources:
          requests:
            cpu: 500m
            memory: 1Gi
          limits:
            cpu: 2000m
            memory: 4Gi
  volumeClaimTemplates:
  - metadata:
      name: postgres-data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      storageClassName: fast-ssd
      resources:
        requests:
          storage: 50Gi
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: vod-tender
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
    targetPort: 5432
  clusterIP: None
```

### Application Deployment

```yaml
# vod-tender-deployment.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vod-data
  namespace: vod-tender
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 200Gi
  storageClassName: standard
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vod-api
  namespace: vod-tender
  labels:
    app: vod-api
spec:
  replicas: 1  # Single replica due to download state management
  strategy:
    type: Recreate  # Avoid concurrent processing
  selector:
    matchLabels:
      app: vod-api
  template:
    metadata:
      labels:
        app: vod-api
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: api
        image: ghcr.io/subculture-collective/vod-tender:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: DB_DSN
          value: "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)"
        - name: DB_USER
          valueFrom:
            secretKeyRef:
              name: vod-postgres-creds
              key: DB_USER
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vod-postgres-creds
              key: DB_PASSWORD
        - name: DB_HOST
          valueFrom:
            configMapKeyRef:
              name: vod-postgres-config
              key: DB_HOST
        - name: DB_PORT
          valueFrom:
            configMapKeyRef:
              name: vod-postgres-config
              key: DB_PORT
        - name: DB_NAME
          valueFrom:
            configMapKeyRef:
              name: vod-postgres-config
              key: DB_NAME
        - name: DB_SSLMODE
          valueFrom:
            configMapKeyRef:
              name: vod-postgres-config
              key: DB_SSLMODE
        - name: DATA_DIR
          value: "/data"
        - name: LOG_FORMAT
          value: "json"
        - name: LOG_LEVEL
          value: "info"
        envFrom:
        - secretRef:
            name: vod-twitch-creds
        - configMapRef:
            name: vod-config
        volumeMounts:
        - name: vod-data
          mountPath: /data
        - name: cookies
          mountPath: /run/cookies
          readOnly: true
        resources:
          requests:
            cpu: 1000m
            memory: 2Gi
          limits:
            cpu: 4000m
            memory: 8Gi
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
      volumes:
      - name: vod-data
        persistentVolumeClaim:
          claimName: vod-data
      - name: cookies
        secret:
          secretName: vod-cookies
          optional: true
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: vod-config
  namespace: vod-tender
data:
  TWITCH_CHANNEL: "your_channel"
  TWITCH_BOT_USERNAME: "your_bot"
  CHAT_AUTO_START: "1"
  YTDLP_COOKIES_PATH: "/run/cookies/twitch-cookies.txt"
  CIRCUIT_FAILURE_THRESHOLD: "5"
  CIRCUIT_OPEN_COOLDOWN: "10m"
  VOD_CATALOG_BACKFILL_INTERVAL: "6h"
---
apiVersion: v1
kind: Service
metadata:
  name: vod-api
  namespace: vod-tender
spec:
  selector:
    app: vod-api
  ports:
  - port: 8080
    targetPort: 8080
    name: http
  type: ClusterIP
```

### Frontend Deployment

```yaml
# frontend-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vod-frontend
  namespace: vod-tender
spec:
  replicas: 2
  selector:
    matchLabels:
      app: vod-frontend
  template:
    metadata:
      labels:
        app: vod-frontend
    spec:
      containers:
      - name: frontend
        image: ghcr.io/subculture-collective/vod-tender-frontend:latest
        ports:
        - containerPort: 80
        env:
        - name: VITE_API_BASE_URL
          value: "https://vod-api.example.com"
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: vod-frontend
  namespace: vod-tender
spec:
  selector:
    app: vod-frontend
  ports:
  - port: 80
    targetPort: 80
  type: ClusterIP
```

### Ingress Configuration

Using **nginx-ingress**:

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: vod-tender
  namespace: vod-tender
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/proxy-body-size: "0"  # No upload size limit
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"  # Long-running downloads
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - vod-tender.example.com
    - vod-api.example.com
    secretName: vod-tender-tls
  rules:
  - host: vod-tender.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: vod-frontend
            port:
              number: 80
  - host: vod-api.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: vod-api
            port:
              number: 8080
```

### TLS Certificate Management

Using **cert-manager**:

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Create ClusterIssuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
EOF
```

### Network Policies

```yaml
# network-policy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: vod-api-policy
  namespace: vod-tender
spec:
  podSelector:
    matchLabels:
      app: vod-api
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: vod-frontend
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8080
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  - to:  # Allow external API calls (Twitch, YouTube)
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443
    - protocol: TCP
      port: 80
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: postgres-policy
  namespace: vod-tender
spec:
  podSelector:
    matchLabels:
      app: postgres
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: vod-api
    ports:
    - protocol: TCP
      port: 5432
```

### Deployment Commands

```bash
# Apply all configurations
kubectl apply -f postgres-config.yaml
kubectl apply -f postgres-statefulset.yaml
kubectl apply -f vod-tender-deployment.yaml
kubectl apply -f frontend-deployment.yaml
kubectl apply -f ingress.yaml
kubectl apply -f network-policy.yaml

# Verify deployment
kubectl get pods -n vod-tender
kubectl get svc -n vod-tender
kubectl get ingress -n vod-tender

# Check logs
kubectl logs -f deployment/vod-api -n vod-tender
kubectl logs -f deployment/vod-frontend -n vod-tender

# Port forward for local testing
kubectl port-forward svc/vod-api 8080:8080 -n vod-tender
```

## Docker Swarm Deployment

### Stack File

```yaml
# vod-swarm-stack.yml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: vod
      POSTGRES_USER: vod
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
    secrets:
      - db_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - internal
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.postgres == true
      resources:
        limits:
          cpus: '2'
          memory: 4G
        reservations:
          cpus: '0.5'
          memory: 1G

  api:
    image: ghcr.io/subculture-collective/vod-tender:latest
    environment:
      DB_DSN: postgres://vod:${DB_PASSWORD}@postgres:5432/vod?sslmode=disable
      DATA_DIR: /data
      LOG_FORMAT: json
      LOG_LEVEL: info
      TWITCH_CHANNEL: ${TWITCH_CHANNEL}
      TWITCH_BOT_USERNAME: ${TWITCH_BOT_USERNAME}
      TWITCH_OAUTH_TOKEN_FILE: /run/secrets/twitch_oauth
      TWITCH_CLIENT_ID_FILE: /run/secrets/twitch_client_id
      TWITCH_CLIENT_SECRET_FILE: /run/secrets/twitch_client_secret
      CHAT_AUTO_START: "1"
      YTDLP_COOKIES_PATH: /run/secrets/twitch_cookies
    secrets:
      - twitch_oauth
      - twitch_client_id
      - twitch_client_secret
      - twitch_cookies
    configs:
      - source: vod_config
        target: /etc/vod-tender/config
    volumes:
      - vod_data:/data
    networks:
      - internal
      - web
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.vod-storage == true
      update_config:
        parallelism: 1
        delay: 10s
        order: stop-first
      resources:
        limits:
          cpus: '4'
          memory: 8G
        reservations:
          cpus: '1'
          memory: 2G
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.vod-api.rule=Host(`vod-api.example.com`)"
        - "traefik.http.routers.vod-api.entrypoints=websecure"
        - "traefik.http.routers.vod-api.tls.certresolver=letsencrypt"
        - "traefik.http.services.vod-api.loadbalancer.server.port=8080"

  frontend:
    image: ghcr.io/subculture-collective/vod-tender-frontend:latest
    environment:
      VITE_API_BASE_URL: https://vod-api.example.com
    networks:
      - web
    deploy:
      replicas: 2
      update_config:
        parallelism: 1
        delay: 10s
      resources:
        limits:
          cpus: '0.5'
          memory: 512M
        reservations:
          cpus: '0.1'
          memory: 128M
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.vod-frontend.rule=Host(`vod-tender.example.com`)"
        - "traefik.http.routers.vod-frontend.entrypoints=websecure"
        - "traefik.http.routers.vod-frontend.tls.certresolver=letsencrypt"
        - "traefik.http.services.vod-frontend.loadbalancer.server.port=80"

  backup:
    image: postgres:16-alpine
    environment:
      PGHOST: postgres
      PGUSER: vod
      PGPASSWORD_FILE: /run/secrets/db_password
      PGDATABASE: vod
    secrets:
      - db_password
    volumes:
      - backup_data:/backups
    networks:
      - internal
    entrypoint: /bin/sh
    command: >
      -c "while true; do
        pg_dump -Fc > /backups/vod_$$(date +%Y%m%d_%H%M%S).dump;
        find /backups -name '*.dump' -mtime +7 -delete;
        sleep 86400;
      done"
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.backup == true

networks:
  internal:
    driver: overlay
  web:
    external: true

volumes:
  postgres_data:
    driver: local
  vod_data:
    driver: local
  backup_data:
    driver: local

secrets:
  db_password:
    external: true
  twitch_oauth:
    external: true
  twitch_client_id:
    external: true
  twitch_client_secret:
    external: true
  twitch_cookies:
    external: true

configs:
  vod_config:
    external: true
```

### Swarm Setup Commands

```bash
# Initialize swarm
docker swarm init

# Label nodes for placement
docker node update --label-add postgres=true node1
docker node update --label-add vod-storage=true node1
docker node update --label-add backup=true node2

# Create external network
docker network create -d overlay web

# Create secrets
echo "secure_db_password" | docker secret create db_password -
echo "oauth:your_token" | docker secret create twitch_oauth -
echo "your_client_id" | docker secret create twitch_client_id -
echo "your_client_secret" | docker secret create twitch_client_secret -
docker secret create twitch_cookies ./secrets/twitch-cookies.txt

# Deploy stack
docker stack deploy -c vod-swarm-stack.yml vod

# Monitor deployment
docker stack ps vod
docker service logs -f vod_api

# Update service (rolling)
docker service update --image ghcr.io/subculture-collective/vod-tender:v2 vod_api

# Scale frontend
docker service scale vod_frontend=3
```

### Constraints and Placement

Use node labels to control where services run:

```bash
# Storage node (large disk)
docker node update --label-add vod-storage=true node1

# Database node (fast I/O)
docker node update --label-add postgres=true node2

# Backup node (separate failure domain)
docker node update --label-add backup=true node3
```

## Bare Metal / VM Deployment

### System Requirements

- **OS**: Ubuntu 22.04 LTS or Debian 12 (recommended)
- **Architecture**: x86_64 or ARM64
- **User**: Dedicated `vod` user (non-root)

### Installation Steps

```bash
# 1. Install dependencies
sudo apt update
sudo apt install -y postgresql-16 python3-pip ffmpeg aria2 nginx certbot

# Install yt-dlp
sudo pip3 install yt-dlp

# 2. Create dedicated user
sudo useradd -r -s /bin/bash -d /opt/vod-tender vod
sudo mkdir -p /opt/vod-tender/{bin,data,logs,backups}
sudo chown -R vod:vod /opt/vod-tender

# 3. Configure PostgreSQL
sudo -u postgres psql <<EOF
CREATE USER vod WITH PASSWORD 'secure_password';
CREATE DATABASE vod OWNER vod;
GRANT ALL PRIVILEGES ON DATABASE vod TO vod;
\c vod
ALTER SCHEMA public OWNER TO vod;
EOF

# 4. Download and install binary
VERSION=v1.0.0
wget https://github.com/subculture-collective/vod-tender/releases/download/${VERSION}/vod-tender-linux-amd64 -O /tmp/vod-tender
sudo mv /tmp/vod-tender /opt/vod-tender/bin/
sudo chmod +x /opt/vod-tender/bin/vod-tender
sudo chown vod:vod /opt/vod-tender/bin/vod-tender

# 5. Create configuration
sudo tee /opt/vod-tender/.env > /dev/null <<EOF
# Database
DB_DSN=postgres://vod:secure_password@localhost:5432/vod?sslmode=disable

# Storage
DATA_DIR=/opt/vod-tender/data

# Twitch
TWITCH_CHANNEL=your_channel
TWITCH_BOT_USERNAME=your_bot
TWITCH_OAUTH_TOKEN=oauth:your_token
TWITCH_CLIENT_ID=your_client_id
TWITCH_CLIENT_SECRET=your_client_secret

# Chat
CHAT_AUTO_START=1

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Processing
CIRCUIT_FAILURE_THRESHOLD=5
CIRCUIT_OPEN_COOLDOWN=10m
EOF

sudo chown vod:vod /opt/vod-tender/.env
sudo chmod 600 /opt/vod-tender/.env
```

### Systemd Service

```ini
# /etc/systemd/system/vod-tender.service
[Unit]
Description=VOD Tender - Twitch VOD Archival Service
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=vod
Group=vod
WorkingDirectory=/opt/vod-tender
EnvironmentFile=/opt/vod-tender/.env
ExecStart=/opt/vod-tender/bin/vod-tender
Restart=on-failure
RestartSec=10s
TimeoutStopSec=30s

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/vod-tender/data /opt/vod-tender/logs
CapabilityBoundingSet=

# Resource limits
LimitNOFILE=65536
CPUQuota=400%
MemoryMax=8G

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=vod-tender

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable vod-tender
sudo systemctl start vod-tender
sudo systemctl status vod-tender

# View logs
sudo journalctl -u vod-tender -f
```

### Nginx Reverse Proxy

```nginx
# /etc/nginx/sites-available/vod-tender
upstream vod_api {
    server 127.0.0.1:8080;
    keepalive 32;
}

server {
    listen 80;
    server_name vod-api.example.com;
    
    # Redirect to HTTPS
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name vod-api.example.com;
    
    # SSL certificates (managed by certbot)
    ssl_certificate /etc/letsencrypt/live/vod-api.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/vod-api.example.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
    
    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    
    # Logging
    access_log /var/log/nginx/vod-api-access.log;
    error_log /var/log/nginx/vod-api-error.log;
    
    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;
    limit_req zone=api_limit burst=20 nodelay;
    
    location / {
        proxy_pass http://vod_api;
        proxy_http_version 1.1;
        
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";
        
        # Long-running requests (downloads)
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        
        # Buffer settings
        proxy_buffering off;
        proxy_request_buffering off;
    }
    
    # Metrics endpoint (restrict access)
    location /metrics {
        proxy_pass http://vod_api;
        
        # Allow only from monitoring servers
        allow 10.0.0.0/8;
        deny all;
    }
}
```

Enable and test:

```bash
sudo ln -s /etc/nginx/sites-available/vod-tender /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx

# Obtain SSL certificate
sudo certbot --nginx -d vod-api.example.com -d vod-tender.example.com
```

### Log Rotation

```bash
# /etc/logrotate.d/vod-tender
/opt/vod-tender/logs/*.log {
    daily
    rotate 14
    compress
    delaycompress
    notifempty
    missingok
    sharedscripts
    postrotate
        systemctl reload vod-tender > /dev/null 2>&1 || true
    endscript
}
```

### Backup Automation

```bash
# /opt/vod-tender/bin/backup.sh
#!/bin/bash
set -euo pipefail

BACKUP_DIR=/opt/vod-tender/backups
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="${BACKUP_DIR}/vod_${TIMESTAMP}.sql.gz"

# Create backup
pg_dump -U vod -h localhost vod | gzip > "${BACKUP_FILE}"

# Remove backups older than 30 days
find "${BACKUP_DIR}" -name "*.sql.gz" -mtime +30 -delete

echo "Backup completed: ${BACKUP_FILE}"
```

Add to crontab:

```bash
sudo -u vod crontab -e
# Add:
0 2 * * * /opt/vod-tender/bin/backup.sh >> /opt/vod-tender/logs/backup.log 2>&1
```

## Cloud Platform Guides

### AWS Deployment

#### ECS (Elastic Container Service)

**Task Definition**:

```json
{
  "family": "vod-tender",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "2048",
  "memory": "4096",
  "containerDefinitions": [
    {
      "name": "vod-api",
      "image": "ghcr.io/subculture-collective/vod-tender:latest",
      "portMappings": [
        {
          "containerPort": 8080,
          "protocol": "tcp"
        }
      ],
      "environment": [
        {
          "name": "DATA_DIR",
          "value": "/data"
        },
        {
          "name": "LOG_FORMAT",
          "value": "json"
        }
      ],
      "secrets": [
        {
          "name": "DB_DSN",
          "valueFrom": "arn:aws:secretsmanager:us-east-1:123456789:secret:vod-tender/db-dsn"
        },
        {
          "name": "TWITCH_CLIENT_ID",
          "valueFrom": "arn:aws:secretsmanager:us-east-1:123456789:secret:vod-tender/twitch:ClientId::"
        }
      ],
      "mountPoints": [
        {
          "sourceVolume": "vod-data",
          "containerPath": "/data",
          "readOnly": false
        }
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/vod-tender",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "api"
        }
      }
    }
  ],
  "volumes": [
    {
      "name": "vod-data",
      "efsVolumeConfiguration": {
        "fileSystemId": "fs-1234567",
        "transitEncryption": "ENABLED",
        "authorizationConfig": {
          "iam": "ENABLED"
        }
      }
    }
  ]
}
```

**Terraform Configuration**:

```hcl
# main.tf
provider "aws" {
  region = "us-east-1"
}

# RDS PostgreSQL
resource "aws_db_instance" "vod_postgres" {
  identifier           = "vod-tender-db"
  engine               = "postgres"
  engine_version       = "16.1"
  instance_class       = "db.t3.small"
  allocated_storage    = 50
  storage_encrypted    = true
  
  db_name  = "vod"
  username = "vod_admin"
  password = random_password.db_password.result
  
  vpc_security_group_ids = [aws_security_group.db.id]
  db_subnet_group_name   = aws_db_subnet_group.main.name
  
  backup_retention_period = 7
  skip_final_snapshot     = false
  final_snapshot_identifier = "vod-tender-final-snapshot"
  
  tags = {
    Name = "vod-tender-db"
  }
}

# EFS for VOD storage
resource "aws_efs_file_system" "vod_data" {
  creation_token = "vod-tender-data"
  encrypted      = true
  
  lifecycle_policy {
    transition_to_ia = "AFTER_30_DAYS"
  }
  
  tags = {
    Name = "vod-tender-data"
  }
}

# ECS Cluster
resource "aws_ecs_cluster" "main" {
  name = "vod-tender"
  
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

# ECS Service
resource "aws_ecs_service" "vod_api" {
  name            = "vod-api"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.vod_api.arn
  desired_count   = 1
  launch_type     = "FARGATE"
  
  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.ecs_tasks.id]
    assign_public_ip = false
  }
  
  load_balancer {
    target_group_arn = aws_lb_target_group.vod_api.arn
    container_name   = "vod-api"
    container_port   = 8080
  }
}
```

#### EKS (Elastic Kubernetes Service)

Use the Kubernetes deployment manifests from the [Kubernetes section](#kubernetes-deployment) with these AWS-specific additions:

```bash
# Create EKS cluster
eksctl create cluster \
  --name vod-tender \
  --region us-east-1 \
  --nodegroup-name standard-workers \
  --node-type t3.xlarge \
  --nodes 2 \
  --nodes-min 1 \
  --nodes-max 4 \
  --managed

# Install AWS EBS CSI driver for persistent volumes
kubectl apply -k "github.com/kubernetes-sigs/aws-ebs-csi-driver/deploy/kubernetes/overlays/stable/?ref=release-1.25"

# Use RDS for PostgreSQL
# Update DB_DSN to point to RDS endpoint
```

### GCP Deployment

#### Cloud Run

```bash
# Build and push image
gcloud builds submit --tag gcr.io/PROJECT_ID/vod-tender

# Deploy to Cloud Run
gcloud run deploy vod-tender \
  --image gcr.io/PROJECT_ID/vod-tender \
  --platform managed \
  --region us-central1 \
  --memory 4Gi \
  --cpu 2 \
  --timeout 3600 \
  --concurrency 1 \
  --set-env-vars LOG_FORMAT=json \
  --set-secrets DB_DSN=vod-db-dsn:latest,TWITCH_CLIENT_ID=twitch-client-id:latest \
  --vpc-connector vod-vpc-connector \
  --allow-unauthenticated
```

**Limitations**: Cloud Run is stateless; persistent storage requires Cloud Storage FUSE or Filestore.

#### GKE (Google Kubernetes Engine)

```bash
# Create GKE cluster
gcloud container clusters create vod-tender \
  --region us-central1 \
  --num-nodes 1 \
  --machine-type n1-standard-4 \
  --disk-size 100 \
  --enable-autoscaling \
  --min-nodes 1 \
  --max-nodes 3 \
  --enable-stackdriver-kubernetes

# Get credentials
gcloud container clusters get-credentials vod-tender --region us-central1

# Deploy using Kubernetes manifests
kubectl apply -f k8s/
```

### Azure Deployment

#### ACI (Azure Container Instances)

```bash
# Create resource group
az group create --name vod-tender --location eastus

# Create container group
az container create \
  --resource-group vod-tender \
  --name vod-tender-api \
  --image ghcr.io/subculture-collective/vod-tender:latest \
  --cpu 2 \
  --memory 4 \
  --port 8080 \
  --dns-name-label vod-tender-api \
  --environment-variables \
    LOG_FORMAT=json \
    DATA_DIR=/data \
  --secure-environment-variables \
    DB_DSN=$DB_DSN \
    TWITCH_CLIENT_ID=$TWITCH_CLIENT_ID \
  --azure-file-volume-account-name vodstorageaccount \
  --azure-file-volume-account-key $STORAGE_KEY \
  --azure-file-volume-share-name vod-data \
  --azure-file-volume-mount-path /data
```

#### AKS (Azure Kubernetes Service)

```bash
# Create AKS cluster
az aks create \
  --resource-group vod-tender \
  --name vod-tender-cluster \
  --node-count 2 \
  --node-vm-size Standard_D4s_v3 \
  --enable-managed-identity \
  --generate-ssh-keys

# Get credentials
az aks get-credentials --resource-group vod-tender --name vod-tender-cluster

# Deploy using Kubernetes manifests
kubectl apply -f k8s/
```

## Zero-Downtime Upgrades

### Kubernetes Rolling Update

```bash
# Update image version
kubectl set image deployment/vod-api \
  api=ghcr.io/subculture-collective/vod-tender:v2 \
  -n vod-tender

# Monitor rollout
kubectl rollout status deployment/vod-api -n vod-tender

# Rollback if needed
kubectl rollout undo deployment/vod-api -n vod-tender
```

**Pre-upgrade checklist**:

1. Backup database: `kubectl exec -n vod-tender postgres-0 -- pg_dump -U vod vod > backup.sql`
2. Review changelog and breaking changes
3. Test in staging environment
4. Verify health checks pass: `kubectl get pods -n vod-tender`

### Docker Swarm Rolling Update

```bash
# Update with zero downtime
docker service update \
  --image ghcr.io/subculture-collective/vod-tender:v2 \
  --update-parallelism 1 \
  --update-delay 10s \
  vod_api

# Monitor update
docker service ps vod_api

# Rollback if needed
docker service rollback vod_api
```

### Bare Metal Blue-Green Deployment

```bash
# 1. Deploy new version alongside old
sudo cp /opt/vod-tender/bin/vod-tender /opt/vod-tender/bin/vod-tender.old
sudo wget -O /opt/vod-tender/bin/vod-tender.new https://github.com/.../vod-tender-v2
sudo chmod +x /opt/vod-tender/bin/vod-tender.new

# 2. Test new version on alternate port
sudo -u vod PORT=8081 /opt/vod-tender/bin/vod-tender.new &
# Verify: curl http://localhost:8081/healthz

# 3. Switch traffic
sudo systemctl stop vod-tender
sudo mv /opt/vod-tender/bin/vod-tender.new /opt/vod-tender/bin/vod-tender
sudo systemctl start vod-tender

# 4. Verify and cleanup
sudo systemctl status vod-tender
sudo rm /opt/vod-tender/bin/vod-tender.old
```

### Database Migrations

vod-tender runs migrations automatically on startup. For zero-downtime:

1. **Backward-compatible changes first**: Add new columns/tables
2. **Deploy new application version**: Uses new schema
3. **Remove old columns/tables**: After confirming rollback is not needed

**Manual migration** (if needed):

```bash
# Kubernetes
kubectl exec -it deployment/vod-api -n vod-tender -- /bin/sh
# Inside container: migrations run automatically

# Docker Swarm / Bare Metal
sudo -u vod psql -U vod -d vod -f /path/to/migration.sql
```

## Post-Deployment Checklist

### Functional Verification

- [ ] API health check returns 200: `curl https://vod-api.example.com/healthz`
- [ ] Status endpoint shows expected state: `curl https://vod-api.example.com/status`
- [ ] Metrics endpoint accessible: `curl https://vod-api.example.com/metrics`
- [ ] Frontend loads correctly: Visit `https://vod-tender.example.com`
- [ ] Database connection successful: Check logs for `connected to database`
- [ ] Chat recording starts: Check logs for `chat recorder started`
- [ ] VOD catalog backfill runs: Check logs for `catalog backfill completed`

### Security Verification

- [ ] HTTPS/TLS configured and working
- [ ] Secrets not visible in logs or environment dumps
- [ ] Database credentials rotated from defaults
- [ ] Network policies/firewall rules applied
- [ ] Rate limiting configured
- [ ] Security headers present in HTTP responses

### Monitoring Setup

- [ ] Prometheus scraping metrics endpoint
- [ ] Grafana dashboard imported and displaying data
- [ ] Log aggregation collecting structured logs
- [ ] Alerts configured for critical conditions
- [ ] Uptime monitoring active

### Backup Verification

- [ ] Database backup job running
- [ ] Backup files created successfully
- [ ] Restore test passed
- [ ] Offsite backup copy verified

### Documentation

- [ ] Runbook updated with environment-specific details
- [ ] Credentials stored in password manager
- [ ] On-call rotation configured
- [ ] Incident communication channels established

## Troubleshooting

### Common Deployment Issues

**Problem**: Pods/containers in CrashLoopBackOff

```bash
# Check logs
kubectl logs deployment/vod-api -n vod-tender --previous
docker service logs vod_api

# Common causes:
# - Missing environment variables
# - Database connection failure
# - Invalid secrets
```

**Problem**: Database connection refused

```bash
# Verify database is running
kubectl get pods -n vod-tender | grep postgres
docker service ps vod_postgres

# Check connection from pod
kubectl exec -it deployment/vod-api -n vod-tender -- nc -zv postgres 5432

# Verify credentials
kubectl get secret vod-postgres-creds -n vod-tender -o yaml
```

**Problem**: Persistent volume not mounting

```bash
# Check PVC status
kubectl get pvc -n vod-tender
# Should show "Bound" status

# Check events
kubectl describe pvc vod-data -n vod-tender

# Verify storage class exists
kubectl get storageclass
```

**Problem**: Ingress not routing traffic

```bash
# Check ingress status
kubectl get ingress -n vod-tender
kubectl describe ingress vod-tender -n vod-tender

# Verify cert-manager certificate
kubectl get certificate -n vod-tender

# Test internal service
kubectl port-forward svc/vod-api 8080:8080 -n vod-tender
curl http://localhost:8080/healthz
```

## Next Steps

- Configure monitoring: See [RUNBOOKS.md](./RUNBOOKS.md#monitoring-setup-guide)
- Set up alerting: See [RUNBOOKS.md](./RUNBOOKS.md#alert-rule-examples)
- Review security hardening: See [SECURITY_HARDENING.md](./SECURITY_HARDENING.md)
- Performance tuning: See [PERFORMANCE.md](./PERFORMANCE.md)
