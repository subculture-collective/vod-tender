# Minimal Makefile for vod-tender

.PHONY: help lint lint-backend lint-frontend lint-fix lint-fix-backend lint-fix-frontend test test-backend test-frontend build build-backend build-frontend up dcu down restart ps logs logs-backend logs-frontend logs-db db-reset db-seed dev-setup migrate-install migrate-create migrate-up migrate-down migrate-status migrate-force k8s-validate helm-validate

.DEFAULT_GOAL := help

DC := docker compose

## Show this help
help:
	@echo "Targets (most common first):"; \
	grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sed -e 's/:.*##/: /' | sort

# Docker Compose lifecycle
up: ## Start all services with docker compose (detached)
	$(DC) up -d --build

dcu: ## Alias for `up` (detached build)
	$(DC) up -d --build

down: ## Stop and remove services (and networks); keep volumes
	$(DC) down

restart: ## Restart app services
	$(DC) restart api frontend

ps: ## List running compose services
	$(DC) ps

logs: ## Follow logs for all services
	$(DC) logs -f --tail=200

logs-backend: ## Follow backend logs
	$(DC) logs -f --tail=200 api

logs-frontend: ## Follow frontend logs
	$(DC) logs -f --tail=200 frontend

logs-db: ## Follow Postgres logs
	$(DC) logs -f --tail=200 postgres


# Development - Unified targets for backend and frontend
lint: lint-backend lint-frontend ## Run all linters (backend + frontend)

lint-backend: ## Run golangci-lint on backend code
	@echo "Running backend linter..."
	@cd backend && golangci-lint run --timeout=5m

lint-frontend: ## Run ESLint and Prettier check on frontend code
	@echo "Running frontend linters..."
	@cd frontend && npm run lint && npm run format:check

lint-fix: lint-fix-backend lint-fix-frontend ## Auto-fix linter issues (backend + frontend)

lint-fix-backend: ## Run golangci-lint with auto-fix on backend code
	@echo "Running golangci-lint with --fix..."
	@cd backend && golangci-lint run --timeout=5m --fix

lint-fix-frontend: ## Auto-fix frontend linter issues
	@echo "Auto-fixing frontend linter issues..."
	@cd frontend && npm run lint:fix && npm run format

test: test-backend test-frontend ## Run all tests (backend + frontend)

test-backend: ## Run backend Go tests
	@echo "Running backend tests..."
	@cd backend && go test ./... -v

test-frontend: ## Run frontend tests with coverage
	@echo "Running frontend tests..."
	@cd frontend && npm run test:coverage

build: build-backend build-frontend ## Build all components (backend + frontend)

build-backend: ## Build backend Go binary
	@echo "Building backend..."
	@cd backend && go build -o vod-tender .

build-frontend: ## Build frontend production bundle
	@echo "Building frontend..."
	@cd frontend && npm run build

# Database
# Tries docker compose exec first; if unavailable, falls back to docker exec on ${STACK_NAME:-vod}-postgres
db-reset: ## Drop and recreate the Postgres database for this stack
	@echo "Resetting database (DROP/CREATE) using container env vars..."
	@POSTGRES_CONTAINER=$$(sh -c 'if [ -f .env ]; then . ./.env; printf "%s-postgres" "$${STACK_NAME:-vod}"; else printf "vod-postgres"; fi'); \
	DB_NAME_CMD=': "$${POSTGRES_DB:?}"; : "$${POSTGRES_USER:?}"; psql -U "$$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$$POSTGRES_DB\" WITH (FORCE);" -c "CREATE DATABASE \"$$POSTGRES_DB\";"'; \
	( $(DC) exec -T postgres bash -lc "set -e; $$DB_NAME_CMD" ) || ( echo "compose exec failed, trying docker exec on $$POSTGRES_CONTAINER"; docker start $$POSTGRES_CONTAINER >/dev/null 2>&1 || true; docker exec -i $$POSTGRES_CONTAINER bash -lc "set -e; $$DB_NAME_CMD" ); \
	echo "Database reset completed."

db-seed: ## Load sample development data into the database
	@echo "Loading seed data..."
	@./scripts/seed-dev-data.sh

dev-setup: ## Complete development setup (start services + seed data)
	@echo "Setting up complete development environment..."
	@$(MAKE) up
	@echo "Waiting for services to be ready..."
	@POSTGRES_CONTAINER=$$(sh -c 'if [ -f .env ]; then . ./.env; printf "%s-postgres" "$${STACK_NAME:-vod}"; else printf "vod-postgres"; fi'); \
	TIMEOUT=30; \
	for i in $$(seq 1 $$TIMEOUT); do \
	  if $(DC) exec -T postgres pg_isready -U vod >/dev/null 2>&1; then \
	    echo "✓ Postgres is ready!"; \
	    break; \
	  fi; \
	  if [ $$i -eq $$TIMEOUT ]; then \
	    echo "ERROR: Postgres did not become ready after $$TIMEOUT seconds."; \
	    exit 1; \
	  fi; \
	  sleep 1; \
	done
	@SEED_CONFIRM=yes $(MAKE) db-seed
	@echo ""
	@echo "✓ Development environment ready!"
	@echo ""
	@echo "Services:"
	@echo "  • API: http://localhost:8080"
	@echo "  • Frontend: http://localhost:3000"
	@echo "  • Jaeger: http://localhost:16686"
	@echo ""
	@echo "Quick commands:"
	@echo "  • View VODs: curl http://localhost:8080/vods | jq"
	@echo "  • View status: curl http://localhost:8080/status | jq"
	@echo "  • View logs: make logs"

# Database Migrations
# Install migrate CLI tool for local development
migrate-install: ## Install golang-migrate CLI tool
	@echo "Installing golang-migrate CLI..."
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "✓ migrate CLI installed to $$(go env GOPATH)/bin/migrate"

# Get DSN from environment or use default for compose
MIGRATE_DSN ?= postgres://vod:vod@localhost:5469/vod?sslmode=disable
MIGRATE_PATH := backend/db/migrations

migrate-create: ## Create new migration (Usage: make migrate-create name=add_something)
	@if [ -z "$(name)" ]; then \
		echo "Error: name parameter required. Usage: make migrate-create name=add_something"; \
		exit 1; \
	fi
	@echo "Creating new migration: $(name)"
	@migrate create -ext sql -dir $(MIGRATE_PATH) -seq $(name)
	@echo "✓ Migration files created in $(MIGRATE_PATH)"

migrate-up: ## Run all pending migrations
	@echo "Running pending migrations..."
	@migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DSN)" up
	@echo "✓ Migrations applied"

migrate-down: ## Rollback last migration
	@echo "Rolling back last migration..."
	@migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DSN)" down 1
	@echo "✓ Migration rolled back"

migrate-status: ## Show current migration version
	@echo "Current migration status:"
	@migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DSN)" version

migrate-force: ## Force set migration version (DANGEROUS - Usage: make migrate-force VERSION=1)
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION parameter required. Usage: make migrate-force VERSION=1"; \
		exit 1; \
	fi
	@echo "WARNING: Force setting migration version to $(VERSION)"
	@echo "This should only be used to fix a dirty state. Continue? [y/N]"
	@read -r confirm && [ "$$confirm" = "y" ] || (echo "Aborted" && exit 1)
	@migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DSN)" force $(VERSION)
	@echo "✓ Migration version forced to $(VERSION)"


# Kubernetes and Helm
k8s-validate: ## Validate Kubernetes manifests with kustomize
	@echo "Validating Kubernetes manifests..."
	@kubectl kustomize k8s/base > /dev/null && echo "✓ Base manifests valid"
	@kubectl kustomize k8s/overlays/dev > /dev/null && echo "✓ Dev overlay valid"
	@kubectl kustomize k8s/overlays/staging > /dev/null && echo "✓ Staging overlay valid"
	@kubectl kustomize k8s/overlays/production > /dev/null && echo "✓ Production overlay valid"

helm-validate: ## Validate Helm chart
	@echo "Validating Helm chart..."
	@helm lint charts/vod-tender && echo "✓ Chart linting passed"
	@helm template vod-tender charts/vod-tender > /dev/null && echo "✓ Chart rendering passed"

helm-docs: ## Generate Helm chart documentation
	@echo "Generating Helm documentation..."
	@cd charts/vod-tender && helm-docs || echo "helm-docs not installed, skipping"
