###### Backend Multi-Stage Dockerfile (optimized) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
FROM golang:1.24 AS builder
WORKDIR /app

# No native deps needed (pure Go build with pgx)
ENV CGO_ENABLED=0 GO111MODULE=on

# Cache go mod first
COPY go.mod go.sum ./
RUN go mod download

# Copy rest of source
COPY . .

# Build (trimpath + smaller binary)
RUN go build -trimpath -ldflags="-s -w" -o /out/vod-tender-backend . \
 && go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

# Fetch yt-dlp and ffmpeg in a tiny stage
FROM debian:bookworm-slim AS media
RUN set -eux; \
	apt-get update; \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		ca-certificates curl ffmpeg; \
	curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp; \
	chmod +x /usr/local/bin/yt-dlp; \
	rm -rf /var/lib/apt/lists/*

# --- Runtime stage ---
FROM debian:bookworm-slim AS runtime
WORKDIR /app

# Distroless already non-root (UID 65532) and has certs
RUN set -eux; \
	apt-get update; \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		ca-certificates ffmpeg; \
	rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/vod-tender-backend ./vod-tender-backend
COPY --from=builder /out/healthcheck ./healthcheck
COPY --from=media /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp

EXPOSE 8080
ENV HTTP_ADDR=:8080

# Internal healthcheck binary copied; docker-compose uses it for container health.

ENTRYPOINT ["./vod-tender-backend"]
