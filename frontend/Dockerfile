###### Frontend Dockerfile (build + nginx serve) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
# Pin node with digest for reproducible builds
FROM node:25-alpine@sha256:26ded7f450a0ad37241d2ae97ea521a59cb551a1785c8a950f74b0a291ad3aea AS build
WORKDIR /app
ENV NODE_ENV=development

# Accept API base URL at build time so the SPA can call the API from the browser
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

COPY package.json package-lock.json* ./

# Install dependencies with buildkit cache mount
RUN --mount=type=cache,target=/root/.npm \
    if [ -f package-lock.json ]; then \
        npm ci --include=dev; \
    else \
        npm install --include=dev --no-audit --no-fund; \
    fi \
 && echo '--- DEBUG: listing installed react & typescript packages ---' \
 && npm ls react @types/react typescript || true

COPY . .

# Ensure TypeScript binary is available even if dev deps were omitted by npm ci
RUN npm install --no-save typescript@~5.9.3

# Build with cache mount for Vite cache
RUN --mount=type=cache,target=/app/node_modules/.vite \
    npm run build

# --- Runtime stage ---
# Pin nginx with digest for reproducible builds
FROM nginx:1.29-alpine@sha256:b3c656d55d7ad751196f21b7fd2e8d4da9cb430e32f646adcf92441b72f82b14 AS runtime
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80

# Healthcheck: ensure index served
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O - http://127.0.0.1/ >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
