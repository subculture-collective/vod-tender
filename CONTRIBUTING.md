# Contributing to vod-tender

Thank you for your interest in contributing to vod-tender! This document provides guidelines and workflows for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing Requirements](#testing-requirements)
- [Code Review Process](#code-review-process)
- [Release Process](#release-process)
- [Getting Help](#getting-help)

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](./CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to <conduct@subculture-collective.com>.

## Getting Started

### Prerequisites

- **Go**: 1.21+ (see `go.mod` for exact version)
- **Node.js**: 18+ and npm/yarn (for frontend)
- **Docker**: 24.0+ and Docker Compose
- **yt-dlp**: Latest version
- **ffmpeg**: 5.0+ (optional but recommended)
- **PostgreSQL**: 14+ (via Docker Compose)

### Initial Setup

1. **Fork the repository** on GitHub

2. **Clone your fork**:

   ```bash
   git clone https://github.com/YOUR_USERNAME/vod-tender.git
   cd vod-tender
   ```

3. **Add upstream remote**:

   ```bash
   git remote add upstream https://github.com/subculture-collective/vod-tender.git
   ```

4. **Install dependencies**:

   ```bash
   # Backend
   cd backend
   go mod download
   
   # Frontend
   cd ../frontend
   npm install
   ```

5. **Copy configuration**:

   ```bash
   cp backend/.env.example backend/.env
   # Edit backend/.env with your Twitch credentials
   ```

6. **Start development environment**:

   ```bash
   make up
   ```

7. **Verify setup**:

   ```bash
   # Check services
   make ps
   
   # Check API health
   curl http://localhost:8080/healthz
   
   # Check frontend
   open http://localhost:3000
   ```

### Development Environment

**Docker Compose Stack**:

- PostgreSQL (port 5432)
- API backend (port 8080)
- Frontend dev server (port 3000)

**Useful Commands**:

```bash
make up              # Start all services
make down            # Stop all services
make logs-backend    # Tail API logs
make logs-frontend   # Tail frontend logs
make db-reset        # Reset database
make lint            # Run linter
make test            # Run tests
```

## Development Workflow

### Creating a Feature Branch

```bash
# Sync with upstream
git fetch upstream
git checkout main
git merge upstream/main

# Create feature branch
git checkout -b feature/your-feature-name

# Or for bug fixes
git checkout -b fix/issue-number-short-description
```

**Branch Naming Convention**:

- `feature/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `refactor/` - Code refactoring
- `test/` - Test additions/improvements
- `chore/` - Maintenance tasks

### Making Changes

1. **Write code** following our [coding standards](#coding-standards)

2. **Test locally**:

   ```bash
   # Run linter
   make lint
   
   # Run tests
   make test
   
   # Build to verify compilation
   cd backend && go build ./...
   ```

3. **Commit changes** using [Conventional Commits](https://www.conventionalcommits.org/):

   ```bash
   git add .
   git commit -m "feat: add VOD priority queue"
   git commit -m "fix: resolve circuit breaker race condition"
   git commit -m "docs: update deployment guide"
   ```

**Commit Message Format**:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style (formatting, missing semicolons, etc.)
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `perf`: Performance improvement
- `test`: Adding or updating tests
- `chore`: Maintenance tasks (dependency updates, build changes)

**Example**:

```
feat(vod): add download priority queue

Implements priority-based VOD processing to handle high-value
streams first. Priority is determined by view count and recency.

Closes #123
```

### Submitting a Pull Request

1. **Push to your fork**:

   ```bash
   git push origin feature/your-feature-name
   ```

2. **Create Pull Request** on GitHub:
   - Use the PR template (automatically populated)
   - Provide clear title and description
   - Link related issues (`Closes #123`, `Fixes #456`)
   - Add screenshots for UI changes
   - Mark as draft if WIP

3. **Respond to feedback**:
   - Address review comments
   - Push additional commits
   - Request re-review when ready

4. **Keep PR updated**:

   ```bash
   # Rebase on latest main
   git fetch upstream
   git rebase upstream/main
   git push --force-with-lease origin feature/your-feature-name
   ```

## Coding Standards

### Go Code Style

Follow [Effective Go](https://golang.org/doc/effective_go.html) and project conventions:

**Formatting**:

```bash
# Format code
gofmt -w .
goimports -w .

# Or use golangci-lint
make lint-fix
```

**Package Structure**:

```
backend/
├── cmd/           # Main applications
├── internal/      # Private application code
│   ├── config/    # Configuration
│   ├── db/        # Database
│   ├── server/    # HTTP server
│   ├── vod/       # VOD processing
│   ├── chat/      # Chat recording
│   ├── oauth/     # OAuth management
│   └── ...
├── pkg/           # Public libraries (none currently)
└── api/           # API specifications (OpenAPI)
```

**Naming Conventions**:

- **Packages**: lowercase, single word (e.g., `vod`, `chat`, `server`)
- **Files**: lowercase with underscores (e.g., `processing.go`, `catalog_test.go`)
- **Types**: PascalCase (e.g., `VOD`, `ChatMessage`, `Downloader`)
- **Functions**: camelCase for private, PascalCase for exported (e.g., `downloadVOD()`, `DownloadVOD()`)
- **Constants**: PascalCase or ALL_CAPS for exported (e.g., `DefaultTimeout`, `MAX_RETRIES`)

**Error Handling**:

```go
// ✅ GOOD: Return errors, don't panic
func downloadVOD(id string) error {
    if id == "" {
        return errors.New("id cannot be empty")
    }
    // ...
    return nil
}

// ✅ GOOD: Wrap errors with context
if err := saveToDatabase(vod); err != nil {
    return fmt.Errorf("failed to save VOD %s: %w", vod.ID, err)
}

// ❌ BAD: Ignoring errors
saveToDatabase(vod) // Missing error check

// ❌ BAD: Panic in library code
if id == "" {
    panic("id cannot be empty")
}
```

**Logging**:

```go
// Use structured logging with slog
slog.Info("download started",
    "component", "vod_download",
    "vod_id", vodID,
    "size_mb", sizeBytes / 1024 / 1024,
)

// Include correlation IDs
logger := slog.With("corr", correlationID)
logger.Error("download failed", "error", err)
```

**Context Handling**:

```go
// Always accept context as first parameter
func processVOD(ctx context.Context, id string) error {
    // Check cancellation
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    
    // Pass context down
    return downloadWithContext(ctx, id)
}
```

**Database Queries**:

```go
// ✅ GOOD: Parameterized queries
rows, err := db.Query(
    "SELECT * FROM vods WHERE id = $1 AND processed = $2",
    vodID,
    false,
)

// ❌ BAD: String concatenation (SQL injection risk)
query := fmt.Sprintf("SELECT * FROM vods WHERE id = '%s'", vodID)
rows, err := db.Query(query)
```

### TypeScript/React Conventions

**Code Style**:

```typescript
// Use TypeScript strict mode
// tsconfig.json: "strict": true

// Prefer functional components with hooks
const VodList: React.FC = () => {
  const [vods, setVods] = useState<VOD[]>([]);
  
  useEffect(() => {
    fetchVods().then(setVods);
  }, []);
  
  return (
    <div>
      {vods.map(vod => (
        <VodCard key={vod.id} vod={vod} />
      ))}
    </div>
  );
};

// Use explicit types
interface VOD {
  id: string;
  title: string;
  duration: number;
  createdAt: Date;
}

// Avoid any types
// ❌ BAD
const data: any = await fetch();

// ✅ GOOD
interface ApiResponse {
  vods: VOD[];
  total: number;
}
const data: ApiResponse = await fetch();
```

**File Naming**:

- Components: `PascalCase.tsx` (e.g., `VodCard.tsx`)
- Utilities: `camelCase.ts` (e.g., `apiClient.ts`)
- Types: `types.ts` or `*.types.ts`

### Documentation

**Code Comments**:

```go
// Document exported functions, types, constants
// Use godoc format

// DownloadVOD downloads a Twitch VOD and saves it locally.
// It returns the local file path or an error.
//
// The download is resumable and uses exponential backoff on failures.
func DownloadVOD(ctx context.Context, vodID string) (string, error) {
    // Implementation...
}

// VOD represents a Twitch VOD with processing metadata.
type VOD struct {
    // TwitchVODID is the unique identifier from Twitch
    TwitchVODID string
    
    // Processed indicates whether the VOD has been fully processed
    Processed bool
}
```

**Markdown Documentation**:

- Use clear headings (H1, H2, H3)
- Include code examples with language tags
- Add tables for structured data
- Link to related documentation

## Testing Requirements

### Minimum Coverage

- **Overall**: 70% code coverage
- **Critical paths**: 90%+ (download, upload, processing)
- **New code**: Must include tests

### Test Structure

**Naming Convention**:

```
file_test.go          # Tests for file.go
file_integration_test.go  # Integration tests
```

**Test Function Names**:

```go
func TestFunctionName(t *testing.T) { }
func TestFunctionName_ErrorCase(t *testing.T) { }
func TestFunctionName_EdgeCase(t *testing.T) { }
```

**Example Test**:

```go
func TestDownloadVOD_Success(t *testing.T) {
    // Arrange
    ctx := context.Background()
    mockDownloader := &MockDownloader{
        DownloadFunc: func(ctx context.Context, vodID string) error {
            return nil
        },
    }
    
    // Act
    err := mockDownloader.Download(ctx, "12345678")
    
    // Assert
    if err != nil {
        t.Errorf("expected no error, got %v", err)
    }
}

func TestDownloadVOD_NetworkError(t *testing.T) {
    // Test error handling
    ctx := context.Background()
    mockDownloader := &MockDownloader{
        DownloadFunc: func(ctx context.Context, vodID string) error {
            return errors.New("network timeout")
        },
    }
    
    err := mockDownloader.Download(ctx, "12345678")
    
    if err == nil {
        t.Error("expected error, got nil")
    }
}
```

### Running Tests

```bash
# Run all tests
make test

# Run with coverage
go test -cover ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test ./backend/internal/vod/...

# Run specific test
go test -run TestDownloadVOD_Success ./...

# Run with race detector
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests

Integration tests require PostgreSQL:

```bash
# Set test database DSN
export TEST_PG_DSN="postgres://vod:vod@localhost:5432/vod_test?sslmode=disable"

# Run integration tests
go test -tags=integration ./...
```

**Integration Test Example**:

```go
// +build integration

func TestProcessingJob_Integration(t *testing.T) {
    // Requires TEST_PG_DSN
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    // Setup database
    db := setupTestDB(t)
    defer db.Close()
    
    // Test with real database
    // ...
}
```

### Mocking

Use interfaces for mockable dependencies:

```go
// Define interface
type Downloader interface {
    Download(ctx context.Context, vodID string) error
}

// Implement mock
type MockDownloader struct {
    DownloadFunc func(ctx context.Context, vodID string) error
}

func (m *MockDownloader) Download(ctx context.Context, vodID string) error {
    return m.DownloadFunc(ctx, vodID)
}

// Use in tests
func TestWithMock(t *testing.T) {
    mock := &MockDownloader{
        DownloadFunc: func(ctx context.Context, vodID string) error {
            return nil
        },
    }
    // Test code using mock
}
```

## Code Review Process

### For Contributors

1. **Self-review**: Review your own code before requesting review
2. **CI must pass**: All checks (lint, test, build) must pass
3. **Keep it small**: PRs should be < 500 lines when possible
4. **Respond promptly**: Address feedback within 48 hours

### For Reviewers

**Review Checklist**:

- [ ] Code follows style guidelines
- [ ] Tests are included and pass
- [ ] Documentation is updated
- [ ] No security vulnerabilities introduced
- [ ] Performance impact considered
- [ ] Backward compatibility maintained
- [ ] Error handling is appropriate

**Review Comments**:

- Be respectful and constructive
- Explain the "why" behind suggestions
- Distinguish between required changes and suggestions
- Use prefixes: `nit:` for minor issues, `question:` for clarification

**Approval Requirements**:

- 1 approval required for merge
- Maintainer approval for breaking changes
- Security review for authentication/authorization changes

### Merge Strategy

**Squash and Merge** (default):

- Squashes commits into a single commit
- Keeps main branch history clean
- Use for most PRs

**Rebase and Merge**:

- Maintains individual commits
- Use for well-structured commit history

**Merge Commit**:

- Creates a merge commit
- Use for merging release branches

## Release Process

Maintained by project maintainers.

### Version Numbering

Follow [Semantic Versioning](https://semver.org/):

- **MAJOR**: Breaking changes (v2.0.0)
- **MINOR**: New features, backward compatible (v1.1.0)
- **PATCH**: Bug fixes, backward compatible (v1.0.1)

### Release Workflow

1. **Create release branch**:

   ```bash
   git checkout -b release/v1.2.0
   ```

2. **Update version**:
   - Update `version` in relevant files
   - Update CHANGELOG.md

3. **Create PR**: `release/v1.2.0` → `main`

4. **Merge and tag**:

   ```bash
   git checkout main
   git pull origin main
   git tag -a v1.2.0 -m "Release v1.2.0"
   git push origin v1.2.0
   ```

5. **Create GitHub Release**:
   - Navigate to Releases
   - Draft new release
   - Select tag v1.2.0
   - Add release notes from CHANGELOG
   - Publish release

6. **Announce**:
   - Post in discussions
   - Update documentation
   - Notify users (if breaking changes)

### Changelog

Maintain CHANGELOG.md following [Keep a Changelog](https://keepachangelog.com/):

```markdown
# Changelog

## [1.2.0] - 2025-10-20

### Added
- Download priority queue for high-value VODs
- YouTube upload rate limiting

### Changed
- Improved circuit breaker logic
- Updated dependencies

### Fixed
- Chat reconciliation race condition
- Memory leak in download progress tracking

### Security
- Patched SQL injection vulnerability in admin endpoints
```

## Getting Help

### Resources

- **Documentation**: `/docs` directory
- **API Reference**: `backend/api/openapi.yaml`
- **Architecture**: `docs/ARCHITECTURE.md`
- **Troubleshooting**: `docs/TROUBLESHOOTING.md`

### Communication Channels

- **GitHub Issues**: Bug reports and feature requests
- **GitHub Discussions**: Questions and community discussion
- **Discord**: Real-time chat (link in README)
- **Email**: <dev@subculture-collective.com>

### Asking Questions

Before asking:

1. Search existing issues and discussions
2. Check documentation
3. Review troubleshooting guide

When asking:

- Provide context and relevant details
- Include error messages and logs
- Describe what you've already tried
- Specify your environment (OS, versions)

**Good Question Template**:

```
## Problem
Brief description of the issue

## Environment
- OS: Ubuntu 22.04
- Go version: 1.21.0
- Deployment: Docker Compose

## Steps to Reproduce
1. Start service with `make up`
2. Navigate to...
3. Click...

## Expected Behavior
What should happen

## Actual Behavior
What actually happens

## Logs/Screenshots
```

[error log or screenshot]

```

## Additional Context
Any other relevant information
```

## Recognition

Contributors are recognized in:

- CONTRIBUTORS.md (alphabetical listing)
- Release notes (for significant contributions)
- GitHub contributor graph

Thank you for contributing to vod-tender! 🎉

---

**Last Updated**: 2025-10-20  
**Questions?** Open a [discussion](https://github.com/subculture-collective/vod-tender/discussions)
