###### Backend Multi-Stage Dockerfile (optimized) ######
# syntax=docker/dockerfile:1

# --- Build stage ---
# Pin golang with digest for reproducible builds
FROM golang:1.26.10@sha256:24a707af49702fa44f02785475d02fb752f6982d3e9c0afb7696e1f0204e7ce9 AS builder
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
ARG TARGETARCH
RUN set -eux; \
	apt-get update; \
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		ca-certificates curl xz-utils; \
	# Download yt-dlp (pinned version) and verify checksum
	YTDLP_VERSION="2026.02.04"; \
	curl -L "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp" -o /usr/local/bin/yt-dlp; \
	curl -L "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/SHA2-256SUMS" -o /tmp/SHA2-256SUMS; \
	cd /usr/local/bin && grep "yt-dlp$" /tmp/SHA2-256SUMS | sha256sum -c; \
	chmod +x /usr/local/bin/yt-dlp; \
	# Download static ffmpeg from John Van Sickle (pinned version for reproducibility)
	# Note: John Van Sickle builds don't provide separate checksum files, but pinning version ensures reproducibility
	FFMPEG_VERSION=7.0.2; \
	case "$TARGETARCH" in amd64|arm64) ;; *) echo "Unsupported media architecture: $TARGETARCH" >&2; exit 1 ;; esac; \
	FFMPEG_TARBALL="ffmpeg-${FFMPEG_VERSION}-${TARGETARCH}-static.tar.xz"; \
	FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/${FFMPEG_TARBALL}"; \
	curl -fL "$FFMPEG_URL" -o /tmp/${FFMPEG_TARBALL}; \
	tar xf /tmp/${FFMPEG_TARBALL} -C /tmp; \
	mv /tmp/ffmpeg-*-${TARGETARCH}-static/ffmpeg /usr/local/bin/ffmpeg; \
	mv /tmp/ffmpeg-*-${TARGETARCH}-static/ffprobe /usr/local/bin/ffprobe; \
	chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe; \
	ffmpeg -version >/dev/null; \
	ffprobe -version >/dev/null; \
	rm -rf /tmp/ffmpeg* /var/lib/apt/lists/*

# --- Runtime stage ---
# Static Go and media binaries allow a minimal Python runtime.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS runtime
WORKDIR /app

# Only install minimal runtime deps (ca-certificates for HTTPS, python3 for yt-dlp)
# Static ffmpeg from media stage eliminates need for libav* packages (~400MB savings)
# Require the fixes for CVE-2026-77214 and CVE-2026-115557, even with cached base layers.
RUN apk add --no-cache ca-certificates 'python3>=3.14.10-r0' 'libexpat>=2.9.0-r0' \
 && apk upgrade --no-cache

# Copy binaries from build stage
COPY --from=builder /out/vod-tender-backend ./vod-tender-backend
COPY --from=builder /out/healthcheck ./healthcheck
COPY --from=builder /out/migrate-tokens ./migrate-tokens

# Copy media tools from media stage
# Both ffmpeg and ffprobe are copied from the static build, as yt-dlp may use both for video downloads and processing.
# ffprobe is required for some yt-dlp operations, so both binaries are included in the runtime image.
COPY --from=media /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=media /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=media /usr/local/bin/ffprobe /usr/local/bin/ffprobe

EXPOSE 8080
ENV HTTP_ADDR=:8080

# Internal healthcheck binary copied; docker-compose uses it for container health.

ENTRYPOINT ["./vod-tender-backend"]
