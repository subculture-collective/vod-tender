# Kubernetes Deployment Guide

This guide covers deploying vod-tender on Kubernetes using either raw manifests with Kustomize or the Helm chart.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Deployment Methods](#deployment-methods)
  - [Method 1: Kustomize (kubectl)](#method-1-kustomize-kubectl)
  - [Method 2: Helm Chart](#method-2-helm-chart)
- [Configuration](#configuration)
- [Migration from Docker Compose](#migration-from-docker-compose)
- [Multi-Channel Deployment](#multi-channel-deployment)
- [Production Considerations](#production-considerations)
- [Monitoring and Observability](#monitoring-and-observability)
- [Backup and Disaster Recovery](#backup-and-disaster-recovery)
- [Troubleshooting](#troubleshooting)

## Overview

vod-tender supports Kubernetes deployment for production-grade orchestration, scaling, and reliability. The deployment includes:

- **API Backend**: Single replica deployment (single-channel concurrency model)
- **Frontend**: Horizontally scalable web UI with optional HPA
- **PostgreSQL**: StatefulSet with persistent storage
- **Ingress**: TLS-terminated external access
- **Network Policies**: Pod-to-pod communication restrictions
- **Monitoring**: Prometheus ServiceMonitor integration

## Prerequisites

### Required

- **Kubernetes cluster** (1.28 or later)
  - Managed: EKS, GKE, AKS, DigitalOcean Kubernetes
  - Self-hosted: kubeadm, k3s, kind, minikube
- **kubectl** CLI (matching cluster version)
- **Storage provisioner** for PersistentVolumes
  - Cloud: EBS (AWS), Persistent Disks (GCP), Azure Disks
  - Self-hosted: Longhorn, OpenEBS, Rook Ceph

### Optional but Recommended

- **Helm** 3.8+ (for chart-based deployment)
- **Ingress Controller** (nginx-ingress, Traefik, etc.)
- **cert-manager** for automatic TLS certificate management
- **Prometheus Operator** for ServiceMonitor support
- **External Secrets Operator** for secret management (AWS Secrets Manager, Vault, etc.)

### Local Development/Testing

- **kind** (Kubernetes in Docker): `brew install kind` or `go install sigs.k8s.io/kind@latest`
- **k3s** (lightweight Kubernetes): `curl -sfL https://get.k3s.io | sh -`
- **minikube**: `brew install minikube` or download from <https://minikube.sigs.k8s.io/>

## Quick Start

### Using Helm (Recommended)

```bash
# Clone repository
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender

# Install chart
helm install vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  --create-namespace \
  --set config.twitchChannel=your_channel \
  --set config.twitchBotUsername=your_bot \
  --set secrets.twitch.clientId=abc123 \
  --set secrets.twitch.clientSecret=xyz789 \
  --set ingress.frontend.host=vod-tender.yourdomain.com \
  --set ingress.api.host=vod-api.yourdomain.com

# Check status
kubectl get pods -n vod-tender
```

### Using kubectl + Kustomize

```bash
# Clone repository
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender

# Update secrets in k8s/base/secret.yaml (DO NOT commit!)
# Then apply manifests
kubectl apply -k k8s/overlays/production

# Check status
kubectl get pods -n vod-tender
```

## Deployment Methods

### Method 1: Kustomize (kubectl)

Kustomize allows managing Kubernetes manifests with environment-specific overlays.

#### Directory Structure

```
k8s/
├── base/          # Base manifests
└── overlays/      # Environment-specific patches
    ├── dev/
    ├── staging/
    └── production/
```

#### Step-by-Step Deployment

**1. Update Configuration**

Edit `k8s/base/configmap.yaml`:

```yaml
data:
  TWITCH_CHANNEL: "your_channel_name"
  TWITCH_BOT_USERNAME: "your_bot_username"
  # ... other config
```

**2. Update Secrets**

⚠️ **Security Warning**: Never commit real secrets to version control!

For development, edit `k8s/base/secret.yaml` directly.

For production, use one of these methods:

**Option A: Create secret imperatively**

```bash
kubectl create secret generic vod-tender-secrets \
  --namespace vod-tender \
  --from-literal=db-dsn='postgres://vod:SECURE_PASS@postgres:5432/vod?sslmode=require' \
  --from-literal=postgres-password='SECURE_PASS' \
  --from-literal=twitch-client-id='abc123' \
  --from-literal=twitch-client-secret='xyz789' \
  --from-literal=twitch-oauth-token='oauth:token' \
  --from-literal=yt-client-id='' \
  --from-literal=yt-client-secret='' \
  --dry-run=client -o yaml | kubectl apply -f -
```

**Option B: Use External Secrets Operator** (recommended)

See [Production Considerations](#production-considerations) section.

**3. Update Ingress Hostnames**

Edit `k8s/base/ingress.yaml` or overlay patches:

```yaml
spec:
  tls:
  - hosts:
    - vod-tender.yourdomain.com
    - vod-api.yourdomain.com
  rules:
  - host: vod-tender.yourdomain.com
    # ...
  - host: vod-api.yourdomain.com
    # ...
```

**4. Deploy**

```bash
# Development
kubectl apply -k k8s/overlays/dev

# Staging
kubectl apply -k k8s/overlays/staging

# Production
kubectl apply -k k8s/overlays/production
```

**5. Verify Deployment**

```bash
# Check all resources
kubectl get all -n vod-tender

# Check pods
kubectl get pods -n vod-tender -o wide

# Check PVCs
kubectl get pvc -n vod-tender

# Check ingress
kubectl get ingress -n vod-tender

# Watch logs
kubectl logs -n vod-tender deployment/vod-tender-api --follow
```

### Method 2: Helm Chart

Helm provides templating and release management for Kubernetes deployments.

#### Installation

**1. Prepare values file**

Create `my-values.yaml`:

```yaml
config:
  twitchChannel: "your_channel"
  twitchBotUsername: "your_bot"
  chatAutoStart: "1"

secrets:
  create: true
  twitch:
    clientId: "abc123"
    clientSecret: "xyz789"
    oauthToken: "oauth:token"
  youtube:
    clientId: ""
    clientSecret: ""
  postgres:
    password: "SECURE_PASSWORD"

ingress:
  frontend:
    host: vod-tender.yourdomain.com
  api:
    host: vod-api.yourdomain.com
  tls:
    secretName: vod-tender-tls

persistence:
  data:
    size: 100Gi
    storageClass: "gp3"  # AWS EBS

postgres:
  persistence:
    size: 20Gi
    storageClass: "gp3"
```

**2. Install chart**

```bash
helm install vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  --create-namespace \
  -f my-values.yaml
```

**3. Verify installation**

```bash
helm status vod-tender -n vod-tender
kubectl get pods -n vod-tender
```

#### Upgrades

```bash
# Update values file or chart
helm upgrade vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  -f my-values.yaml

# Check upgrade status
helm history vod-tender -n vod-tender
```

#### Rollback

```bash
# Rollback to previous version
helm rollback vod-tender -n vod-tender

# Rollback to specific revision
helm rollback vod-tender 3 -n vod-tender
```

#### Uninstall

```bash
# Remove release (keeps PVCs)
helm uninstall vod-tender -n vod-tender

# Delete PVCs (data loss!)
kubectl delete pvc -n vod-tender --all
```

## Configuration

### Environment Variables via ConfigMap

All non-sensitive configuration is stored in ConfigMap:

| Variable | Description | Default |
|----------|-------------|---------|
| `TWITCH_CHANNEL` | Channel name to monitor | (required) |
| `TWITCH_BOT_USERNAME` | Bot username for chat | (required) |
| `CHAT_AUTO_START` | Auto-start live chat recording | `1` |
| `VOD_CATALOG_BACKFILL_INTERVAL` | Catalog refresh interval | `6h` |
| `BACKFILL_AUTOCLEAN` | Delete files after upload | `1` |

See [CONFIG.md](CONFIG.md) for full list.

### Secrets via Secret Resource

Sensitive data is stored in Kubernetes Secret:

- Database connection string (`db-dsn`)
- Twitch OAuth credentials
- YouTube OAuth credentials
- Postgres password

**Production**: Use External Secrets Operator to sync from cloud secret managers.

### Resource Requests and Limits

Default resource allocations:

**API Backend:**

- Requests: 512Mi memory, 500m CPU
- Limits: 2Gi memory, 2000m CPU

**Frontend:**

- Requests: 128Mi memory, 100m CPU
- Limits: 256Mi memory, 500m CPU

**Postgres:**

- Requests: 256Mi memory, 250m CPU
- Limits: 1Gi memory, 1000m CPU

Adjust via Helm values or Kustomize patches based on workload.

### Storage

**VOD Data PVC:**

- Default: 100Gi
- Access mode: ReadWriteOnce
- Mounted at `/data` in API pods

**Postgres PVC:**

- Default: 20Gi (via VolumeClaimTemplate)
- Access mode: ReadWriteOnce

Update storage class and size based on cloud provider:

```yaml
# Helm values
persistence:
  data:
    size: 500Gi
    storageClass: "fast-ssd"

postgres:
  persistence:
    size: 50Gi
    storageClass: "gp3"
```

## Migration from Docker Compose

### Conceptual Mapping

| Docker Compose | Kubernetes |
|----------------|------------|
| `docker-compose.yml` | Deployment + Service + Ingress |
| `.env` file | ConfigMap + Secret |
| `volumes:` | PersistentVolumeClaim |
| Port mapping | Service + Ingress |
| Container networking | Service DNS + NetworkPolicy |

### Migration Steps

**1. Extract configuration**

From `backend/.env` → ConfigMap and Secret manifests

**2. Convert volumes**

Docker volumes → PersistentVolumeClaims with appropriate size

**3. Configure ingress**

Port mappings → Ingress rules with hostnames

**4. Deploy to Kubernetes**

```bash
# Stop Docker Compose
docker compose down

# Deploy to Kubernetes
kubectl apply -k k8s/overlays/production
# OR
helm install vod-tender ./charts/vod-tender -f values.yaml

# Migrate data (if needed)
kubectl cp /path/to/docker/volume/data \
  vod-tender/vod-tender-api-xxx:/data
```

**5. Verify functionality**

- Check API health: `https://vod-api.yourdomain.com/healthz`
- Access frontend: `https://vod-tender.yourdomain.com`
- Verify VOD processing continues

### Side-by-Side Testing

Run both Docker Compose and Kubernetes simultaneously for testing:

- Use different databases (or different DB names)
- Use different ingress hostnames
- Monitor both systems before cutover

## Multi-Channel Deployment

vod-tender's single-channel model requires one deployment per channel.

### Approach 1: Separate Namespaces (Helm)

```bash
# Channel A
helm install vod-channel-a ./charts/vod-tender \
  --namespace vod-channel-a \
  --create-namespace \
  --set config.twitchChannel=channel_a \
  --set ingress.frontend.host=channel-a.example.com \
  --set ingress.api.host=api-channel-a.example.com

# Channel B
helm install vod-channel-b ./charts/vod-tender \
  --namespace vod-channel-b \
  --create-namespace \
  --set config.twitchChannel=channel_b \
  --set ingress.frontend.host=channel-b.example.com \
  --set ingress.api.host=api-channel-b.example.com
```

### Approach 2: Kustomize with Multiple Overlays

```bash
k8s/overlays/
├── channel-a/
│   └── kustomization.yaml
└── channel-b/
    └── kustomization.yaml

# Deploy
kubectl apply -k k8s/overlays/channel-a
kubectl apply -k k8s/overlays/channel-b
```

### Resource Isolation

Each channel gets:

- Separate namespace
- Separate database (Postgres StatefulSet)
- Separate PVCs
- Separate ingress hostnames

### Shared Resources

Consider using shared infrastructure:

- External Postgres cluster (set `postgres.enabled=false`)
- Shared ingress controller
- Shared monitoring stack

## Production Considerations

### High Availability

**Frontend:**

- Enable HPA: `frontend.autoscaling.enabled=true`
- Set minReplicas ≥ 2
- Configure PodDisruptionBudget

**API:**

- Single replica only (concurrency constraint)
- Configure PodDisruptionBudget to prevent disruption
- Use `Recreate` strategy for updates

**Database:**

- Consider managed Postgres (RDS, Cloud SQL, etc.)
- If self-hosted, use backup/replication solution

### Security

**1. Network Policies**

Enabled by default, restricts pod-to-pod communication:

```yaml
networkPolicy:
  enabled: true
```

**2. External Secrets Operator**

Store secrets in AWS Secrets Manager, HashiCorp Vault, etc.:

```yaml
externalSecrets:
  enabled: true
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
```

**3. Pod Security Standards**

Apply pod security policies:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: vod-tender
  labels:
    pod-security.kubernetes.io/enforce: restricted
```

**4. TLS Everywhere**

- Enable ingress TLS
- Use cert-manager for automatic certificate renewal
- Consider service mesh (Istio, Linkerd) for internal mTLS

### Resource Management

**1. Resource Quotas**

Limit namespace resource consumption:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: vod-tender-quota
  namespace: vod-tender
spec:
  hard:
    requests.cpu: "10"
    requests.memory: 20Gi
    persistentvolumeclaims: "5"
```

**2. LimitRanges**

Set default limits for pods:

```yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: vod-tender-limits
  namespace: vod-tender
spec:
  limits:
  - max:
      memory: 4Gi
      cpu: "4"
    min:
      memory: 128Mi
      cpu: 100m
    type: Container
```

### Monitoring and Alerts

See [Monitoring and Observability](#monitoring-and-observability) section.

## Monitoring and Observability

### Prometheus Integration

**1. Install Prometheus Operator**

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

**2. Enable ServiceMonitor**

Helm:

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

Kustomize: Included in `k8s/overlays/production/servicemonitor.yaml`

**3. Access Grafana**

```bash
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80
# Open http://localhost:3000
# Default: admin/prom-operator
```

**4. Import Dashboard**

Import `docs/grafana-dashboard.json` for pre-built visualizations.

### Key Metrics

- `vod_downloads_started_total` - Download attempts counter
- `vod_queue_depth` - Pending VOD queue size
- `http_request_duration_seconds` - API request latency

### Logging

**1. Centralized Logging**

Deploy log aggregator (ELK, Loki, etc.):

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm install loki grafana/loki-stack \
  --namespace logging \
  --create-namespace
```

**2. View Logs**

```bash
# API logs
kubectl logs -n vod-tender deployment/vod-tender-api --follow

# Frontend logs
kubectl logs -n vod-tender deployment/vod-tender-frontend --follow

# Postgres logs
kubectl logs -n vod-tender statefulset/postgres --follow

# All logs with label filter
kubectl logs -n vod-tender -l app.kubernetes.io/name=vod-tender --follow
```

### Tracing (Optional)

For distributed tracing, integrate with Jaeger or Tempo.

## Backup and Disaster Recovery

### Database Backups

**Option 1: Automated CronJob (Recommended)**

The Kubernetes manifests and Helm chart include an optional automated backup CronJob that runs pg_dump daily and optionally uploads to S3.

**Using Kustomize:**

Uncomment the backup resources in `k8s/base/kustomization.yaml`:

```yaml
resources:
  # ... other resources ...
  - backup-pvc.yaml
  - backup-cronjob.yaml
```

Then apply:

```bash
kubectl apply -k k8s/overlays/production
```

**Using Helm:**

Enable backups in your values file:

```yaml
postgres:
  backup:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM UTC
    retentionDays: 7  # Keep last 7 days
    
    # Optional: Enable S3 uploads
    s3:
      enabled: true
      bucket: "my-backup-bucket"
      region: "us-east-1"
```

Then deploy with:

```bash
helm upgrade vod-tender ./charts/vod-tender -f values.yaml
```

**Manual backup example:**

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
  namespace: vod-tender
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:17-alpine
            command:
            - sh
            - -c
            - |
              pg_dump $DATABASE_URL | gzip > /backups/backup-$(date +%Y%m%d-%H%M%S).sql.gz
              # Upload to S3 or other storage
            env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: vod-tender-secrets
                  key: db-dsn
            volumeMounts:
            - name: backups
              mountPath: /backups
          volumes:
          - name: backups
            persistentVolumeClaim:
              claimName: postgres-backups
          restartPolicy: OnFailure
```

**Option 2: Managed Database Backups**

Use cloud provider automated backups (RDS, Cloud SQL).

### VOD Data Backups

PVC snapshots or sync to object storage:

```bash
# Create PVC snapshot (if supported)
kubectl create volumesnapshot vod-data-snapshot \
  --volume vod-tender-data \
  --snapshot-class csi-snapclass \
  --namespace vod-tender

# Or sync to S3
kubectl exec -n vod-tender deployment/vod-tender-api -- \
  aws s3 sync /data s3://my-backup-bucket/vod-data/
```

### Disaster Recovery Plan

1. **Backup Validation**: Test restores monthly
2. **RTO/RPO Targets**: Define acceptable downtime and data loss
3. **Runbook**: Document recovery procedures
4. **Multi-Region**: Consider cross-region replication for critical data

## Troubleshooting

### Pods Not Starting

**Check pod status:**

```bash
kubectl get pods -n vod-tender
kubectl describe pod -n vod-tender <pod-name>
```

**Common issues:**

- Image pull errors: Check `imagePullSecrets`
- Insufficient resources: Check cluster capacity
- PVC binding issues: Check PV provisioner
- Init container failures: Postgres not ready

### Database Connection Errors

**Check Postgres pod:**

```bash
kubectl logs -n vod-tender statefulset/postgres
kubectl exec -n vod-tender postgres-0 -- pg_isready -U vod
```

**Verify secret:**

```bash
kubectl get secret -n vod-tender vod-tender-secrets -o yaml
```

**Test connection from API pod:**

```bash
kubectl exec -n vod-tender deployment/vod-tender-api -- \
  sh -c 'apk add postgresql-client && psql $DB_DSN -c "SELECT 1"'
```

### Ingress Issues

**Check ingress status:**

```bash
kubectl describe ingress -n vod-tender vod-tender-ingress
```

**Verify ingress controller:**

```bash
kubectl get pods -n ingress-nginx
kubectl logs -n ingress-nginx deployment/ingress-nginx-controller
```

**Check DNS:**

```bash
nslookup vod-tender.yourdomain.com
```

**Check certificate:**

```bash
kubectl get certificate -n vod-tender
kubectl describe certificate -n vod-tender vod-tender-tls
```

### PVC Binding Issues

**Check PVC status:**

```bash
kubectl get pvc -n vod-tender
kubectl describe pvc -n vod-tender vod-tender-data
```

**Check StorageClass:**

```bash
kubectl get storageclass
kubectl describe storageclass <storage-class-name>
```

**Check PV provisioner logs** (cloud provider specific)

### Performance Issues

**Check resource usage:**

```bash
kubectl top pods -n vod-tender
kubectl top nodes
```

**Increase resources:**

```yaml
api:
  resources:
    limits:
      memory: 4Gi
      cpu: 4000m
```

**Scale frontend:**

```bash
kubectl scale deployment vod-tender-frontend -n vod-tender --replicas=5
```

### Recovery Procedures

**Restart pods:**

```bash
kubectl rollout restart deployment/vod-tender-api -n vod-tender
kubectl rollout restart deployment/vod-tender-frontend -n vod-tender
```

**Force delete stuck pod:**

```bash
kubectl delete pod <pod-name> -n vod-tender --grace-period=0 --force
```

**Restore from backup:**

```bash
# Restore database
kubectl exec -n vod-tender postgres-0 -- \
  sh -c 'gunzip < /backups/backup.sql.gz | psql -U vod -d vod'
```

## See Also

- [Kustomize Manifests README](../k8s/README.md)
- [Helm Chart README](../charts/vod-tender/README.md)
- [Configuration Reference](CONFIG.md)
- [Operations Guide](OPERATIONS.md)
- [Architecture Overview](ARCHITECTURE.md)
