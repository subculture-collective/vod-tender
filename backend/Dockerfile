# Use an official Golang image as the build stage
FROM golang:1.24 as builder

# Install build dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    libc6-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .
WORKDIR /app/backend
# Make sure we download all dependencies and build with proper CGO for sqlite3
RUN CGO_ENABLED=1 go mod download && \
    go mod tidy && \
    go build -o /app/vod-tender-backend .

# Use a minimal base image for running
FROM ubuntu:24.04
WORKDIR /app
COPY --from=builder /app/vod-tender-backend ./vod-tender-backend
# Don't bake secrets into the image; pass env at runtime. Optionally mount a file.
# COPY backend/.env ./backend.env
# Expose any ports if needed (e.g., 8080)
# EXPOSE 8080
CMD ["/app/vod-tender-backend"]
