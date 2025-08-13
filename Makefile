# Makefile for vod-tender monorepo

.PHONY: help \
	build build-all run \
	test test-backend \
	lint lint-backend lint-frontend \
	backend-clean fmt-backend vet-backend \
	up down restart ps logs logs-backend logs-frontend \
	frontend-install frontend-dev frontend-build frontend-lint frontend-preview

.DEFAULT_GOAL := help

# Tools and settings
DC := docker compose

## Show this help
help:
	@echo "Targets (most common first):"; \
	grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sed -e 's/:.*##/: /' | sort

# ----------------------
# Docker Compose lifecycle
# ----------------------
up: ## Start all services with docker compose (detached)
	$(DC) up -d --build

down: ## Stop and remove services (and networks); keep volumes
	$(DC) down

restart: ## Restart app services
	$(DC) restart backend frontend

ps: ## List running compose services
	$(DC) ps

logs: ## Follow logs for all services
	$(DC) logs -f --tail=200

logs-backend: ## Follow backend logs
	$(DC) logs -f --tail=200 backend

logs-frontend: ## Follow frontend logs
	$(DC) logs -f --tail=200 frontend

 

# ----------------------
# Backend (Go)
# ----------------------
build: build-backend ## Build backend binary to project root

build-backend: ## Build backend binary to ./vod-tender-backend
	cd backend && go build -v -o ../vod-tender-backend .

run: build-backend ## Run backend locally (expects backend/.env to be set)
	./vod-tender-backend

test: test-backend ## Run backend tests

test-backend: ## go test ./... in backend
	cd backend && go test -race ./...

lint: lint-backend lint-frontend ## Lint backend and frontend

lint-backend: ## golangci-lint for backend (install from https://golangci-lint.run/)
	@[ -x "$$\(command -v golangci-lint\)" ] || { echo "golangci-lint not installed. Install from https://golangci-lint.run/"; exit 1; }
	cd backend && golangci-lint run ./...

fmt-backend: ## go fmt for backend
	cd backend && go fmt ./...

vet-backend: ## go vet for backend
	cd backend && go vet ./...

backend-clean: ## Clean backend build artifacts
	rm -f vod-tender-backend
	cd backend && go clean

# ----------------------
# Frontend (Vite + React)
# ----------------------
frontend-install: ## Install frontend dependencies
	cd frontend && npm ci

frontend-dev: ## Start frontend dev server
	cd frontend && npm run dev

frontend-build: ## Build frontend for production
	cd frontend && npm run build

frontend-lint: ## ESLint for frontend
	cd frontend && npm run lint

frontend-preview: ## Preview built frontend locally
	cd frontend && npm run preview

# ----------------------
# Aggregate
# ----------------------
build-all: build-backend frontend-build ## Build backend and frontend
