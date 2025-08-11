# Makefile for vod-tender monorepo

.PHONY: all build run test lint backend-clean docker-build docker-run docker-clean

all: build

build:
	cd backend && go build -o ../vod-tender-backend

run: build
	./vod-tender-backend

test:
	cd backend && go test ./...

lint:
	@[ -x "$$(command -v golangci-lint)" ] || { echo "golangci-lint not installed. Install from https://golangci-lint.run/"; exit 1; }
	cd backend && golangci-lint run ./...

backend-clean:
	rm -f vod-tender-backend
	cd backend && go clean

docker-build:
	docker build -t vod-tender .

docker-run:
	docker run -p 8080:8080 --env-file backend/.env --rm vod-tender

docker-clean:
	docker rmi vod-tender || true
