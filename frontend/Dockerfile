###### Frontend Dockerfile (build + nginx serve) ######
# syntax=docker/dockerfile:1
FROM node:25-alpine AS build
WORKDIR /app
ENV NODE_ENV=development
# Accept API base URL at build time so the SPA can call the API from the browser
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
COPY package.json package-lock.json* ./
# Install dependencies (including dev) for build; fall back if lock absent
RUN if [ -f package-lock.json ]; then npm ci --include=dev; else npm install --include=dev --no-audit --no-fund; fi \
 && echo '--- DEBUG: listing installed react & typescript packages ---' \
 && npm ls react @types/react typescript || true
COPY . .
# Ensure TypeScript binary is available even if dev deps were omitted by npm ci
RUN npm install --no-save typescript@~5.9.3
RUN npm run build

FROM nginx:1.29-alpine AS runtime
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
# Healthcheck: ensure index served
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O - http://127.0.0.1/ >/dev/null 2>&1 || exit 1
CMD ["nginx", "-g", "daemon off;"]
