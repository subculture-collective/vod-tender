# Use an official Golang image as the build stage
FROM golang:1.22 as builder
WORKDIR /app
COPY backend/ ./backend/
WORKDIR /app/backend
RUN go mod tidy && go build -o /app/vod-tender-backend

# Use a minimal base image for running
FROM ubuntu:24.04
WORKDIR /app
COPY --from=builder /app/vod-tender-backend ./vod-tender-backend
# Don't bake secrets into the image; pass env at runtime. Optionally mount a file.
# COPY backend/.env ./backend.env
# Expose any ports if needed (e.g., 8080)
# EXPOSE 8080
CMD ["/app/vod-tender-backend"]
