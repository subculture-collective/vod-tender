#!/bin/bash
# Kubernetes Deployment Validation Script
# Tests vod-tender K8s manifests and Helm chart in a local kind cluster

set -e

echo "=== vod-tender Kubernetes Deployment Validation ==="
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Cluster name
CLUSTER_NAME="vod-tender-test"

# Function to print status
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✓${NC} $2"
    else
        echo -e "${RED}✗${NC} $2"
        exit 1
    fi
}

print_info() {
    echo -e "${YELLOW}→${NC} $1"
}

# Check prerequisites
print_info "Checking prerequisites..."
command -v kind >/dev/null 2>&1 || { echo "kind is required but not installed. Aborting."; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required but not installed. Aborting."; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "helm is required but not installed. Aborting."; exit 1; }
print_status 0 "Prerequisites installed"

# Create kind cluster
print_info "Creating kind cluster: $CLUSTER_NAME"
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    print_info "Cluster already exists, deleting..."
    kind delete cluster --name "$CLUSTER_NAME"
fi
kind create cluster --name "$CLUSTER_NAME" --wait 5m
print_status $? "Kind cluster created"

# Test 1: Validate kustomize build
print_info "Test 1: Validating kustomize base build..."
kubectl kustomize k8s/base > /dev/null
print_status $? "Kustomize base builds successfully"

print_info "Test 2: Validating kustomize overlays..."
kubectl kustomize k8s/overlays/dev > /dev/null
print_status $? "Dev overlay builds successfully"

kubectl kustomize k8s/overlays/staging > /dev/null
print_status $? "Staging overlay builds successfully"

kubectl kustomize k8s/overlays/production > /dev/null
print_status $? "Production overlay builds successfully"

# Test 3: Validate Helm chart
print_info "Test 3: Linting Helm chart..."
helm lint charts/vod-tender > /dev/null
print_status $? "Helm chart lint passed"

print_info "Test 4: Testing Helm template rendering..."
helm template test charts/vod-tender \
    --set config.twitchChannel=test \
    --set config.twitchBotUsername=testbot \
    > /dev/null
print_status $? "Helm template renders successfully"

print_info "Test 5: Testing Helm template with backup enabled..."
helm template test charts/vod-tender \
    --set config.twitchChannel=test \
    --set config.twitchBotUsername=testbot \
    --set postgres.backup.enabled=true \
    > /dev/null
print_status $? "Helm template with backup renders successfully"

print_info "Test 6: Testing Helm template with Grafana dashboard..."
helm template test charts/vod-tender \
    --set config.twitchChannel=test \
    --set config.twitchBotUsername=testbot \
    --set monitoring.grafana.enabled=true \
    > /dev/null
print_status $? "Helm template with Grafana renders successfully"

print_info "Test 7: Testing Helm template with production values..."
helm template test charts/vod-tender \
    -f charts/vod-tender/values-production.yaml \
    --set config.twitchChannel=test \
    --set config.twitchBotUsername=testbot \
    > /dev/null
print_status $? "Helm template with production values renders successfully"

# Test 8: Deploy to kind cluster using Helm
print_info "Test 8: Deploying to kind cluster using Helm (dry-run)..."
helm install vod-tender-test charts/vod-tender \
    --namespace vod-tender \
    --create-namespace \
    --set config.twitchChannel=test \
    --set config.twitchBotUsername=testbot \
    --set secrets.twitch.clientId=test-id \
    --set secrets.twitch.clientSecret=test-secret \
    --set ingress.enabled=false \
    --set monitoring.serviceMonitor.enabled=false \
    --set postgres.backup.enabled=true \
    --set monitoring.grafana.enabled=true \
    --dry-run > /dev/null
print_status $? "Helm dry-run install successful"

# Test 9: Validate kustomize dry-run
print_info "Test 9: Testing kustomize deployment (dry-run)..."
kubectl apply -k k8s/overlays/dev --dry-run=client > /dev/null
print_status $? "Kustomize dry-run successful"

# Cleanup
print_info "Cleaning up..."
kind delete cluster --name "$CLUSTER_NAME"
print_status $? "Kind cluster deleted"

echo ""
echo -e "${GREEN}=== All validation tests passed! ===${NC}"
echo ""
echo "Summary:"
echo "  ✓ Kustomize base and overlays (dev, staging, production) build successfully"
echo "  ✓ Helm chart lints without errors"
echo "  ✓ Helm templates render correctly with various configurations"
echo "  ✓ Backup CronJob template works"
echo "  ✓ Grafana dashboard ConfigMap template works"
echo "  ✓ Dry-run deployments succeed"
