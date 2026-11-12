###### Frontend Dockerfile (build + nginx serve) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
# Pin node with digest for reproducible builds
FROM --platform=$BUILDPLATFORM node:25-alpine@sha256:7e467cc5aa91c87e94f93c4608cf234ca24aac3ec941f7f3db207367ccccdd11 AS build
WORKDIR /app
ENV NODE_ENV=development

# Accept API base URL at build time so the SPA can call the API from the browser
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

COPY package.json package-lock.json ./

# Install the locked dev dependencies once, including TypeScript.
RUN --mount=type=cache,target=/root/.npm npm ci --include=dev

COPY . .

# Build with cache mount for Vite cache
RUN --mount=type=cache,target=/app/node_modules/.vite \
    npm run build

# --- Runtime stage ---
# Pin nginx with digest for reproducible builds
FROM nginx:1.31-alpine@sha256:713fc2ebfd92e8217db451477182ee91944b123d2b1ee9fd5a8c6e91ca6c3a11 AS runtime
# Apply available Alpine runtime security updates.
RUN apk upgrade --no-cache
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80

# Healthcheck: ensure index served
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O - http://127.0.0.1/ >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
