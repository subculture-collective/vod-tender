# CI/CD Pipeline Documentation

This document describes the comprehensive CI/CD pipeline for vod-tender, covering automated testing, releases, and deployments.

## Overview

The CI/CD pipeline is organized into five main workflows:

1. **CI** (`ci.yml`) - Continuous Integration with testing and security scanning
2. **Release** (`release.yml`) - Automated releases with container publishing
3. **Deploy Staging** (`deploy-staging.yml`) - Automatic staging deployments
4. **Deploy Production** (`deploy-production.yml`) - Manual production deployments
5. **Quality Gates** (`quality-gates.yml`) - Performance benchmarks and security scoring

## Workflows

### 1. CI Workflow (`ci.yml`)

**Triggers:** Push to `main`, Pull Requests to `main`

**Jobs:**

#### Backend Jobs

- **gitleaks**: Secret scanning with gitleaks
- **govulncheck**: Go vulnerability scanning
- **build-test-lint**: Go build, test, vet, and lint with golangci-lint

#### Frontend Jobs

- **frontend**:
  - Install dependencies
  - TypeScript type checking (`tsc --noEmit`)
  - Build with Vite
  - Lint with ESLint
  - Bundle size tracking

#### Database Jobs

- **database-migration-tests**:
  - Spin up Postgres 16 service container
  - Run migration tests with `TEST_PG_DSN` environment variable
  - Verify schema integrity

#### Integration Jobs

- **docker-images**:
  - Build backend and frontend Docker images with Buildx
  - Use GitHub Actions cache for layer caching
  - Run Trivy vulnerability scanner (SARIF + JSON reports)
  - Upload security findings to GitHub Security tab
  
- **integration-tests**:
  - Create docker network and environment files
  - Start full docker-compose stack
  - Wait for all services to be healthy
  - Run smoke tests against API endpoints
  - Test frontend availability
  - Cleanup resources

**Features:**

- Parallel job execution for speed
- Comprehensive security scanning
- Docker layer caching
- Health check validation
- Automatic cleanup

### 2. Release Workflow (`release.yml`)

**Triggers:** 
- Push to `main` branch (triggers semantic-release)
- Push of version tags matching `v*` pattern (manual releases)

**Jobs:**

1. **semantic-release** (runs on push to main):
   - Analyzes commits using conventional commits
   - Determines version bump (major/minor/patch)
   - Creates git tag automatically
   - Updates CHANGELOG.md
   - Triggers release job if new version published

2. **release** (runs on semantic-release OR manual tag):
   - Extracts version from semantic-release output or git tag
   - Builds multi-arch container images (linux/amd64, linux/arm64)
   - Pushes to GitHub Container Registry (ghcr.io)
   - Signs images with cosign (keyless)
   - Generates SBOM files using Syft (SPDX JSON format)
   - Builds cross-platform binaries:
     - linux/amd64, linux/arm64
     - darwin/amd64, darwin/arm64
   - Generates changelog from CHANGELOG.md
   - Creates/updates GitHub Release with all artifacts
   - Uploads SBOM files as workflow artifacts (90-day retention)

**Container Images:**

- `ghcr.io/<owner>/<repo>/backend:$VERSION`
- `ghcr.io/<owner>/<repo>/backend:latest`
- `ghcr.io/<owner>/<repo>/backend:$SHA`
- `ghcr.io/<owner>/<repo>/frontend:$VERSION`
- `ghcr.io/<owner>/<repo>/frontend:latest`
- `ghcr.io/<owner>/<repo>/frontend:$SHA`

**Artifacts:**

- Backend binaries (4 platforms)
- SHA256 checksums
- SBOM files (SPDX JSON format):
  - `sbom-backend.spdx.json` - Backend container SBOM
  - `sbom-frontend.spdx.json` - Frontend container SBOM
  - Generated using Syft from Anchore
  - Attached to GitHub Release
  - Available as workflow artifacts

**Image Signing:**
Uses Sigstore cosign for keyless signing. Signatures can be verified with:

```bash
cosign verify \
  --certificate-identity-regexp="https://github.com/<owner>/<repo>" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/<repo>/backend:$VERSION
```

### 3. Deploy Staging Workflow (`deploy-staging.yml`)

**Triggers:**

- Push to `main` branch
- Manual trigger (`workflow_dispatch`)

**Concurrency:** Single deployment at a time (no cancellation)

**Steps:**

1. Checkout code
2. Deploy to staging environment
3. Wait for services to stabilize
4. Run health checks
5. Run smoke tests
6. Report deployment status
7. Rollback on failure

**Environment:** `staging`

**Customization Points:**
The workflow includes placeholder commands that should be replaced with actual deployment logic:

- SSH to staging server
- Pull images from ghcr.io
- Run `docker compose pull && docker compose up -d`
- Or: `kubectl apply` for Kubernetes

### 4. Deploy Production Workflow (`deploy-production.yml`)

**Triggers:** Manual only (`workflow_dispatch`)

**Input Parameters:**

- `version`: Version tag to deploy (e.g., `v1.0.0`)

**Concurrency:** Single deployment at a time (no cancellation)

**Strategy:** Blue-Green Deployment

1. Validate version tag format
2. Verify release exists
3. Deploy to green environment
4. Health check green environment
5. Run production smoke tests
6. Switch traffic to green
7. Monitor metrics for 5 minutes
8. Rollback to blue on failure

**Environment:** `production`

**Features:**

- Zero-downtime deployments
- Quick rollback capability
- Pre-deployment validation
- Post-deployment monitoring

### 5. Quality Gates Workflow (`quality-gates.yml`)

**Triggers:** Pull Requests, Push to `main`

**Jobs:**

#### benchmarks

- Run Go benchmarks
- Upload results as artifacts
- Compare against baseline (on PRs)

#### coverage

- Run tests with coverage profiling
- Calculate coverage percentage
- Report in PR summary
- Upload coverage reports
- Warn if below 70% target

#### security-scorecard

- Run OSSF Scorecard analysis
- Upload results to GitHub Security
- Publish security score

**Quality Metrics:**

- Test coverage target: 70%
- Performance regression threshold: 10%
- Security scorecard: Published for transparency

## Dependabot Configuration

**File:** `.github/dependabot.yml`

**Update Schedule:** Weekly

**Ecosystems Monitored:**

- Go modules (`/backend`)
- npm packages (`/frontend`)
- Docker images (`/backend`, `/frontend`)
- GitHub Actions (`/`)

**Features:**

- Automatic security updates
- Dependency version bumps
- Labeled PRs for easy triage

## Usage Guide

### Making a Release

The project supports two release workflows:

#### Option 1: Automatic Releases (Recommended)

Releases are automatically created when code is merged to `main` using semantic-release:

1. **Use conventional commits** in your PRs:
   - `feat:` → minor version bump (new features)
   - `fix:` → patch version bump (bug fixes)
   - `BREAKING CHANGE:` in commit body → major version bump
   - Other types (`docs:`, `chore:`, etc.) → no release

2. **Merge PR to main** - semantic-release will:
   - Analyze commits to determine version bump
   - Create a git tag automatically
   - Generate changelog from commits
   - Trigger the release workflow

3. **Monitor the release workflow** in Actions tab

#### Option 2: Manual Releases

For manual releases or re-running a release workflow:

1. **Ensure all tests pass on main branch**
2. **Create and push a version tag:**

   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

3. **Monitor the release workflow** in Actions tab

#### Release Artifacts

Both workflows produce the same artifacts:

- **Container images** (ghcr.io):
  - `backend:$VERSION` (and `:latest`, `:$SHA`)
  - `frontend:$VERSION` (and `:latest`, `:$SHA`)
  - Multi-arch: linux/amd64, linux/arm64
  - Signed with cosign (keyless)
- **SBOM files** (SPDX JSON format):
  - `sbom-backend.spdx.json`
  - `sbom-frontend.spdx.json`
  - Generated with Syft
- **Backend binaries** (4 platforms):
  - linux/amd64, linux/arm64
  - darwin/amd64, darwin/arm64
- **Checksums** (`checksums.txt`)
- **Release notes** (generated from CHANGELOG.md)

### Deploying to Staging

Staging deployments happen automatically when code is pushed to `main`.

**Manual staging deployment:**

1. Go to Actions → Deploy Staging
2. Click "Run workflow"
3. Select branch (usually `main`)
4. Monitor deployment progress

### Deploying to Production

Production deployments are **manual only** and require a version tag.

1. **Ensure the version is released**
   - Release workflow must have completed successfully
   - Verify images exist in ghcr.io

2. **Trigger deployment:**
   - Go to Actions → Deploy Production
   - Click "Run workflow"
   - Enter version tag (e.g., `v1.0.0`)
   - Click "Run workflow"

3. **Monitor deployment:**
   - Watch blue-green deployment progress
   - Verify health checks pass
   - Confirm traffic switch

4. **Rollback if needed:**
   - Workflow automatically rolls back on failure
   - Or manually trigger with previous version

### Monitoring Quality Gates

Quality gates run automatically on all PRs and commits to main.

**Check results:**

- Go to Actions → Quality Gates
- View benchmark comparisons
- Check coverage reports
- Review OSSF Scorecard

**Coverage enforcement:**

- Target is 70% overall coverage
- Warnings appear if coverage drops
- Reports available in artifacts

### Security Scanning

Security scanning happens at multiple levels:

1. **Gitleaks** - Secret scanning on every commit
2. **govulncheck** - Go vulnerability database
3. **Trivy** - Container image scanning
4. **OSSF Scorecard** - Supply chain security

**View results:**

- Go to Security tab → Code scanning alerts
- Check workflow artifacts for detailed reports
- SARIF reports auto-uploaded to GitHub Security

## Customization

### Adding Deployment Infrastructure

The deployment workflows (`deploy-staging.yml` and `deploy-production.yml`) contain placeholder commands. Replace them with your actual deployment logic:

**For Docker Compose deployments:**

```yaml
- name: Deploy to staging
  run: |
    ssh user@staging-host << 'EOF'
      cd /opt/vod-tender
      docker compose pull
      docker compose up -d --no-deps api frontend
    EOF
```

**For Kubernetes deployments:**

```yaml
- name: Deploy to staging
  run: |
    kubectl config use-context staging
    kubectl set image deployment/api api=ghcr.io/${{ github.repository }}/backend:${{ github.sha }}
    kubectl set image deployment/frontend frontend=ghcr.io/${{ github.repository }}/frontend:${{ github.sha }}
    kubectl rollout status deployment/api
    kubectl rollout status deployment/frontend
```

### Adding Environment Secrets

Configure secrets in GitHub repository settings:

**For staging/production:**

- `STAGING_SSH_KEY` - SSH key for staging server
- `PROD_SSH_KEY` - SSH key for production server
- `KUBECONFIG` - Kubernetes config (if using k8s)

**For deployments:**

- Configure environment protection rules
- Add required reviewers for production
- Set deployment branch restrictions

### Customizing Quality Gates

**Adjust coverage threshold:**

```yaml
# In quality-gates.yml
if (( $(echo "${{ steps.coverage.outputs.percentage }} < 80" | bc -l) )); then
  # Change 80 to your desired threshold
```

**Add custom benchmarks:**

```bash
# In your Go code
func BenchmarkMyFeature(b *testing.B) {
    for i := 0; i < b.N; i++ {
        // Your code
    }
}
```

## Troubleshooting

### Integration Tests Failing

**Issue:** Services not becoming healthy

**Solution:**

```bash
# Check logs locally
docker compose logs api
docker compose logs postgres

# Verify health checks
docker compose ps
```

**Issue:** Network already exists

**Solution:** Network is created with `|| true` to ignore if exists

### Docker Build Failing

**Issue:** Out of disk space

**Solution:** GitHub Actions runners have limited space. Consider:

- Cleaning up unused images in workflow
- Using `docker system prune` before builds
- Reducing layer count in Dockerfiles

### Release Workflow Failing

**Issue:** Tag push doesn't trigger workflow

**Solution:**

- Verify tag format matches `v*` pattern
- Check workflow permissions
- Ensure GITHUB_TOKEN has packages write permission

### Deployment Failures

**Issue:** Unable to connect to deployment target

**Solution:**

- Verify SSH keys are configured
- Check environment secrets
- Confirm network connectivity

## Best Practices

1. **Always test locally first**

   ```bash
   # Run tests
   cd backend && go test ./...
   cd frontend && npm test
   
   # Build Docker images
   docker build -t test-backend ./backend
   docker build -t test-frontend ./frontend
   
   # Test integration
   docker compose up -d
   ```

2. **Use semantic versioning**
   - v1.0.0 - Major release
   - v1.1.0 - Minor release (new features)
   - v1.1.1 - Patch release (bug fixes)

3. **Write meaningful commit messages**
   - Helps with auto-generated changelogs
   - Follow conventional commits format

4. **Monitor deployments**
   - Check logs after deployment
   - Verify metrics
   - Monitor error rates

5. **Keep dependencies updated**
   - Review Dependabot PRs weekly
   - Test dependency updates before merging

## Performance Metrics

Target performance for CI/CD pipeline:

- **CI pipeline execution:** <10 minutes
- **Release creation:** <15 minutes
- **Staging deployment:** <5 minutes
- **Production deployment:** <10 minutes
- **Container build cache hit rate:** >80%

## Security Considerations

1. **Image signing:** All release images are signed with cosign
2. **SBOM generation:** Software Bill of Materials for transparency
3. **Vulnerability scanning:** Automated with Trivy (blocks on CRITICAL/HIGH)
4. **Secret scanning:** Gitleaks prevents credential leaks
5. **Supply chain security:** OSSF Scorecard monitors best practices

## References

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Docker Buildx Multi-platform Builds](https://docs.docker.com/build/building/multi-platform/)
- [Cosign Image Signing](https://docs.sigstore.dev/cosign/overview/)
- [OSSF Scorecard](https://github.com/ossf/scorecard)
- [Dependabot Configuration](https://docs.github.com/en/code-security/dependabot)
