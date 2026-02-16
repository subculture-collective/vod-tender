# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- CHANGELOG.md to track notable changes across releases

## [0.2.0] - 2026-02-15

### Added
- Distributed rate limiter for multi-replica deployments (#243)
- End-to-end local development guide with sample data seeding (#178)
- GitHub issue/PR templates and CODE_OF_CONDUCT (#177)
- Logging guide for JSON ingestion with Loki/ELK (#176)
- ESLint, Prettier, and unit tests for frontend components (#173)
- Progress polling and error states to VOD detail page (#172)
- Prometheus scrape config and Grafana dashboard documentation (#167)
- Health/readiness endpoints with Docker Compose healthchecks and K8s probe patterns (#166)
- Prometheus histogram telemetry with PromQL examples and test coverage (#164)
- Segmentation API design spec to replace 501 placeholder (#163)
- SSE chat replay timing and backpressure validation tests (#162)
- Edge-case tests for Helix pagination and catalog backfill (#161)
- Focused test suite for chat reconciliation (#159)
- Error classification for download failures and aria2c detection (#157)
- Half-open probing and Prometheus metrics for circuit breaker (#156)
- OAuth token encryption migration tool (#155)
- Download scheduler with priority management, concurrency control, and bandwidth limits (#154)
- Admin authentication, rate limiting, and CORS hardening (#130)
- Comprehensive observability with distributed tracing (#108)
- Manual tag trigger to release workflow and enhanced SBOM artifacts (#175)
- Versioned database migrations with golang-migrate framework (#123)
- Multi-channel support to database schema and core processing functions (#117)

### Changed
- Refactored NewMux into handler modules for improved maintainability (#250)
- Unified dual migration system with comprehensive documentation (#244)
- Optimized Docker images: 614MB→321MB backend, pinned digests, BuildKit cache (#165)
- Updated to Go 1.24.x across all workflows

### Fixed
- uploadToYouTube now reuses DB connection pool (#245)
- Cancel endpoint to handle race conditions and maintain consistent DB state (#158)
- Port stripping logic for IPv6 addresses (#134)
- Rate limiting path matching to use regex instead of suffix matching (#136)
- Header order in readyz endpoint - set Content-Type before WriteHeader (#114)
- Redundant constraint check in database migrations (#119)

### Removed
- Deprecated rechat.twitch.tv chat import API (#242)

### Security
- Implemented OAuth token envelope encryption design proposal (#126)

## [0.1.0] - 2025-10-26

### Added
- Initial release of vod-tender
- Go backend service for Twitch VOD archival automation
- Helix API integration for VOD discovery
- yt-dlp integration for VOD downloads with resume support
- Live chat recording to PostgreSQL
- Optional YouTube upload functionality
- React + TypeScript frontend for VOD browsing and chat replay
- Docker Compose development environment
- PostgreSQL database schema with idempotent migrations
- OAuth token management with automatic refresh
- Circuit breaker pattern for download resilience
- Prometheus metrics exposition at `/metrics`
- HTTP server with health endpoints (`/healthz`, `/readyz`)
- Configurable retention policy and automated storage cleanup (#137)
- Semantic versioning with commitlint and auto-merge for dependencies
- Comprehensive CI/CD pipeline with GitHub Actions
  - Code linting (golangci-lint)
  - Security scanning (gitleaks, govulncheck, Trivy)
  - Race detection
  - SBOM generation
  - Multi-platform Docker builds (amd64, arm64)
  - Image signing with cosign
- Makefile with unified lint/test/build targets (#125)

### Documentation
- ARCHITECTURE.md with component diagram and data flow
- CONFIG.md with exhaustive environment variable reference
- OPERATIONS.md runbook for multi-instance deployment
- CONTRIBUTING.md with development workflows and coding standards
- LOCAL_DEV_GUIDE.md for quick onboarding
- MIGRATIONS.md for database schema changes

[Unreleased]: https://github.com/subculture-collective/vod-tender/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/subculture-collective/vod-tender/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/subculture-collective/vod-tender/releases/tag/v0.1.0
