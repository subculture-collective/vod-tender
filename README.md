# vod-tender

vod-tender is a self-hosted service for streamers who want to keep their own
Twitch VODs. It finds a channel's past broadcasts, downloads them with yt-dlp,
records live chat and ties it to the matching VOD, and can upload the videos to
a YouTube channel you own. A small web app lists the archived VODs and replays
chat alongside them.

## Responsible use

vod-tender is for archiving your own content, or content you have explicit
permission from the owner to archive.

- Archive your own public Twitch VODs.
- Record chat for channels you own or moderate and are authorized to archive.
- Upload to YouTube only content you own or are authorized to manage.

It is not for downloading or redistributing other people's content,
getting around subscriber-only or other access restrictions, or re-uploading
someone else's VODs. Subscriber-only and other restricted VODs are skipped. You
are responsible for following Twitch and YouTube terms, local law and copyright.

## Features

- Catalog backfill: discovers historical VODs through the Twitch Helix API, with
  limits on how many and how old.
- Processing queue: resumable yt-dlp downloads with a concurrency limit,
  retries with backoff, a circuit breaker that pauses work after repeated
  failures, and per-VOD priority, cancel and reprocess controls.
- Chat recording: records a VOD's chat when given its ID and start time, or, in
  auto mode, starts when the channel goes live and reconciles the recording to
  the real VOD once Twitch publishes it.
- Chat replay: the API serves chat as JSON and as a server-sent event stream
  from a chosen offset and speed, and the web app replays it.
- YouTube upload: off by default; turned on only with an explicit ownership
  declaration. Daily upload limits and a per-VOD "skip upload" flag.
- Multiple channels in one deployment, each with its own records, workers and
  circuit breaker ([multi-channel guide](docs/MULTI_CHANNEL.md)).
- Retention: removes old downloaded files by age or count while keeping the
  database records, with a dry-run mode ([retention](docs/RETENTION.md)).
- Twitch and YouTube OAuth flows that store refresh tokens in PostgreSQL,
  encrypted with AES-256-GCM when `ENCRYPTION_KEY` is set.
- Admin endpoints protected by basic auth or a token, with rate limiting.
- Observability: Prometheus metrics, OpenTelemetry tracing to Jaeger, `/healthz`
  and `/readyz`, optional pprof, plus Prometheus alert rules and a Grafana
  dashboard in `prometheus/` and `grafana/`
  ([observability](docs/OBSERVABILITY.md)).

The `/vods/{id}/segments` endpoint is reserved and returns 501 for now.

## Quick start

You need Docker with Compose. For work outside containers you also need Go 1.26,
Node.js 20 and golangci-lint.

```bash
git clone https://github.com/subculture-collective/vod-tender.git
cd vod-tender
cp backend/.env.example backend/.env
make dev-setup
```

`make dev-setup` starts PostgreSQL, the API, the frontend and Jaeger, then loads
sample VODs and chat. Then open:

- Frontend: <http://localhost:3000>
- API: <http://localhost:8080> (metrics at `/metrics`, status at `/status`)
- Jaeger: <http://localhost:16686>

The [local development guide](docs/LOCAL_DEV_GUIDE.md) walks through the setup.

Common `make` targets (run `make help` for the full list):

| Command | What it does |
| --- | --- |
| `make up` / `make down` | Start or stop the Compose stack |
| `make logs` | Follow logs from all services |
| `make db-seed` / `make db-reset` | Load sample data / reset the database |
| `make build` | Build the backend and frontend |
| `make test` | Run backend and frontend tests |
| `make lint` / `make lint-fix` | Run linters / fix what they can |
| `make migrate-up` / `make migrate-status` | Apply or inspect migrations |
| `make helm-validate` | Lint and render the Helm chart |

Each of `build`, `test` and `lint` also has `-backend` and `-frontend`
variants. Frontend end-to-end tests run with `npm run test:e2e` in `frontend/`.

## Configuration

The API reads environment variables; for Compose, put them in `backend/.env`
(start from [`backend/.env.example`](backend/.env.example)). The root
[`.env.example`](.env.example) sets Compose-level values such as `STACK_NAME`
and host ports (`API_PORT`, `FRONTEND_PORT`, `JAEGER_UI_PORT`).

Minimum for a single channel:

- `TWITCH_CHANNEL` (or `TWITCH_CHANNELS` as a comma-separated list)
- `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET` for Helix discovery and auto chat
- `TWITCH_BOT_USERNAME` and `TWITCH_OAUTH_TOKEN` for chat, or authorize through
  `/auth/twitch/start` and let the service store and refresh the token
- `DB_DSN` and `DATA_DIR`

For production also set `ENCRYPTION_KEY` (base64, 32 bytes), `ADMIN_USERNAME`
and `ADMIN_PASSWORD` or `ADMIN_TOKEN`, `ENV=production` (any value other than empty, `dev` or `development` turns off
permissive CORS) and `CORS_ALLOWED_ORIGINS`.

Other groups:

- Chat: `CHAT_AUTO_START`, `CHAT_AUTO_POLL_INTERVAL`, `TWITCH_VOD_ID`,
  `TWITCH_VOD_START`, `VOD_RECONCILE_DELAY`.
- Processing: `VOD_PROCESS_INTERVAL`, `MAX_CONCURRENT_DOWNLOADS`,
  `DOWNLOAD_MAX_ATTEMPTS`, `DOWNLOAD_RATE_LIMIT`, `CIRCUIT_FAILURE_THRESHOLD`,
  `CIRCUIT_OPEN_COOLDOWN`, `YTDLP_ARGS`, `YTDLP_VERBOSE`.
- Catalog: `VOD_CATALOG_BACKFILL_INTERVAL`, `VOD_CATALOG_MAX`,
  `VOD_CATALOG_MAX_AGE_DAYS`.
- YouTube: `YOUTUBE_UPLOAD_ENABLED=1` together with
  `YOUTUBE_UPLOAD_OWNERSHIP=self` or `authorized`; credentials through
  `YT_CLIENT_ID`, `YT_CLIENT_SECRET`, `YT_REDIRECT_URI` and `/auth/youtube/start`,
  or `YT_CREDENTIALS_FILE`/`YT_CREDENTIALS_JSON` with
  `YT_TOKEN_FILE`/`YT_TOKEN_JSON`. The token needs the
  `https://www.googleapis.com/auth/youtube.upload` scope. Limits:
  `UPLOAD_DAILY_LIMIT`, `UPLOAD_MAX_ATTEMPTS`.
- Retention: `RETENTION_KEEP_DAYS`, `RETENTION_KEEP_COUNT`,
  `RETENTION_INTERVAL`, `RETENTION_DRY_RUN`.
- Rate limiting: `RATE_LIMIT_ENABLED`, `RATE_LIMIT_REQUESTS_PER_IP`,
  `RATE_LIMIT_WINDOW_SECONDS`, `RATE_LIMIT_BACKEND`.
- Logging and tracing: `LOG_LEVEL`, `LOG_FORMAT` (`text` or `json`),
  `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACE_SAMPLE_RATE`, `ENABLE_PPROF`.

[docs/CONFIG.md](docs/CONFIG.md) lists every variable with defaults.

## Deploy

The bundled `docker-compose.yml` is the simplest self-hosted setup: PostgreSQL
16, the Go API (its image includes pinned yt-dlp and ffmpeg), the frontend built
with Vite and served by nginx, a backup container that runs `pg_dump` daily into
the `pgbackups` volume, and Jaeger. Put a reverse proxy with TLS in front of the
frontend and API.

```bash
docker compose up -d --build
docker compose ps
```

For Kubernetes there is a Helm chart in `charts/vod-tender` and Kustomize
manifests in `k8s/` with dev, staging and production overlays
([Kubernetes guide](docs/KUBERNETES.md)). [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)
covers deployment in more detail, and [docs/OPERATIONS.md](docs/OPERATIONS.md)
covers backups, restores and troubleshooting.

The OpenAPI spec is at [`backend/api/openapi.yaml`](backend/api/openapi.yaml).

## Project layout

| Path | Contents |
| --- | --- |
| `backend/` | Go service: API server, VOD processing, chat, OAuth, database and migrations |
| `frontend/` | React app for browsing VODs and replaying chat, with Vitest and Playwright tests |
| `charts/`, `k8s/` | Helm chart and Kustomize manifests |
| `prometheus/`, `grafana/` | Alert rules and dashboard |
| `scripts/` | Backup, restore, seed and CI helpers |
| `docs/` | Architecture, configuration, deployment and operations guides |

## Contributing

Report bugs and request features in
[GitHub issues](https://github.com/subculture-collective/vod-tender/issues). Ask
questions and share ideas in
[GitHub Discussions](https://github.com/subculture-collective/vod-tender/discussions).

Send code as a pull request on GitHub against `main`. CI runs on every pull
request: secret scanning, govulncheck, backend build, tests, vet and lint,
frontend type check, lint, tests and build, Playwright end-to-end tests,
migration and integration tests, and Docker image builds. Commit messages follow
Conventional Commits. See [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md); release history is in
[CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE).
