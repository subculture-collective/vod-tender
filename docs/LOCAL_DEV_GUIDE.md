# Local Development Guide

Complete end-to-end guide for setting up a local vod-tender development environment from scratch. This guide will take you from zero to a fully functional development environment with sample data.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start (5 minutes)](#quick-start-5-minutes)
- [Detailed Setup](#detailed-setup)
- [Working with Sample Data](#working-with-sample-data)
- [Development Workflow](#development-workflow)
- [Common Tasks](#common-tasks)
- [Troubleshooting](#troubleshooting)
- [Next Steps](#next-steps)

## Prerequisites

### Required Software

- **Docker Desktop** (24.0+) and Docker Compose
  - [Download for macOS](https://docs.docker.com/desktop/install/mac-install/)
  - [Download for Windows](https://docs.docker.com/desktop/install/windows-install/)
  - [Download for Linux](https://docs.docker.com/desktop/install/linux-install/)

- **Git** (2.x+)
  - macOS: Included with Xcode Command Line Tools (`xcode-select --install`)
  - Windows: [Git for Windows](https://git-scm.com/download/win)
  - Linux: `sudo apt-get install git` or equivalent

### Optional but Recommended

- **jq** - For formatting JSON output from API endpoints
  - macOS: `brew install jq`
  - Linux: `sudo apt-get install jq`
  - Windows: [Download from GitHub](https://stedolan.github.io/jq/download/)

- **curl** - For testing API endpoints (usually pre-installed)

- **Make** - For convenience commands (usually pre-installed on macOS/Linux)
  - Windows: Install via [Chocolatey](https://chocolatey.org/) or use Git Bash

### For Advanced Development

If you plan to develop backend or frontend code directly:

- **Go** 1.21+ - [Download](https://golang.org/dl/)
- **Node.js** 18+ and npm - [Download](https://nodejs.org/)
- **golangci-lint** - [Installation instructions](https://golangci-lint.run/welcome/install/)

## Quick Start (5 minutes)

Get a fully functional development environment with sample data in just a few commands:

```bash
# 1. Clone the repository
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender

# 2. Copy environment configuration
cp backend/.env.example backend/.env
# Note: Default settings work for local development with sample data

# 3. Start services and load sample data
make dev-setup

# 4. Verify everything is working
curl http://localhost:8080/healthz
# Should return: OK

# 5. View sample VODs
curl http://localhost:8080/vods | jq
```

**That's it!** You now have:
- PostgreSQL database with sample data
- Backend API running on port 8080
- Frontend running on port 3000
- Jaeger tracing UI on port 16686

Open http://localhost:3000 in your browser to see the frontend.

### What Sample Data is Included?

The `make dev-setup` command loads:
- **7 sample VODs** with various states (completed, in-progress, pending, failed)
- **15+ chat messages** for testing chat replay functionality
- **Circuit breaker configuration** (closed state, no failures)
- **Performance metrics** (average download/upload times)

All sample data uses IDs prefixed with `seed-` to distinguish it from real data.

## Detailed Setup

### Step 1: Clone and Initial Setup

```bash
# Clone the repository
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender

# Add upstream remote (if you forked the repo)
git remote add upstream https://github.com/subculture-collective/vod-tender.git
```

### Step 2: Environment Configuration

The backend requires environment variables for configuration. For local development with sample data, the defaults work fine.

```bash
# Copy the example environment file
cp backend/.env.example backend/.env
```

**For Development with Sample Data Only:**

The default `.env.example` file is configured to work out of the box. You don't need to change anything.

**For Development with Real Twitch Integration:**

If you want to test real Twitch VOD discovery and chat recording, you'll need to add real credentials:

1. Create a Twitch application at https://dev.twitch.tv/console
2. Edit `backend/.env` and fill in:
   ```bash
   TWITCH_CHANNEL=your_channel_name
   TWITCH_BOT_USERNAME=your_bot_username
   TWITCH_CLIENT_ID=your_twitch_app_client_id
   TWITCH_CLIENT_SECRET=your_twitch_app_client_secret
   ```

For more details on all configuration options, see [docs/CONFIG.md](./CONFIG.md).

### Step 3: Start Docker Services

```bash
# Start all services (Postgres, API, Frontend, Jaeger)
make up

# Wait for services to start (usually takes 10-30 seconds)
# You can monitor the logs with:
make logs
```

**What's Starting:**

- **PostgreSQL** - Database on port 5432 (internal) / 5469 (host)
- **API Backend** - Go service on port 8080
- **Frontend** - React + Vite dev server on port 3000
- **Jaeger** - Distributed tracing UI on port 16686
- **Backup Service** - Daily database backups

### Step 4: Load Sample Data

```bash
# Load development seed data
make db-seed
```

This command loads realistic sample data into your database so you can immediately:
- Browse VODs in the frontend
- Test chat replay functionality
- See various VOD states (downloading, completed, failed)
- Test API endpoints with real-looking data

**Sample Data Included:**

| VOD ID | Title | State | Notes |
|--------|-------|-------|-------|
| `seed-completed-001` | Epic Gameplay Session | Completed | Has 15+ chat messages for testing chat replay |
| `seed-downloading-002` | Late Night Stream | Downloading | Shows 62.5% progress (750 MB / 1.2 GB) |
| `seed-pending-003` | Tournament Practice | Pending | High priority (10), ready to process |
| `seed-failed-004` | Speedrun Attempts | Failed | Has error message after 3 retries |
| `seed-priority-005` | Special Event | Pending | Very high priority (100) |
| `seed-completed-006` | Chill Stream | Completed | No YouTube upload |
| `seed-archive-007` | Classic Stream Archive | Completed | 90 days old, uploaded to YouTube |

### Step 5: Verify Installation

```bash
# Check service health
curl http://localhost:8080/healthz
# Expected: OK

# Check readiness with detailed checks
curl http://localhost:8080/readyz | jq
# Expected: JSON with all checks passing

# View system status
curl http://localhost:8080/status | jq
# Expected: Queue stats, circuit breaker state, metrics

# List VODs
curl http://localhost:8080/vods | jq
# Expected: Array of 7 sample VODs

# Get a specific VOD with chat
curl http://localhost:8080/vods/seed-completed-001 | jq
```

### Step 6: Access the Frontend

Open your browser and navigate to:
- **Frontend UI**: http://localhost:3000
- **API Endpoints**: http://localhost:8080
- **Jaeger Tracing**: http://localhost:16686

In the frontend, you should see:
- List of 7 sample VODs
- Various states and progress indicators
- Ability to view chat replay for completed VODs

## Working with Sample Data

### Understanding the Sample Data

All sample data uses the prefix `seed-` in IDs to distinguish it from real data. This allows you to:
- Mix sample and real data
- Reset sample data without affecting real data
- Easily identify test data in logs and UI

### Reloading Sample Data

```bash
# Clear and reload sample data
make db-seed

# The script automatically:
# 1. Deletes existing seed-* records
# 2. Inserts fresh sample data
# 3. Resets circuit breaker and stats
```

### Adding Custom Sample Data

You can modify `backend/db/seed-dev-data.sql` to add your own test scenarios:

```sql
-- Add your custom VOD
INSERT INTO vods (
    twitch_vod_id, 
    title, 
    date, 
    duration_seconds,
    processed,
    priority,
    channel
) VALUES (
    'seed-custom-001',
    'My Custom Test VOD',
    NOW() - INTERVAL '1 day',
    3600,  -- 1 hour
    false,
    50,    -- Medium-high priority
    ''
);
```

Then reload with `make db-seed`.

### Resetting the Database

If you want to start completely fresh:

```bash
# Drop and recreate the database (removes ALL data)
make db-reset

# Restart services to run migrations
make restart

# Load sample data
make db-seed
```

## Development Workflow

### Daily Workflow

```bash
# 1. Start your day - ensure services are running
make up

# 2. View logs to see what's happening
make logs-backend   # Backend only
make logs-frontend  # Frontend only
make logs           # All services

# 3. Make code changes...

# 4. Services auto-reload on changes
# - Backend: Recompiles on file changes (via Docker build)
# - Frontend: Hot-reloads via Vite HMR

# 5. End of day - stop services (optional)
make down
```

### Making Backend Changes

```bash
# Edit Go files in backend/
vim backend/vod/processing.go

# Rebuild and restart to test changes
docker compose up -d --build api

# View logs
make logs-backend

# Run tests
cd backend && go test ./...

# Run linter
make lint-backend
```

### Making Frontend Changes

```bash
# Edit TypeScript/React files in frontend/
vim frontend/src/components/VodList.tsx

# Changes auto-reload via Vite HMR
# Just refresh your browser

# Run tests
cd frontend && npm test

# Run linter
make lint-frontend
```

### Testing API Endpoints

```bash
# List VODs
curl http://localhost:8080/vods | jq

# Get specific VOD
curl http://localhost:8080/vods/seed-completed-001 | jq

# Get VOD progress
curl http://localhost:8080/vods/seed-downloading-002/progress | jq

# Get chat messages for a VOD
curl http://localhost:8080/vods/seed-completed-001/chat | jq

# Get system status
curl http://localhost:8080/status | jq

# View metrics (Prometheus format)
curl http://localhost:8080/metrics

# Health checks
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz | jq
```

### Working with the Database

```bash
# Connect to database with psql
docker compose exec postgres psql -U vod -d vod

# Run SQL queries
docker compose exec postgres psql -U vod -d vod -c "SELECT * FROM vods LIMIT 5;"

# View tables
docker compose exec postgres psql -U vod -d vod -c "\dt"

# Export data
docker compose exec postgres pg_dump -U vod vod > backup.sql

# Import data
cat backup.sql | docker compose exec -T postgres psql -U vod -d vod
```

## Common Tasks

### Viewing Logs

```bash
# All services
make logs

# Specific service
make logs-backend
make logs-frontend
make logs-db

# Follow logs (tail -f style)
docker compose logs -f api

# Last 100 lines
docker compose logs --tail=100 api
```

### Running Tests

```bash
# All tests (backend + frontend)
make test

# Backend only
make test-backend
# or
cd backend && go test ./...

# Backend with race detector
cd backend && go test -race ./...

# Backend with coverage
cd backend && go test -cover ./...

# Frontend only
make test-frontend
# or
cd frontend && npm test
```

### Linting and Formatting

```bash
# Lint everything
make lint

# Lint backend only
make lint-backend

# Lint frontend only
make lint-frontend

# Auto-fix issues
make lint-fix
```

### Building

```bash
# Build everything
make build

# Build backend binary
make build-backend

# Build frontend bundle
make build-frontend

# Build Docker images
docker compose build
```

### Managing Services

```bash
# Start services
make up

# Stop services (keeps containers)
docker compose stop

# Start stopped services
docker compose start

# Restart services
make restart

# Stop and remove containers
make down

# View service status
make ps
# or
docker compose ps

# View resource usage
docker stats
```

## Troubleshooting

### Services Won't Start

**Problem:** `make up` fails or services keep restarting

**Solutions:**

1. Check Docker is running:
   ```bash
   docker ps
   ```

2. Check port conflicts:
   ```bash
   # Check if ports are already in use
   lsof -i :8080   # API
   lsof -i :3000   # Frontend
   lsof -i :5469   # Postgres
   ```

3. View error logs:
   ```bash
   make logs-backend
   make logs-db
   ```

4. Reset everything:
   ```bash
   make down
   docker compose down -v  # Remove volumes too
   make up
   ```

### Database Connection Errors

**Problem:** Backend can't connect to database

**Solutions:**

1. Check if Postgres is running:
   ```bash
   docker compose ps postgres
   ```

2. Check database credentials in `backend/.env`:
   ```bash
   DB_DSN=postgres://vod:vod@postgres:5432/vod?sslmode=disable
   ```

3. Restart Postgres:
   ```bash
   docker compose restart postgres
   ```

4. Reset database:
   ```bash
   make db-reset
   make restart
   make db-seed
   ```

### Sample Data Not Showing

**Problem:** Frontend shows no VODs

**Solutions:**

1. Verify data was loaded:
   ```bash
   curl http://localhost:8080/vods | jq
   ```

2. Reload sample data:
   ```bash
   make db-seed
   ```

3. Check database directly:
   ```bash
   docker compose exec postgres psql -U vod -d vod -c "SELECT COUNT(*) FROM vods;"
   ```

### Frontend Not Loading

**Problem:** http://localhost:3000 doesn't load

**Solutions:**

1. Check frontend service:
   ```bash
   make logs-frontend
   ```

2. Rebuild frontend:
   ```bash
   docker compose up -d --build frontend
   ```

3. Check for JavaScript errors in browser console (F12)

### API Returns Empty Results

**Problem:** API endpoints return empty arrays

**Solutions:**

1. Load sample data:
   ```bash
   make db-seed
   ```

2. Check migrations ran:
   ```bash
   docker compose exec postgres psql -U vod -d vod -c "\dt"
   # Should show: vods, chat_messages, oauth_tokens, kv
   ```

3. Restart API:
   ```bash
   docker compose restart api
   ```

### Port Already in Use

**Problem:** Error about port 8080, 3000, or 5469 already in use

**Solutions:**

1. Change ports in root `.env` file:
   ```bash
   cp .env.example .env
   # Edit .env and change:
   API_PORT=8090        # Instead of 8080
   FRONTEND_PORT=3090   # Instead of 3000
   ```

2. Or stop the conflicting service:
   ```bash
   # Find what's using the port
   lsof -i :8080
   # Kill that process
   kill -9 <PID>
   ```

### Out of Disk Space

**Problem:** Docker complains about disk space

**Solutions:**

```bash
# Clean up unused Docker resources
docker system prune -a

# Remove old volumes
docker volume prune

# Remove unused images
docker image prune -a
```

## Next Steps

### For New Contributors

1. Read [CONTRIBUTING.md](../CONTRIBUTING.md) for contribution guidelines
2. Review [docs/ARCHITECTURE.md](./ARCHITECTURE.md) to understand the system
3. Check open issues labeled "good first issue"
4. Join discussions in GitHub Discussions

### For Advanced Development

1. **Real Twitch Integration**
   - Add real Twitch credentials to `backend/.env`
   - Enable auto chat recording with `CHAT_AUTO_START=1`
   - See [docs/CONFIG.md](./CONFIG.md) for all options

2. **YouTube Upload**
   - Set up YouTube OAuth credentials
   - Configure `YT_CLIENT_ID` and `YT_CLIENT_SECRET`
   - See [README.md](../README.md#youtube-upload-configuration)

3. **Multi-Channel Setup**
   - Run multiple instances for different channels
   - See [docs/MULTI_CHANNEL.md](./MULTI_CHANNEL.md)

4. **Production Deployment**
   - Review [docs/DEPLOYMENT.md](./DEPLOYMENT.md)
   - Set up Kubernetes with [docs/KUBERNETES.md](./KUBERNETES.md)
   - Configure monitoring with [docs/OBSERVABILITY.md](./OBSERVABILITY.md)

5. **Security Hardening**
   - Enable token encryption with `ENCRYPTION_KEY`
   - Set up admin authentication
   - Review [docs/SECURITY.md](./SECURITY.md)

### Learning the Codebase

1. **Start with the data flow:**
   - `backend/vod/catalog.go` - VOD discovery
   - `backend/vod/processing.go` - Download & upload
   - `backend/chat/auto.go` - Live chat recording

2. **Key concepts:**
   - Circuit breaker pattern in `processing.go`
   - OAuth token management in `backend/oauth/`
   - API server in `backend/server/`

3. **Testing approach:**
   - Unit tests use mocks (see `*_test.go` files)
   - Integration tests use real database
   - Run with `TEST_PG_DSN` set

### Useful Resources

- **API Documentation**: [backend/api/openapi.yaml](../backend/api/openapi.yaml)
- **Architecture Overview**: [docs/ARCHITECTURE.md](./ARCHITECTURE.md)
- **Configuration Reference**: [docs/CONFIG.md](./CONFIG.md)
- **Operations Runbook**: [docs/OPERATIONS.md](./OPERATIONS.md)
- **Troubleshooting Guide**: [docs/RUNBOOKS.md](./RUNBOOKS.md)

## Getting Help

If you run into issues not covered here:

1. **Search existing issues**: [GitHub Issues](https://github.com/subculture-collective/vod-tender/issues)
2. **Ask in discussions**: [GitHub Discussions](https://github.com/subculture-collective/vod-tender/discussions)
3. **Check troubleshooting docs**: [docs/RUNBOOKS.md](./RUNBOOKS.md)

When asking for help, include:
- Your operating system
- Docker version (`docker --version`)
- Error messages and logs
- Steps to reproduce the issue

## Quick Reference

### Essential Commands

```bash
# Development
make dev-setup      # Complete setup with sample data
make up             # Start services
make down           # Stop services
make logs           # View all logs
make db-seed        # Load sample data

# Testing
make test           # Run all tests
make lint           # Run all linters

# Database
make db-reset       # Reset database
docker compose exec postgres psql -U vod -d vod  # Connect to DB

# Cleanup
make down           # Stop and remove containers
docker system prune # Clean up Docker resources
```

### Useful URLs

- Frontend: http://localhost:3000
- API: http://localhost:8080
- Health: http://localhost:8080/healthz
- Status: http://localhost:8080/status
- Metrics: http://localhost:8080/metrics
- Jaeger: http://localhost:16686

---

**Happy coding!** 🚀

For questions or feedback on this guide, please [open an issue](https://github.com/subculture-collective/vod-tender/issues/new).
