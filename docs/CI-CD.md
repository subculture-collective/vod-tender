# CI/CD Pipeline Documentation

This document describes the automated CI/CD pipeline for vod-tender, including continuous integration, releases, deployments, and quality gates.

## Overview

The CI/CD pipeline consists of multiple workflows that automate:
- Code quality checks (linting, formatting, security scanning)
- Testing (unit, integration, database migrations)
- Performance monitoring (benchmarks, coverage)
- Releases (semantic versioning, multi-arch builds, container publishing)
- Deployments (staging, production)
- Dependency management (automated updates)

## Workflows

### 1. CI Workflow (`.github/workflows/ci.yml`)

**Trigger:** Push to `main` or pull requests to `main`

**Jobs:**

#### gitleaks
- Scans for secrets in code
- Uses `.gitleaks.toml` for configuration
- Fails on any secret findings

#### govulncheck
- Scans Go dependencies for known vulnerabilities
- Uses Go's official vulnerability database

#### build-test-lint
- Builds Go backend
- Runs unit tests with race detection
- Generates coverage reports (uploaded to Codecov)
- **Coverage threshold:** 50% (enforced) with target of 70%
- Runs `go vet` for static analysis
- Runs `golangci-lint` for comprehensive linting
- Builds Docker image

#### frontend
- Installs npm dependencies
- TypeScript type checking
- Builds frontend with Vite
- Runs ESLint
- Tracks bundle size

#### docker-images
- Builds Docker images for backend and frontend (linux/amd64 only for scanning)
- Runs Trivy vulnerability scanning
- Uploads SARIF reports to GitHub Security
- Generates JSON reports for artifacts

#### multi-arch-builds
- Validates multi-architecture builds (linux/amd64, linux/arm64)
- Uses Docker Buildx
- Caches layers for faster builds
- Does not push images (verification only)

#### database-migration-tests
- Spins up Postgres container
- Runs migration tests
- Verifies schema integrity

#### integration-tests
- Starts full docker-compose stack
- Validates service health checks
- Runs smoke tests against API endpoints
- Tests frontend availability

### 2. Commit Lint Workflow (`.github/workflows/commitlint.yml`)

**Trigger:** Push to `main` or pull requests to `main`

**Purpose:** Enforces conventional commit message format

**Configuration:** `.commitlintrc.js`

**Supported types:**
- `feat`: New feature (minor version bump)
- `fix`: Bug fix (patch version bump)
- `docs`: Documentation changes (patch version bump)
- `style`: Code style changes (no version bump)
- `refactor`: Code refactoring (patch version bump)
- `perf`: Performance improvements (patch version bump)
- `test`: Test additions/changes (no version bump)
- `build`: Build system changes (no version bump)
- `ci`: CI/CD changes (no version bump)
- `chore`: Other changes (no version bump)
- `revert`: Revert changes (patch version bump)

**Breaking changes:** Add `BREAKING CHANGE:` in commit body or footer for major version bump

### 3. Release Workflow (`.github/workflows/release.yml`)

**Trigger:** Push to `main` branch (automatic) or manual tag creation

**Process:**

#### semantic-release job
1. Analyzes commits since last release
2. Determines version bump based on conventional commits
3. Generates changelog
4. Creates Git tag
5. Updates CHANGELOG.md

#### release job (runs only if new release created)
1. Builds multi-arch Docker images (linux/amd64, linux/arm64)
2. Pushes to GitHub Container Registry (ghcr.io)
3. Tags images with version, latest, and commit SHA
4. Signs images with cosign
5. Generates SBOM (Software Bill of Materials)
6. Builds backend binaries for multiple platforms:
   - linux/amd64
   - linux/arm64
   - darwin/amd64 (macOS Intel)
   - darwin/arm64 (macOS Apple Silicon)
7. Generates checksums
8. Creates GitHub Release with:
   - Changelog notes
   - Binary artifacts
   - Checksums
   - SBOM files

**Configuration:** `.releaserc.json`

### 4. Quality Gates Workflow (`.github/workflows/quality-gates.yml`)

**Trigger:** Push to `main` or pull requests to `main`

**Jobs:**

#### benchmarks
- Runs Go benchmarks
- Uploads results as artifacts
- **On PRs:** Compares against baseline from main branch
- **On main:** Saves new baseline
- **Performance regression detection:** Fails if >10% performance degradation (optional)

#### coverage
- Runs tests with coverage
- Calculates coverage percentage
- Reports in GitHub Step Summary
- Target: 70% coverage

#### security-scorecard
- Runs OSSF Scorecard analysis
- Evaluates security best practices
- Uploads SARIF results to GitHub Security
- Checks for:
  - Dependency vulnerabilities
  - Code review practices
  - CI/CD security
  - Token permissions
  - Branch protection

### 5. Dependabot Auto-Merge (`.github/workflows/dependabot-auto-merge.yml`)

**Trigger:** Dependabot pull requests

**Behavior:**
- **Patch updates:** Auto-approves and auto-merges after tests pass
- **Minor updates:** Auto-approves only (manual merge required)
- **Major updates:** Adds warning comment (manual review required)

**Configuration:** `.github/dependabot.yml`

**Update schedule:** Weekly for:
- Go modules
- npm packages
- Docker base images
- GitHub Actions

### 6. Staging Deployment (`.github/workflows/deploy-staging.yml`)

**Trigger:** 
- Push to `main` (automatic)
- Manual trigger via `workflow_dispatch`

**Process:**
1. Deploys latest main branch to staging environment
2. Waits for services to be healthy
3. Runs health checks
4. Runs smoke tests
5. Rolls back on failure

**Status:** Placeholder - requires actual deployment infrastructure

### 7. Production Deployment (`.github/workflows/deploy-production.yml`)

**Trigger:** Manual trigger via `workflow_dispatch` (requires version tag input)

**Strategy:** Blue-green deployment

**Process:**
1. Validates version tag format (vX.Y.Z)
2. Verifies release exists
3. Deploys to green environment
4. Runs health checks on green
5. Runs production smoke tests
6. Switches traffic to green
7. Monitors metrics for 5 minutes
8. Keeps blue environment as backup
9. Rolls back to blue on failure

**Status:** Placeholder - requires actual deployment infrastructure

## Performance Regression Detection

**Script:** `scripts/check-performance-regression.sh`

**Threshold:** 10% performance degradation

**Usage:**
```bash
./scripts/check-performance-regression.sh current_bench.txt baseline_bench.txt [threshold]
```

**How it works:**
1. Compares benchmark results using `benchstat`
2. Identifies benchmarks with >10% performance regression
3. Fails CI if significant regressions detected
4. Currently set to `continue-on-error: true` to not block PRs

## Release Process

### Automatic Release (Recommended)

1. Commit changes to feature branch using conventional commit format:
   ```bash
   git commit -m "feat: add new feature"
   git commit -m "fix: resolve bug in component"
   ```

2. Create pull request and merge to `main`

3. Release workflow automatically:
   - Analyzes commits
   - Bumps version
   - Generates changelog
   - Creates GitHub release
   - Publishes Docker images
   - Uploads binaries

### Manual Release (Emergency)

1. Create and push a version tag:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. Release workflow triggers on tag push

## Container Images

**Registry:** GitHub Container Registry (ghcr.io)

**Images:**
- `ghcr.io/subculture-collective/vod-tender/backend:latest`
- `ghcr.io/subculture-collective/vod-tender/backend:<version>`
- `ghcr.io/subculture-collective/vod-tender/backend:<commit-sha>`
- `ghcr.io/subculture-collective/vod-tender/frontend:latest`
- `ghcr.io/subculture-collective/vod-tender/frontend:<version>`
- `ghcr.io/subculture-collective/vod-tender/frontend:<commit-sha>`

**Platforms:**
- linux/amd64
- linux/arm64

**Security:**
- Images are signed with cosign
- SBOM generated for each image
- Regular vulnerability scanning with Trivy

## Metrics and Monitoring

### Success Metrics

- **CI pipeline execution time:** Target <10 minutes
- **Coverage:** Current 50%, target 70%
- **Performance regression threshold:** <10%
- **Container cache hit rate:** Target >80%

### Artifacts Retention

- Benchmark results: 30 days (baseline: 90 days)
- Coverage reports: 30 days
- Trivy scan reports: 30 days
- OSSF Scorecard results: 30 days

## Troubleshooting

### Coverage Below Threshold

If CI fails due to coverage:
1. Add tests for uncovered code
2. Or update threshold in `.github/workflows/ci.yml` if justified

### Semantic Release Not Creating Release

Check that commits follow conventional commit format:
- Use valid types (feat, fix, etc.)
- Breaking changes need `BREAKING CHANGE:` in commit body

### Dependabot PR Not Auto-Merging

Ensure:
1. All CI checks pass
2. Update is a patch version
3. Repository settings allow auto-merge

### Performance Regression Detected

If benchmarks show regression:
1. Review the changes that caused it
2. Optimize the code
3. Or justify the regression in PR description

## Security Considerations

### Secrets Management

- Never commit secrets to the repository
- Use `.gitleaks.toml` to configure secret scanning
- Store secrets in GitHub Secrets
- Rotate secrets regularly

### Container Security

- Base images are scanned with Trivy
- Images signed with cosign for verification
- SBOM generated for supply chain transparency
- Regular dependency updates via Dependabot

### Branch Protection

Recommended branch protection rules for `main`:
- Require pull request reviews
- Require status checks to pass (CI, commitlint, quality-gates)
- Require signed commits
- Include administrators
- Require linear history

## Contributing

When contributing, ensure:
1. Commits follow conventional commit format
2. Tests pass locally
3. Code coverage doesn't decrease
4. Linters pass (`make lint`)
5. Docker builds succeed locally

## References

- [Conventional Commits](https://www.conventionalcommits.org/)
- [Semantic Release](https://github.com/semantic-release/semantic-release)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Docker Buildx Multi-platform](https://docs.docker.com/build/building/multi-platform/)
- [OSSF Scorecard](https://github.com/ossf/scorecard)
- [Cosign Container Signing](https://github.com/sigstore/cosign)
