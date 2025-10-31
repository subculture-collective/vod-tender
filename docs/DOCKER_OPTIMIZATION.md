# Docker Image Optimization

This document describes the optimizations applied to vod-tender's Docker images to reduce size and improve build efficiency.

## Results

### Image Sizes
- **Backend**: 614MB → 321MB (47.7% reduction)
- **Frontend**: 53.2MB (already optimal, <100MB target)

### Target Compliance
- Backend: 321MB vs 300MB target (7% over, acceptable given requirements)
- Frontend: 53.2MB vs 100MB target ✅

## Backend Optimizations

### 1. Static FFmpeg Binary
**Problem**: apt-installed ffmpeg package adds ~400MB of dependencies (libav*, codecs, etc.)

**Solution**: Download John Van Sickle's static ffmpeg builds (~80MB each for ffmpeg/ffprobe)
- Source: https://johnvansickle.com/ffmpeg/
- Trusted, regularly updated builds
- Includes all codecs needed for Twitch VOD processing
- No system library dependencies
- Version pinned (7.0.2) for reproducibility

```dockerfile
# Download static ffmpeg from John Van Sickle (pinned version)
FFMPEG_VERSION=7.0.2;
FFMPEG_TARBALL="ffmpeg-${FFMPEG_VERSION}-amd64-static.tar.xz";
FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/${FFMPEG_TARBALL}";
curl -L "$FFMPEG_URL" -o /tmp/${FFMPEG_TARBALL};
tar xf /tmp/${FFMPEG_TARBALL} -C /tmp;
mv /tmp/ffmpeg-*-amd64-static/ffmpeg /usr/local/bin/ffmpeg;
mv /tmp/ffmpeg-*-amd64-static/ffprobe /usr/local/bin/ffprobe;
```

**Savings**: ~320MB

**Security**: Version is pinned for reproducibility. John Van Sickle doesn't provide separate checksum files, but pinning to a specific version prevents unintended changes.

### 1a. yt-dlp with Checksum Verification
**Problem**: Downloading binaries without verification poses security risks

**Solution**: Pin yt-dlp version and verify with SHA256 checksums from GitHub releases
- Version pinned (2024.04.09) for reproducibility
- Checksum verification using official SHA2-256SUMS file
- Prevents tampering or corrupted downloads

```dockerfile
# Download yt-dlp (pinned version) and verify checksum
YTDLP_VERSION="2024.04.09";
curl -L "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp" -o /usr/local/bin/yt-dlp;
curl -L "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/SHA2-256SUMS" -o /tmp/SHA2-256SUMS;
cd /usr/local/bin && grep "yt-dlp$" /tmp/SHA2-256SUMS | sha256sum -c;
```

**Security**: Both version pinning and checksum verification provide strong assurance of build reproducibility and integrity.

### 2. BuildKit Cache Mounts
**Problem**: Go modules and build artifacts re-downloaded on every build

**Solution**: Use BuildKit cache mounts to persist across builds
```dockerfile
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download
```

**Benefits**:
- Faster incremental builds (seconds vs minutes)
- Reduced network bandwidth usage
- Works with GitHub Actions cache (`type=gha`)

### 3. Pinned Base Images
**Problem**: Tag-based images (`golang:1.25`, `debian:bookworm-slim`) can change content

**Solution**: Pin with SHA256 digests
```dockerfile
FROM golang:1.25@sha256:6bac879c5b77e0fc9c556a5ed8920e89dab1709bd510a854903509c828f67f96
FROM debian:bookworm-slim@sha256:78d2f66e0fec9e5a39fb2c72ea5e052b548df75602b5215ed01a17171529f706
```

**Benefits**:
- Reproducible builds across environments
- Security: prevents base image tampering
- Explicit about dependency versions

### 4. Size Breakdown (Final)
```
Debian base:              74.8MB
Python3 (for yt-dlp):     46.5MB
ffmpeg static:            79.8MB
ffprobe static:           79.7MB
Go binaries:              ~40MB
yt-dlp:                    3.09MB
ca-certificates:           ~5MB
─────────────────────────────────
Total:                    ~321MB
```

## Frontend Optimizations

### 1. BuildKit Cache Mounts
**Problem**: npm dependencies and Vite cache re-downloaded/rebuilt on every build

**Solution**: Cache npm and Vite build artifacts
```dockerfile
RUN --mount=type=cache,target=/root/.npm \
    npm ci --include=dev

RUN --mount=type=cache,target=/app/node_modules/.vite \
    npm run build
```

### 2. Pinned Base Images
```dockerfile
FROM node:25-alpine@sha256:7e467cc5aa91c87e94f93c4608cf234ca24aac3ec941f7f3db207367ccccdd11
FROM nginx:1.29-alpine@sha256:b3c656d55d7ad751196f21b7fd2e8d4da9cb430e32f646adcf92441b72f82b14
```

### 3. Size Analysis
Frontend was already optimal at 53.2MB:
- nginx:alpine base: ~45MB
- Built static assets: ~8MB

No further optimization needed.

## CI/CD Integration

### GitHub Actions Cache
Both Dockerfiles work seamlessly with GitHub Actions cache:

```yaml
- name: Build backend Docker image
  uses: docker/build-push-action@v6
  with:
    context: ./backend
    cache-from: type=gha
    cache-to: type=gha,mode=max
```

The `type=gha` cache backend:
- Automatically manages cache across workflow runs
- Supports multi-stage builds
- Works with both amd64 and arm64 architectures
- Max mode (`mode=max`) caches all layers for optimal reuse

## Build Performance

### Without Cache (Cold Build)
- Backend: ~2-3 minutes
- Frontend: ~30-45 seconds

### With Cache (Warm Build)
- Backend: ~30-45 seconds
- Frontend: ~10-15 seconds

### Cache Hit Scenarios
- Go modules unchanged: Skip download (~30s saved)
- Source unchanged: Skip Go build (~20s saved)
- npm packages unchanged: Skip npm ci (~15s saved)
- Frontend unchanged: Skip Vite build (~5s saved)

## Maintenance

### Updating Base Image Digests
When updating to newer base images:

1. Pull the image: `docker pull golang:1.26`
2. Get the digest: `docker inspect golang:1.26 --format='{{index .RepoDigests 0}}'`
3. Update Dockerfile: `FROM golang:1.26@sha256:...`

### Updating Static ffmpeg
John Van Sickle releases are tracked at: https://johnvansickle.com/ffmpeg/

Current version: 7.0.2

To update to a newer version:
1. Check available versions at https://johnvansickle.com/ffmpeg/
2. Update the `FFMPEG_VERSION` variable in the Dockerfile
3. Test the build to ensure the new version works

```dockerfile
FFMPEG_VERSION=7.1.0;  # Update this line
```

**Note**: John Van Sickle doesn't provide separate checksum files. Version pinning ensures reproducibility, but checksums are not verified.

### Updating yt-dlp
Current version: 2024.04.09

To update to a newer version with checksum verification:
1. Check latest release at https://github.com/yt-dlp/yt-dlp/releases
2. Update the `YTDLP_VERSION` variable in the Dockerfile
3. Build will automatically verify the checksum from the release's SHA2-256SUMS file

```dockerfile
YTDLP_VERSION="2024.11.18";  # Update this line
```

**Security**: Checksum verification ensures the downloaded binary matches the official release.

## Why Not Further Optimization?

### Why Keep Python3? (46.5MB)
yt-dlp is a Python application that requires:
- Full Python 3 standard library (getpass, urllib, json, etc.)
- python3-minimal is too minimal (missing required modules)
- Alternative: Compile yt-dlp to standalone binary (not officially supported)

**Decision**: Keep python3 for maintainability and compatibility

### Why Keep ffprobe? (79.7MB)
yt-dlp uses ffprobe for:
- Video format detection
- Stream quality analysis
- Metadata extraction

Removing it could break edge cases in video processing.

**Decision**: Keep ffprobe for reliability

### Why Not Distroless?
Distroless images don't include:
- Shell (needed by healthcheck scripts)
- Package manager (needed for debugging)
- Python runtime (needed by yt-dlp)

**Decision**: debian:bookworm-slim provides good balance of size and functionality

## Testing

### Verify Optimized Images
```bash
# Build images
docker compose build

# Check sizes
docker images | grep vod-tender

# Test functionality
docker compose up -d
curl http://localhost:8080/healthz
curl http://localhost:3000/

# Verify media tools
docker exec vod-api ffmpeg -version
docker exec vod-api yt-dlp --version
```

### Verify Cache Works
```bash
# First build (cold)
time docker build backend/

# Second build (warm - should be <5s)
time docker build backend/
```

## References
- [BuildKit Cache Mounts](https://docs.docker.com/build/cache/backends/)
- [GitHub Actions Cache](https://docs.docker.com/build/ci/github-actions/cache/)
- [John Van Sickle FFmpeg Builds](https://johnvansickle.com/ffmpeg/)
- [Multi-stage Dockerfile Best Practices](https://docs.docker.com/build/building/multi-stage/)
