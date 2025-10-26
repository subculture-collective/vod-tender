# CI/CD Quick Reference

Quick commands and procedures for common CI/CD operations.

## Release Checklist

- [ ] All tests passing on `main`
- [ ] Documentation updated
- [ ] CHANGELOG reviewed
- [ ] Version number decided

### Create a Release

```bash
# 1. Update version (if not automated)
git checkout main
git pull

# 2. Create and push tag
git tag v1.2.3
git push origin v1.2.3

# 3. Monitor release workflow
# Go to: https://github.com/<owner>/<repo>/actions/workflows/release.yml

# 4. Verify release
# Go to: https://github.com/<owner>/<repo>/releases
```

### Verify Container Images

```bash
# Pull released images
docker pull ghcr.io/<owner>/<repo>/backend:v1.2.3
docker pull ghcr.io/<owner>/<repo>/frontend:v1.2.3

# Verify signatures
cosign verify \
  --certificate-identity-regexp="https://github.com/<owner>/<repo>" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/<repo>/backend:v1.2.3

# Inspect SBOM
curl -L https://github.com/<owner>/<repo>/releases/download/v1.2.3/sbom-backend.spdx.json
```

## Deployment Procedures

### Deploy to Staging

**Automatic:** Pushes to `main` auto-deploy to staging

**Manual:**
```
1. Go to: Actions → Deploy Staging
2. Click "Run workflow"
3. Select branch: main
4. Click "Run workflow" button
5. Monitor deployment in Actions tab
```

### Deploy to Production

**Manual only:**
```
1. Go to: Actions → Deploy Production
2. Click "Run workflow"
3. Enter version: v1.2.3
4. Click "Run workflow" button
5. Monitor blue-green deployment
6. Verify health checks pass
```

### Rollback Production

```
Option 1: Re-deploy previous version
1. Go to: Actions → Deploy Production
2. Enter previous version tag (e.g., v1.2.2)
3. Click "Run workflow"

Option 2: Manual rollback (if workflow configured)
- Workflow automatically rolls back on failure
- Blue environment kept as backup
```

## Local Development

### Run Tests Locally

```bash
# Backend tests
cd backend
go test ./... -v

# Backend with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Frontend tests
cd frontend
npm test

# Linting
cd backend && golangci-lint run
cd frontend && npm run lint
```

### Build Docker Images Locally

```bash
# Backend
docker build -t vod-tender-backend:local ./backend

# Frontend
docker build -t vod-tender-frontend:local ./frontend

# Both with docker compose
docker compose build
```

### Run Integration Tests Locally

```bash
# Create network
docker network create web || true

# Copy environment
cp .env.example .env

# Create backend env
cat > backend/.env << EOF
LOG_LEVEL=info
HTTP_ADDR=:8080
EOF

# Start stack
docker compose up -d

# Check health
docker compose ps

# Test endpoints
curl http://localhost:8080/healthz
curl http://localhost:8080/status
curl http://localhost:8080/metrics

# View logs
docker compose logs -f api

# Clean up
docker compose down -v
```

## Monitoring CI/CD

### Check Workflow Status

```bash
# Using GitHub CLI
gh workflow list
gh run list --workflow=ci.yml
gh run view <run-id>

# View logs
gh run view <run-id> --log
```

### View Security Alerts

```
Web UI:
1. Go to Security tab
2. Code scanning alerts
3. Review Trivy and OSSF Scorecard results
```

### Check Coverage Reports

```
Web UI:
1. Go to Actions
2. Select quality-gates workflow
3. Download coverage-report artifact
4. Open coverage.txt or coverage.out
```

## Troubleshooting

### CI Failures

**Tests failing:**
```bash
# Run locally
cd backend && go test ./... -v

# Check for race conditions
cd backend && go test ./... -race

# Frontend
cd frontend && npm test -- --verbose
```

**Docker build failing:**
```bash
# Build locally with verbose output
docker build --progress=plain -t test ./backend

# Check for layer caching
docker build --no-cache -t test ./backend
```

**Integration tests failing:**
```bash
# Check docker-compose locally
docker compose up -d
docker compose logs api
docker compose exec api curl http://localhost:8080/healthz

# Verify network
docker network inspect web
```

### Release Failures

**Tag already exists:**
```bash
# Delete local tag
git tag -d v1.2.3

# Delete remote tag
git push origin :refs/tags/v1.2.3

# Re-create tag
git tag v1.2.3
git push origin v1.2.3
```

**Image push failed:**
- Check GitHub token permissions
- Verify package write permission enabled
- Check repository settings → Packages

### Deployment Failures

**Cannot connect to server:**
- Verify SSH keys in GitHub secrets
- Check firewall rules
- Test SSH connection manually

**Health checks failing:**
```bash
# SSH to server
ssh user@server

# Check docker status
docker compose ps
docker compose logs api

# Check health manually
curl http://localhost:8080/healthz
```

## Dependabot

### Review Dependency Updates

```
Web UI:
1. Go to Pull Requests
2. Filter by label: dependencies
3. Review changes in PR
4. Check CI passes
5. Merge if safe
```

### Configure Auto-merge

```yaml
# Add to .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/backend"
    schedule:
      interval: "weekly"
    # Enable auto-merge for patch updates
    open-pull-requests-limit: 10
    auto-merge:
      - dependency-type: "all"
        update-type: "semver:patch"
```

## Performance Optimization

### Speed Up CI

**Use caching:**
```yaml
# Already configured in workflows
- uses: actions/setup-go@v5
  with:
    cache: true
    
- uses: docker/build-push-action@v5
  with:
    cache-from: type=gha
    cache-to: type=gha,mode=max
```

**Parallelize jobs:**
- Jobs without dependencies run in parallel
- Use `needs:` to control dependencies

### Reduce Docker Build Time

```dockerfile
# Order layers from least to most frequently changing
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build
```

## Security Best Practices

### Scan for Secrets

```bash
# Install gitleaks
brew install gitleaks  # macOS
# or download from https://github.com/gitleaks/gitleaks

# Scan repository
gitleaks detect --source . --verbose

# Scan commits
gitleaks detect --source . --log-opts="--since=2024-01-01"
```

### Verify Image Integrity

```bash
# Check image digest
docker pull ghcr.io/<owner>/<repo>/backend:v1.2.3
docker inspect ghcr.io/<owner>/<repo>/backend:v1.2.3 | jq '.[0].RepoDigests'

# Verify signature
cosign verify \
  --certificate-identity-regexp="https://github.com/<owner>/<repo>" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/<repo>/backend:v1.2.3
```

### Review Security Scorecard

```
Web UI:
1. Go to Security tab
2. View scorecard results
3. Address low-scoring categories
```

## Useful Links

- **Actions:** `https://github.com/<owner>/<repo>/actions`
- **Releases:** `https://github.com/<owner>/<repo>/releases`
- **Packages:** `https://github.com/<owner>/<repo>/pkgs/container/<repo>`
- **Security:** `https://github.com/<owner>/<repo>/security`

## Emergency Procedures

### Stop All Deployments

```
1. Go to Actions tab
2. Find running workflow
3. Click "Cancel workflow"
```

### Emergency Rollback

```bash
# Option 1: Via GitHub Actions
1. Go to Deploy Production workflow
2. Run with previous stable version

# Option 2: Direct server access
ssh user@production
cd /opt/vod-tender
docker compose down
docker compose pull
docker tag ghcr.io/<owner>/<repo>/backend:v1.2.2 vod-backend:current
docker tag ghcr.io/<owner>/<repo>/frontend:v1.2.2 vod-frontend:current
docker compose up -d
```

### Disable Automated Deployments

```yaml
# Temporarily disable in .github/workflows/deploy-staging.yml
on:
  # push:
  #   branches:
  #     - main
  workflow_dispatch:  # Keep manual trigger only
```

## Getting Help

- Check workflow logs in Actions tab
- Review documentation in `docs/CICD.md`
- Check GitHub Actions status: https://www.githubstatus.com/
- File an issue if workflow problems persist
