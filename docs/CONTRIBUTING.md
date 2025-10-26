# Contributing Guide

## Quick Start

1. Fork and clone the repository
2. Create a feature branch
3. Make your changes
4. Run tests and linters
5. Commit using conventional commit format
6. Push and create a pull request

## Commit Message Format

We use [Conventional Commits](https://www.conventionalcommits.org/) for automatic versioning and changelog generation.

### Format

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types

- **feat**: New feature (triggers minor version bump)
- **fix**: Bug fix (triggers patch version bump)
- **docs**: Documentation changes
- **style**: Code style changes (formatting, etc.)
- **refactor**: Code refactoring without changing functionality
- **perf**: Performance improvements
- **test**: Adding or updating tests
- **build**: Build system changes
- **ci**: CI/CD changes
- **chore**: Other changes (dependencies, etc.)

### Examples

```bash
# Feature (minor version bump: 1.0.0 -> 1.1.0)
feat(api): add VOD filtering by date range

# Bug fix (patch version bump: 1.0.0 -> 1.0.1)
fix(chat): prevent duplicate messages in replay

# Documentation
docs(readme): update installation instructions

# Breaking change (major version bump: 1.0.0 -> 2.0.0)
feat(api): redesign VOD listing endpoint

BREAKING CHANGE: VOD listing now returns paginated results.
Previous `/vods` endpoint is deprecated, use `/api/vods?page=1` instead.
```

### Scope (Optional)

Common scopes:

- `api` - Backend API changes
- `frontend` - Frontend changes
- `chat` - Chat recorder/replay
- `vod` - VOD processing
- `db` - Database changes
- `docker` - Docker configuration
- `ci` - CI/CD workflows
- `deps` - Dependency updates

## Development Workflow

### 1. Setup Development Environment

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/vod-tender.git
cd vod-tender

# Add upstream remote
git remote add upstream https://github.com/subculture-collective/vod-tender.git

# Copy environment file
cp backend/.env.example backend/.env
# Edit backend/.env with your credentials
```

### 2. Create Feature Branch

```bash
# Update your fork
git fetch upstream
git checkout main
git merge upstream/main

# Create feature branch
git checkout -b feat/my-feature
```

### 3. Make Changes

Edit code, add tests, update documentation.

### 4. Run Tests and Linters

```bash
# Backend tests
cd backend
go test ./...
go test -race ./...

# Backend linting
make lint

# Frontend tests (if applicable)
cd frontend
npm run build
npm run lint

# Docker build test
docker build -t vod-tender-backend ./backend
docker build -t vod-tender-frontend ./frontend
```

### 5. Commit Changes

```bash
# Stage changes
git add .

# Commit with conventional format
git commit -m "feat(api): add new endpoint for VOD stats"

# Or for multiple files
git commit -m "fix: resolve multiple issues

- fix(chat): prevent race condition in recorder
- fix(vod): handle missing metadata gracefully
- test: add integration tests for error cases"
```

### 6. Push and Create PR

```bash
# Push to your fork
git push origin feat/my-feature

# Create pull request on GitHub
# - Use descriptive title (can use same format as commit message)
# - Fill out PR template
# - Link related issues
```

## Pull Request Guidelines

### PR Title

Use conventional commit format for PR titles:

```
feat: add VOD filtering by date
fix: resolve chat replay timing issues
docs: update API documentation
```

### PR Description

Include:

- What changes were made
- Why the changes were made
- How to test the changes
- Screenshots (if UI changes)
- Breaking changes (if any)
- Related issues (use `Fixes #123` or `Closes #456`)

### PR Checklist

- [ ] Commits follow conventional commit format
- [ ] Tests pass locally
- [ ] Linters pass (`make lint`)
- [ ] Code coverage doesn't decrease (if applicable)
- [ ] Documentation updated (if needed)
- [ ] CHANGELOG.md will be updated automatically by semantic-release

## Code Review Process

1. **Automated checks run:**
   - Commit message linting
   - Build and test
   - Code coverage
   - Security scanning
   - Docker image builds
   - Integration tests

2. **Human review:**
   - Code quality
   - Design decisions
   - Test coverage
   - Documentation

3. **Approval required:**
   - At least one maintainer approval
   - All CI checks must pass

4. **Merge:**
   - Squash merge to main (preferred)
   - Merge commit message follows conventional format
   - Automatic release triggered on merge

## Testing

### Unit Tests

```bash
cd backend
go test ./... -v
```

### Integration Tests

```bash
# Start docker-compose stack
make up

# Run integration tests
make test-integration
```

### Coverage

```bash
cd backend
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out  # View in browser
```

### Benchmarks

```bash
cd backend
go test -bench=. -benchmem ./...
```

## Code Style

### Go

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting (done automatically by golangci-lint)
- Pass all `golangci-lint` checks
- Add comments for exported functions
- Keep functions small and focused

### TypeScript/JavaScript

- Follow ESLint configuration
- Use TypeScript for type safety
- Run `npm run lint` before committing
- Use functional components in React
- Keep components small and reusable

### Documentation

- Update relevant docs when changing behavior
- Add inline comments for complex logic
- Keep README.md up to date
- Add examples for new features

## Common Tasks

### Updating Dependencies

Dependencies are automatically updated weekly by Dependabot.

For manual updates:

```bash
# Go dependencies
cd backend
go get -u ./...
go mod tidy

# npm dependencies
cd frontend
npm update
```

### Adding New Workflows

1. Create workflow file in `.github/workflows/`
2. Test locally with `act` (if possible)
3. Document in `docs/CI-CD.md`
4. Commit with `ci: add new workflow for X`

### Fixing Failed CI

Check workflow logs:

1. Go to Actions tab on GitHub
2. Click on failed workflow
3. Review logs for failed jobs
4. Fix issues locally
5. Push fix

Common failures:

- **Commitlint:** Fix commit message format
- **Tests:** Fix failing tests
- **Coverage:** Add more tests
- **Linting:** Run `make lint-fix`
- **Docker build:** Check Dockerfile syntax

### Release Process

Releases are automatic when commits are merged to `main`:

1. **feat commits:** Trigger minor version bump (1.0.0 -> 1.1.0)
2. **fix commits:** Trigger patch version bump (1.0.0 -> 1.0.1)
3. **BREAKING CHANGE:** Triggers major version bump (1.0.0 -> 2.0.0)

No manual version bumping needed!

## Getting Help

- **Issues:** Check existing issues or create new one
- **Discussions:** Use GitHub Discussions for questions
- **Documentation:** See `/docs` directory
- **Code examples:** Look at existing code

## License

By contributing, you agree that your contributions will be licensed under the project's license.
