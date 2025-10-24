# Kubernetes Manifests

This directory contains Kubernetes deployment manifests for vod-tender, organized using Kustomize for multi-environment support.

## Directory Structure

```
k8s/
├── base/                     # Base manifests (common across all environments)
│   ├── namespace.yaml       # vod-tender namespace
│   ├── configmap.yaml       # Environment configuration
│   ├── secret.yaml          # Secrets template (DO NOT commit real credentials)
│   ├── pvc.yaml             # PersistentVolumeClaim for VOD storage
│   ├── api-deployment.yaml  # API backend deployment
│   ├── frontend-deployment.yaml  # Frontend deployment
│   ├── postgres-statefulset.yaml # Postgres StatefulSet with PVC
│   ├── services.yaml        # Service definitions
│   ├── ingress.yaml         # Ingress with TLS termination
│   ├── networkpolicy.yaml   # Network policies for pod isolation
│   └── kustomization.yaml   # Base kustomization
├── overlays/                # Environment-specific configurations
│   ├── dev/                 # Development environment
│   ├── staging/             # Staging environment
│   └── production/          # Production environment with HPA, PDB, ServiceMonitor
└── README.md               # This file
```

## Resource Descriptions

### Base Resources

#### namespace.yaml
Creates the `vod-tender` namespace for resource isolation.

#### configmap.yaml
Contains non-sensitive configuration:
- Twitch channel settings
- Feature toggles (CHAT_AUTO_START)
- Processing intervals and retry settings
- Circuit breaker configuration
- Storage paths

#### secret.yaml
**IMPORTANT**: This is a template only. Do NOT commit real credentials.

Contains sensitive data:
- Database connection string
- Twitch OAuth credentials (client ID, secret, token)
- YouTube OAuth credentials
- Redirect URIs

For production, use:
- External Secrets Operator (recommended)
- Sealed Secrets
- Cloud provider secret managers (AWS Secrets Manager, GCP Secret Manager)

#### pvc.yaml
PersistentVolumeClaim for VOD storage (`/data` directory):
- Default: 100Gi
- Access mode: ReadWriteOnce
- Update `storageClassName` for your cloud provider

#### api-deployment.yaml
API backend deployment with:
- **Replicas**: 1 (single-channel concurrency model)
- **Strategy**: Recreate (prevents concurrent instances)
- **Init container**: Waits for Postgres
- **Resources**: 512Mi-2Gi memory, 500m-2000m CPU
- **Probes**:
  - Readiness: `/healthz` every 5s (starts at 10s)
  - Liveness: `/healthz` every 10s (starts at 30s)
- **Security**: Runs as UID 1000, fsGroup 1000
- **Volumes**: Mounts PVC at `/data`

#### frontend-deployment.yaml
Frontend deployment with:
- **Replicas**: 2 (stateless, horizontally scalable)
- **Strategy**: RollingUpdate (maxSurge: 1, maxUnavailable: 0)
- **Resources**: 128Mi-256Mi memory, 100m-500m CPU
- **Probes**: HTTP GET `/` on port 80
- **Security**: Runs as UID 101 (nginx)

#### postgres-statefulset.yaml
Postgres StatefulSet with:
- **Replicas**: 1
- **Image**: postgres:17-alpine
- **Resources**: 256Mi-1Gi memory, 250m-1000m CPU
- **Probes**: `pg_isready` command
- **Volume**: VolumeClaimTemplate with 20Gi storage
- **Security**: Runs as UID 70 (postgres)

#### services.yaml
Three ClusterIP services:
- `vod-tender-api`: Port 8080 → API pods
- `vod-tender-frontend`: Port 80 → Frontend pods
- `postgres`: Headless service for StatefulSet (port 5432)

#### ingress.yaml
Ingress with TLS termination:
- **Hosts**: `vod-tender.example.com` (frontend), `vod-api.example.com` (API)
- **TLS**: cert-manager integration (letsencrypt-prod issuer)
- **Annotations**:
  - SSL redirect enabled
  - No upload size limit (`proxy-body-size: 0`)

**Update hostnames** in overlays for your environment.

#### networkpolicy.yaml
Three NetworkPolicies for defense-in-depth:

1. **API Policy**:
   - Ingress: From frontend and ingress controller (port 8080)
   - Egress: To Postgres (5432), DNS (53), external HTTPS (443/80)

2. **Frontend Policy**:
   - Ingress: From ingress controller (port 80)
   - Egress: To API (8080), DNS (53)

3. **Postgres Policy**:
   - Ingress: From API only (port 5432)
   - Egress: DNS (53)

### Overlays

#### dev/
Development environment:
- Reduced storage (10Gi VOD, 5Gi Postgres)
- Single frontend replica
- Auto chat disabled (`CHAT_AUTO_START=0`)
- No auto-cleanup (`BACKFILL_AUTOCLEAN=0`)

#### staging/
Staging environment:
- Moderate storage (50Gi VOD, 10Gi Postgres)
- Reduced upload limit (5/day)

#### production/
Production environment with additional resources:
- **hpa.yaml**: HorizontalPodAutoscaler for frontend (2-5 replicas, 80% CPU/memory target)
- **pdb.yaml**: PodDisruptionBudgets to prevent simultaneous disruptions
- **servicemonitor.yaml**: Prometheus ServiceMonitor for metrics scraping

## Deployment

### Prerequisites

1. **Kubernetes cluster** (1.28+)
2. **kubectl** and **kustomize** installed
3. **Ingress controller** (e.g., nginx-ingress)
4. **cert-manager** for TLS certificates (optional but recommended)
5. **Prometheus Operator** for ServiceMonitor (production only)

### Deploy to Development

```bash
# Review what will be applied
kubectl kustomize k8s/overlays/dev

# Apply manifests
kubectl apply -k k8s/overlays/dev

# Check status
kubectl get pods -n vod-tender-dev
kubectl logs -n vod-tender-dev deployment/dev-vod-tender-api --follow
```

### Deploy to Production

```bash
# Update secrets first (DO NOT use template values)
kubectl create secret generic vod-tender-secrets \
  --namespace vod-tender \
  --from-literal=db-dsn='postgres://vod:SECURE_PASSWORD@postgres:5432/vod?sslmode=require' \
  --from-literal=postgres-password='SECURE_PASSWORD' \
  --from-literal=twitch-client-id='your_client_id' \
  --from-literal=twitch-client-secret='your_client_secret' \
  --from-literal=twitch-oauth-token='oauth:your_token' \
  --dry-run=client -o yaml | kubectl apply -f -

# Update ConfigMap values
kubectl create configmap vod-tender-config \
  --namespace vod-tender \
  --from-literal=TWITCH_CHANNEL='your_channel' \
  --from-literal=TWITCH_BOT_USERNAME='your_bot' \
  --dry-run=client -o yaml | kubectl apply -f -

# Update ingress hostnames in kustomization or via patch

# Apply manifests
kubectl apply -k k8s/overlays/production

# Watch rollout
kubectl rollout status deployment/vod-tender-frontend -n vod-tender
kubectl rollout status deployment/vod-tender-api -n vod-tender

# Check status
kubectl get all -n vod-tender
```

### Using External Secrets Operator

For production, integrate External Secrets Operator:

```yaml
# external-secret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: vod-tender-secrets
  namespace: vod-tender
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: vod-tender-secrets
    creationPolicy: Owner
  data:
  - secretKey: twitch-client-id
    remoteRef:
      key: vod-tender/twitch
      property: client_id
  - secretKey: twitch-client-secret
    remoteRef:
      key: vod-tender/twitch
      property: client_secret
  # ... other secrets
```

Add to `k8s/overlays/production/kustomization.yaml`:

```yaml
resources:
  - external-secret.yaml
```

## Validation

### Test Kustomize Build

```bash
# Validate base
kubectl kustomize k8s/base

# Validate overlays
kubectl kustomize k8s/overlays/dev
kubectl kustomize k8s/overlays/staging
kubectl kustomize k8s/overlays/production
```

### Verify Deployment

```bash
# Check pods
kubectl get pods -n vod-tender

# Check services
kubectl get svc -n vod-tender

# Check ingress
kubectl get ingress -n vod-tender

# Test healthz endpoint
kubectl port-forward -n vod-tender svc/vod-tender-api 8080:8080
curl http://localhost:8080/healthz

# Check logs
kubectl logs -n vod-tender deployment/vod-tender-api --tail=50
kubectl logs -n vod-tender deployment/vod-tender-frontend --tail=50
kubectl logs -n vod-tender statefulset/postgres --tail=50
```

### Test Network Policies

```bash
# From API pod, should reach Postgres
kubectl exec -n vod-tender deployment/vod-tender-api -- \
  nc -zv postgres 5432

# From frontend pod, should reach API
kubectl exec -n vod-tender deployment/vod-tender-frontend -- \
  wget -O- http://vod-tender-api:8080/healthz
```

## Troubleshooting

### Pod not starting

```bash
# Describe pod for events
kubectl describe pod -n vod-tender <pod-name>

# Check logs
kubectl logs -n vod-tender <pod-name> --previous
```

### PVC issues

```bash
# Check PVC status
kubectl get pvc -n vod-tender

# Describe PVC
kubectl describe pvc -n vod-tender vod-tender-data

# Check available StorageClasses
kubectl get storageclass
```

### Ingress not working

```bash
# Check ingress
kubectl describe ingress -n vod-tender vod-tender-ingress

# Check cert-manager certificate
kubectl get certificate -n vod-tender

# Check ingress controller logs
kubectl logs -n ingress-nginx deployment/ingress-nginx-controller
```

### Database connection issues

```bash
# Check Postgres logs
kubectl logs -n vod-tender statefulset/postgres

# Test connection from API pod
kubectl exec -n vod-tender deployment/vod-tender-api -- \
  sh -c 'apk add postgresql-client && psql $DB_DSN -c "SELECT 1"'
```

## Scaling

### Frontend (Horizontal)

```bash
# Manual scaling
kubectl scale deployment vod-tender-frontend -n vod-tender --replicas=3

# With HPA (production)
kubectl autoscale deployment vod-tender-frontend -n vod-tender \
  --min=2 --max=5 --cpu-percent=80
```

### API (Vertical Only)

⚠️ **Do NOT scale API horizontally** - single-channel concurrency model requires exactly 1 replica.

To handle more load:
1. Increase resource limits in deployment
2. Use larger node instance type
3. Run separate instances per channel (multi-instance pattern)

### Storage

To resize PVC (if supported by StorageClass):

```bash
kubectl patch pvc vod-tender-data -n vod-tender \
  -p '{"spec":{"resources":{"requests":{"storage":"200Gi"}}}}'
```

## Cleanup

```bash
# Delete development environment
kubectl delete -k k8s/overlays/dev

# Delete production environment (keeps PVCs)
kubectl delete -k k8s/overlays/production

# Delete PVCs (data loss!)
kubectl delete pvc --all -n vod-tender

# Delete namespace (everything)
kubectl delete namespace vod-tender
```

## Migration from Docker Compose

See [docs/KUBERNETES.md](../docs/KUBERNETES.md) for detailed migration guide.

Quick comparison:

| Docker Compose | Kubernetes |
|----------------|------------|
| `docker compose up` | `kubectl apply -k k8s/overlays/dev` |
| `docker compose logs -f api` | `kubectl logs -f deployment/vod-tender-api -n vod-tender` |
| `docker compose restart api` | `kubectl rollout restart deployment/vod-tender-api -n vod-tender` |
| `.env` file | ConfigMap + Secret |
| `volumes:` | PersistentVolumeClaim |
| Port mapping | Ingress + Service |

## See Also

- [Helm Chart](../charts/vod-tender/README.md) - Alternative deployment method
- [Kubernetes Deployment Guide](../docs/KUBERNETES.md) - Comprehensive guide
- [Operations Guide](../docs/OPERATIONS.md) - Multi-instance patterns
