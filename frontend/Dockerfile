###### Frontend Dockerfile (build + nginx serve) ######
# syntax=docker/dockerfile:1
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
# Install dependencies (including dev) for build; fall back if lock absent
RUN if [ -f package-lock.json ]; then npm ci; else npm install --include=dev --no-audit --no-fund; fi \
 && echo '--- DEBUG: listing installed react & typescript packages ---' \
 && npm ls react @types/react typescript || true
COPY . .
RUN npm run build

FROM nginx:1.27-alpine AS runtime
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
# Healthcheck: ensure index served
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O - http://127.0.0.1/ >/dev/null 2>&1 || exit 1
CMD ["nginx", "-g", "daemon off;"]
