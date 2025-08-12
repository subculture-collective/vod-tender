###### Backend Multi-Stage Dockerfile (optimized) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
FROM golang:1.24 AS builder
WORKDIR /app

# Install build deps (sqlite headers, compiler)
RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc \
        libc6-dev \
        libsqlite3-dev \
        ca-certificates \
        && rm -rf /var/lib/apt/lists/*

ENV CGO_ENABLED=1 GO111MODULE=on

# Cache go mod first
COPY go.mod go.sum ./
RUN go mod download

# Copy rest of source
COPY . .

# Build (trimpath + smaller binary)
RUN go build -trimpath -ldflags="-s -w" -o /out/vod-tender-backend .

# --- Runtime stage ---
FROM debian:bookworm-slim AS runtime
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
        libsqlite3-0 \
        ca-certificates \
        tzdata \
        curl \
        && rm -rf /var/lib/apt/lists/*

# Non‑root user
RUN useradd -r -u 10001 appuser && chown appuser:appuser /app
COPY --from=builder /out/vod-tender-backend ./vod-tender-backend
USER appuser

EXPOSE 8080
ENV HTTP_ADDR=:8080

# Optional healthcheck hitting /healthz
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["./vod-tender-backend"]
