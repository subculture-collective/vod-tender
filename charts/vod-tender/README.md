# vod-tender Helm Chart

Official Helm chart for deploying vod-tender - a Twitch VOD archival and chat recorder system.

## TL;DR

```bash
helm repo add vod-tender https://subculture-collective.github.io/vod-tender
helm install vod-tender vod-tender/vod-tender \
  --set config.twitchChannel=your_channel \
  --set config.twitchBotUsername=your_bot \
  --set secrets.twitch.clientId=abc123 \
  --set secrets.twitch.clientSecret=xyz789
```

## Introduction

This chart bootstraps a vod-tender deployment on a Kubernetes cluster using the Helm package manager. It includes:

- API backend (single replica for single-channel concurrency)
- Frontend web UI (horizontally scalable)
- PostgreSQL database (StatefulSet with persistent storage)
- Ingress with TLS termination
- Network policies for security
- Optional HorizontalPodAutoscaler for frontend
- Optional ServiceMonitor for Prometheus metrics

## Prerequisites

- Kubernetes 1.28+
- Helm 3.8+
- PV provisioner support in the underlying infrastructure
- Ingress controller (e.g., nginx-ingress) if ingress is enabled
- cert-manager for TLS certificates (optional but recommended)
- Prometheus Operator for ServiceMonitor (optional)

## Installing the Chart

### From source

```bash
# Clone repository
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender

# Install with default values
helm install vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  --create-namespace

# Install with custom values file
helm install vod-tender ./charts/vod-tender \
  -f charts/vod-tender/values-production.yaml \
  --namespace vod-tender \
  --create-namespace

# Install with command-line overrides
helm install vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  --create-namespace \
  --set config.twitchChannel=your_channel \
  --set config.twitchBotUsername=your_bot \
  --set secrets.twitch.clientId=abc123 \
  --set secrets.twitch.clientSecret=xyz789 \
  --set ingress.frontend.host=vod-tender.example.com \
  --set ingress.api.host=vod-api.example.com
```

### From Helm repository (when published)

```bash
helm repo add vod-tender https://subculture-collective.github.io/vod-tender
helm repo update
helm install vod-tender vod-tender/vod-tender \
  --namespace vod-tender \
  --create-namespace \
  -f my-values.yaml
```

## Uninstalling the Chart

```bash
helm uninstall vod-tender -n vod-tender
```

This removes all Kubernetes resources associated with the chart but preserves PersistentVolumeClaims by default.

To delete PVCs:

```bash
kubectl delete pvc -n vod-tender --all
```

## Configuration

The following table lists the configurable parameters of the vod-tender chart and their default values.

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full release name | `""` |
| `imagePullSecrets` | Image pull secrets for private registries | `[]` |

### Image Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.api.repository` | API container image repository | `ghcr.io/subculture-collective/vod-tender-api` |
| `image.api.tag` | API container image tag | `latest` |
| `image.api.pullPolicy` | API image pull policy | `IfNotPresent` |
| `image.frontend.repository` | Frontend container image repository | `ghcr.io/subculture-collective/vod-tender-frontend` |
| `image.frontend.tag` | Frontend container image tag | `latest` |
| `image.frontend.pullPolicy` | Frontend image pull policy | `IfNotPresent` |
| `image.postgres.repository` | Postgres container image repository | `postgres` |
| `image.postgres.tag` | Postgres container image tag | `17-alpine` |
| `image.postgres.pullPolicy` | Postgres image pull policy | `IfNotPresent` |

### Application Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.twitchChannel` | Twitch channel name (required) | `""` |
| `config.twitchBotUsername` | Twitch bot username (required) | `""` |
| `config.chatAutoStart` | Enable automatic live chat recording | `"1"` |
| `config.vodCatalogBackfillInterval` | VOD catalog backfill interval | `"6h"` |
| `config.vodProcessInterval` | VOD processing check interval | `"1m"` |
| `config.processingRetryCooldown` | Retry cooldown for failed processing (seconds) | `"600"` |
| `config.downloadMaxAttempts` | Max download retry attempts | `"5"` |
| `config.downloadBackoffBase` | Download retry backoff base | `"2s"` |
| `config.uploadMaxAttempts` | Max upload retry attempts | `"5"` |
| `config.uploadBackoffBase` | Upload retry backoff base | `"2s"` |
| `config.circuitOpenCooldown` | Circuit breaker open cooldown | `"5m"` |
| `config.dataDir` | Data directory path in container | `"/data"` |
| `config.backfillAutoclean` | Auto-clean files after upload | `"1"` |
| `config.retainKeepNewerThanDays` | Keep VODs newer than N days | `"7"` |
| `config.backfillUploadDailyLimit` | Daily upload limit for backfill | `"10"` |
| `config.chatAutoPollInterval` | Live status poll interval | `"30s"` |
| `config.vodReconcileDelay` | Delay before chat reconciliation | `"1m"` |

### Secrets Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `secrets.create` | Create Secret resource | `true` |
| `secrets.existingSecret` | Use existing secret name | `""` |
| `secrets.twitch.clientId` | Twitch OAuth client ID | `""` |
| `secrets.twitch.clientSecret` | Twitch OAuth client secret | `""` |
| `secrets.twitch.oauthToken` | Twitch OAuth token | `""` |
| `secrets.twitch.redirectUri` | Twitch OAuth redirect URI | `"https://vod-api.example.com/auth/twitch/callback"` |
| `secrets.twitch.scopes` | Twitch OAuth scopes | `"chat:read chat:edit"` |
| `secrets.youtube.clientId` | YouTube OAuth client ID | `""` |
| `secrets.youtube.clientSecret` | YouTube OAuth client secret | `""` |
| `secrets.youtube.redirectUri` | YouTube OAuth redirect URI | `"https://vod-api.example.com/auth/youtube/callback"` |
| `secrets.youtube.scopes` | YouTube OAuth scopes | `"https://www.googleapis.com/auth/youtube.upload"` |
| `secrets.postgres.password` | Postgres password | `"vod"` |

### API Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `api.replicas` | Number of API replicas (must be 1) | `1` |
| `api.strategy.type` | Deployment strategy | `Recreate` |
| `api.resources.requests.memory` | Memory request | `512Mi` |
| `api.resources.requests.cpu` | CPU request | `500m` |
| `api.resources.limits.memory` | Memory limit | `2Gi` |
| `api.resources.limits.cpu` | CPU limit | `2000m` |
| `api.service.type` | Service type | `ClusterIP` |
| `api.service.port` | Service port | `8080` |
| `api.securityContext.runAsUser` | Run as UID | `1000` |
| `api.securityContext.fsGroup` | FSGroup | `1000` |

### Frontend Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `frontend.replicas` | Number of frontend replicas | `2` |
| `frontend.autoscaling.enabled` | Enable HorizontalPodAutoscaler | `false` |
| `frontend.autoscaling.minReplicas` | Minimum replicas | `2` |
| `frontend.autoscaling.maxReplicas` | Maximum replicas | `5` |
| `frontend.autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization | `80` |
| `frontend.autoscaling.targetMemoryUtilizationPercentage` | Target memory utilization | `80` |
| `frontend.resources.requests.memory` | Memory request | `128Mi` |
| `frontend.resources.requests.cpu` | CPU request | `100m` |
| `frontend.resources.limits.memory` | Memory limit | `256Mi` |
| `frontend.resources.limits.cpu` | CPU limit | `500m` |
| `frontend.service.type` | Service type | `ClusterIP` |
| `frontend.service.port` | Service port | `80` |
| `frontend.podDisruptionBudget.enabled` | Enable PodDisruptionBudget | `true` |
| `frontend.podDisruptionBudget.minAvailable` | Minimum available pods | `1` |

### PostgreSQL Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgres.enabled` | Enable PostgreSQL StatefulSet | `true` |
| `postgres.replicas` | Number of replicas | `1` |
| `postgres.database` | Database name | `vod` |
| `postgres.username` | Database username | `vod` |
| `postgres.persistence.enabled` | Enable persistence | `true` |
| `postgres.persistence.size` | PVC size | `20Gi` |
| `postgres.persistence.storageClass` | Storage class | `""` (default) |
| `postgres.persistence.accessMode` | Access mode | `ReadWriteOnce` |
| `postgres.resources.requests.memory` | Memory request | `256Mi` |
| `postgres.resources.requests.cpu` | CPU request | `250m` |
| `postgres.resources.limits.memory` | Memory limit | `1Gi` |
| `postgres.resources.limits.cpu` | CPU limit | `1000m` |
| `postgres.service.type` | Service type | `ClusterIP` |
| `postgres.service.port` | Service port | `5432` |
| `postgres.podDisruptionBudget.enabled` | Enable PodDisruptionBudget | `true` |
| `postgres.podDisruptionBudget.maxUnavailable` | Max unavailable pods | `0` |

### Persistence Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.data.enabled` | Enable VOD data persistence | `true` |
| `persistence.data.size` | PVC size | `100Gi` |
| `persistence.data.storageClass` | Storage class | `""` (default) |
| `persistence.data.accessMode` | Access mode | `ReadWriteOnce` |

### Ingress Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Ingress | `true` |
| `ingress.className` | Ingress class name | `nginx` |
| `ingress.annotations` | Ingress annotations | See values.yaml |
| `ingress.frontend.host` | Frontend hostname | `vod-tender.example.com` |
| `ingress.frontend.path` | Frontend path | `/` |
| `ingress.frontend.pathType` | Frontend path type | `Prefix` |
| `ingress.api.host` | API hostname | `vod-api.example.com` |
| `ingress.api.path` | API path | `/` |
| `ingress.api.pathType` | API path type | `Prefix` |
| `ingress.tls.enabled` | Enable TLS | `true` |
| `ingress.tls.secretName` | TLS secret name | `vod-tender-tls` |

### Monitoring Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `monitoring.enabled` | Enable monitoring | `true` |
| `monitoring.serviceMonitor.enabled` | Enable ServiceMonitor | `true` |
| `monitoring.serviceMonitor.interval` | Scrape interval | `30s` |
| `monitoring.serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |
| `monitoring.grafana.enabled` | Deploy Grafana dashboard ConfigMap | `true` |

### Backup Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgres.backup.enabled` | Enable automated database backups | `false` |
| `postgres.backup.schedule` | Backup schedule (cron format) | `"0 2 * * *"` |
| `postgres.backup.retentionDays` | Days to retain backups | `7` |
| `postgres.backup.persistence.size` | Backup storage size | `10Gi` |
| `postgres.backup.persistence.storageClass` | Storage class for backups | `""` |
| `postgres.backup.s3.enabled` | Enable S3 upload | `false` |
| `postgres.backup.s3.bucket` | S3 bucket name | `""` |
| `postgres.backup.s3.region` | AWS region | `us-east-1` |
| `postgres.backup.resources.requests.memory` | Memory request | `128Mi` |
| `postgres.backup.resources.requests.cpu` | CPU request | `100m` |
| `postgres.backup.resources.limits.memory` | Memory limit | `512Mi` |
| `postgres.backup.resources.limits.cpu` | CPU limit | `500m` |

### Network Policy Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `networkPolicy.enabled` | Enable NetworkPolicies | `true` |

### External Secrets Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `externalSecrets.enabled` | Enable External Secrets Operator | `false` |
| `externalSecrets.refreshInterval` | Secret refresh interval | `1h` |
| `externalSecrets.secretStoreRef.name` | SecretStore name | `aws-secrets-manager` |
| `externalSecrets.secretStoreRef.kind` | SecretStore kind | `ClusterSecretStore` |
| `externalSecrets.data` | Secret data mappings | See values.yaml |

## Examples

### Development Deployment

```bash
helm install vod-tender-dev ./charts/vod-tender \
  -f charts/vod-tender/values-dev.yaml \
  --namespace vod-tender-dev \
  --create-namespace \
  --set config.twitchChannel=test_channel \
  --set config.twitchBotUsername=test_bot \
  --set secrets.twitch.clientId=dev_client_id \
  --set secrets.twitch.clientSecret=dev_secret
```

### Production Deployment with External Secrets

```bash
# First, create ExternalSecret mappings in your secret store
# Then deploy with External Secrets enabled

helm install vod-tender ./charts/vod-tender \
  -f charts/vod-tender/values-production.yaml \
  --namespace vod-tender \
  --create-namespace \
  --set config.twitchChannel=production_channel \
  --set config.twitchBotUsername=production_bot \
  --set secrets.create=false \
  --set externalSecrets.enabled=true \
  --set externalSecrets.secretStoreRef.name=aws-secrets-manager \
  --set ingress.frontend.host=vod-tender.yourdomain.com \
  --set ingress.api.host=vod-api.yourdomain.com
```

### Multi-Channel Deployment

Deploy separate releases for each channel:

```bash
# Channel A
helm install vod-channel-a ./charts/vod-tender \
  --namespace vod-channel-a \
  --create-namespace \
  --set config.twitchChannel=channel_a \
  --set ingress.frontend.host=vod-channel-a.example.com \
  --set ingress.api.host=vod-api-channel-a.example.com

# Channel B
helm install vod-channel-b ./charts/vod-tender \
  --namespace vod-channel-b \
  --create-namespace \
  --set config.twitchChannel=channel_b \
  --set ingress.frontend.host=vod-channel-b.example.com \
  --set ingress.api.host=vod-api-channel-b.example.com
```

### Using External Database

```bash
helm install vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  --create-namespace \
  --set postgres.enabled=false \
  --set secrets.create=true \
  --set secrets.postgres.password="" \
  --set-string postgres.externalDsn="postgres://user:pass@external-db:5432/vod?sslmode=require"
```

## Upgrading

### To a new chart version

```bash
helm repo update
helm upgrade vod-tender vod-tender/vod-tender \
  --namespace vod-tender \
  -f my-values.yaml
```

### From source

```bash
git pull
helm upgrade vod-tender ./charts/vod-tender \
  --namespace vod-tender \
  -f my-values.yaml
```

### Rollback

```bash
# List releases
helm history vod-tender -n vod-tender

# Rollback to previous version
helm rollback vod-tender -n vod-tender

# Rollback to specific revision
helm rollback vod-tender 3 -n vod-tender
```

## Troubleshooting

### Check release status

```bash
helm status vod-tender -n vod-tender
helm get all vod-tender -n vod-tender
```

### Validate before install

```bash
helm install vod-tender ./charts/vod-tender \
  --dry-run --debug \
  -f my-values.yaml
```

### Template rendering

```bash
helm template vod-tender ./charts/vod-tender \
  -f my-values.yaml \
  > rendered-manifests.yaml
```

### Common issues

**Pods not starting:**

- Check pod events: `kubectl describe pod -n vod-tender <pod-name>`
- Check logs: `kubectl logs -n vod-tender <pod-name>`
- Verify secrets exist: `kubectl get secret -n vod-tender`
- Verify ConfigMap exists: `kubectl get configmap -n vod-tender`

**PVC issues:**

- Check PVC status: `kubectl get pvc -n vod-tender`
- Verify StorageClass exists: `kubectl get storageclass`
- Check PV provisioner logs

**Ingress not working:**

- Verify ingress controller is running
- Check ingress status: `kubectl describe ingress -n vod-tender`
- Verify DNS points to ingress controller LoadBalancer
- Check cert-manager certificate: `kubectl get certificate -n vod-tender`

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](../../CONTRIBUTING.md) for guidelines.

## License

See [LICENSE](../../LICENSE) file in the repository root.

## Links

- [Project Homepage](https://github.com/subculture-collective/vod-tender)
- [Documentation](https://github.com/subculture-collective/vod-tender/tree/main/docs)
- [Issues](https://github.com/subculture-collective/vod-tender/issues)
