# Minimal Makefile for vod-tender

.PHONY: help up dcu down restart ps logs logs-backend logs-frontend logs-db db-reset k8s-validate helm-validate

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


# Database
# Tries docker compose exec first; if unavailable, falls back to docker exec on ${STACK_NAME:-vod}-postgres
db-reset: ## Drop and recreate the Postgres database for this stack
	@echo "Resetting database (DROP/CREATE) using container env vars..."
	@POSTGRES_CONTAINER=$$(sh -c 'if [ -f .env ]; then . ./.env; printf "%s-postgres" "$${STACK_NAME:-vod}"; else printf "vod-postgres"; fi'); \
	DB_NAME_CMD=': "$${POSTGRES_DB:?}"; : "$${POSTGRES_USER:?}"; psql -U "$$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$$POSTGRES_DB\" WITH (FORCE);" -c "CREATE DATABASE \"$$POSTGRES_DB\";"'; \
	( $(DC) exec -T postgres bash -lc "set -e; $$DB_NAME_CMD" ) || ( echo "compose exec failed, trying docker exec on $$POSTGRES_CONTAINER"; docker start $$POSTGRES_CONTAINER >/dev/null 2>&1 || true; docker exec -i $$POSTGRES_CONTAINER bash -lc "set -e; $$DB_NAME_CMD" ); \
	echo "Database reset completed."

# Development
lint: ## Run golangci-lint on backend code
	@echo "Running golangci-lint..."
	@cd backend && golangci-lint run --timeout=5m

lint-fix: ## Run golangci-lint with auto-fix on backend code
	@echo "Running golangci-lint with --fix..."
	@cd backend && golangci-lint run --timeout=5m --fix

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
