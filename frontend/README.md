# vod-tender frontend

React + TypeScript + Vite single-page app for browsing VODs and replaying chat. Built and served by nginx in Docker; supports local dev via Vite.

Project overview and backend setup: see the root `README.md` and `docs/CONFIG.md`.

## Local development

Prereqs: Node 20+, npm.

- Install deps: `npm ci` (or `npm install`)
- Configure API base for dev: create `.env.local` with

```txt
VITE_API_BASE_URL=http://localhost:8080
```

- Start dev server: `npm run dev`

Open the printed URL (usually <http://localhost:5173>). The app will call the backend at `VITE_API_BASE_URL`.

## API base resolution

The frontend resolves the API base in this order (see `src/lib/api.ts`):

- `VITE_API_BASE_URL` (build-time env; recommended for local dev)
- If hosted on a domain like `vod-tender.<domain>`, it maps to `https://vod-api.<domain>` automatically
- Otherwise, same-origin

In Docker Compose + Caddy, no extra config is needed if you route `vod-tender.*` to the frontend and `vod-api.*` to the backend (see repo README).

## Build and ship

- Production build: `npm run build`
- Preview build locally: `npm run preview`
- Docker: `frontend/Dockerfile` builds the static assets and serves them via nginx. The top-level `docker-compose.yml` includes a `frontend` service; use `docker compose up -d --build` from the repo root.

## Features and endpoints

- List VODs: `GET /vods`
- VOD detail: `GET /vods/{id}` and `GET /vods/{id}/progress`
- Chat: `GET /vods/{id}/chat` (paged JSON) and `GET /vods/{id}/chat/stream` (SSE)

The backend emits empty arrays (`[]`) for list fields to avoid null errors in the UI.

## Troubleshooting

- Requests failing in dev: ensure `VITE_API_BASE_URL` points to your backend (default `http://localhost:8080`)
- In production: confirm domain mapping (`vod-tender.*` → frontend, `vod-api.*` → backend) and CORS if using same-origin fallback
- Blank list: verify the backend is running and `/vods` returns data; tail backend logs via `docker compose logs -f api`
