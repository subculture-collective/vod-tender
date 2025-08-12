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
RUN go build -trimpath -ldflags="-s -w" -o /out/vod-tender-backend .

# --- Runtime stage ---
FROM gcr.io/distroless/base-debian12 AS runtime
WORKDIR /app

# Distroless already non-root (UID 65532) and has certs
COPY --from=builder /out/vod-tender-backend ./vod-tender-backend

EXPOSE 8080
ENV HTTP_ADDR=:8080

# Healthcheck omitted (distroless lacks curl); rely on external compose healthcheck

ENTRYPOINT ["./vod-tender-backend"]
