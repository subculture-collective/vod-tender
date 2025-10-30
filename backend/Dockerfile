###### Backend Multi-Stage Dockerfile (optimized) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
# Pin golang with digest for reproducible builds
FROM golang:1.25@sha256:6bac879c5b77e0fc9c556a5ed8920e89dab1709bd510a854903509c828f67f96 AS builder
WORKDIR /app

# No native deps needed (pure Go build with pgx)
ENV CGO_ENABLED=0 GO111MODULE=on

# Cache go mod first with buildkit cache mount
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Copy rest of source
COPY . .

# Build (trimpath + smaller binary) with buildkit cache
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/vod-tender-backend . \
 && go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck \
 && go build -trimpath -ldflags="-s -w" -o /out/migrate-tokens ./cmd/migrate-tokens

# Fetch yt-dlp and static ffmpeg (saves ~400MB vs apt package)
FROM debian:bookworm-slim@sha256:78d2f66e0fec9e5a39fb2c72ea5e052b548df75602b5215ed01a17171529f706 AS media
RUN set -eux; \
	apt-get update; \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		ca-certificates curl xz-utils; \
	# Download yt-dlp (latest stable release)
	curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp; \
	chmod +x /usr/local/bin/yt-dlp; \
	# Download static ffmpeg from John Van Sickle (trusted source, ~70MB vs ~400MB apt package)
	curl -L https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz -o /tmp/ffmpeg.tar.xz; \
	tar xf /tmp/ffmpeg.tar.xz -C /tmp; \
	mv /tmp/ffmpeg-*-amd64-static/ffmpeg /usr/local/bin/ffmpeg; \
	mv /tmp/ffmpeg-*-amd64-static/ffprobe /usr/local/bin/ffprobe; \
	chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe; \
	rm -rf /tmp/ffmpeg* /var/lib/apt/lists/*

# --- Runtime stage ---
# Pin debian with digest for reproducible builds
FROM debian:bookworm-slim@sha256:78d2f66e0fec9e5a39fb2c72ea5e052b548df75602b5215ed01a17171529f706 AS runtime
WORKDIR /app

# Only install minimal runtime deps (ca-certificates for HTTPS, python3 for yt-dlp)
# Static ffmpeg from media stage eliminates need for libav* packages (~400MB savings)
RUN set -eux; \
	apt-get update; \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
	ca-certificates python3; \
	rm -rf /var/lib/apt/lists/*

# Copy binaries from build stage
COPY --from=builder /out/vod-tender-backend ./vod-tender-backend
COPY --from=builder /out/healthcheck ./healthcheck
COPY --from=builder /out/migrate-tokens ./migrate-tokens

# Copy media tools from media stage
# Note: ffprobe is included with static ffmpeg build but we only copy ffmpeg for video downloads
# yt-dlp may use ffprobe for advanced operations, but basic video download works with ffmpeg alone
COPY --from=media /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=media /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=media /usr/local/bin/ffprobe /usr/local/bin/ffprobe

EXPOSE 8080
ENV HTTP_ADDR=:8080

# Internal healthcheck binary copied; docker-compose uses it for container health.

ENTRYPOINT ["./vod-tender-backend"]
