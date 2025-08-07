# Makefile for vod-tender monorepo

.PHONY: all build run backend-backend backend-clean docker-build docker-run docker-clean

all: build

build:
	cd backend && go build -o ../vod-tender-backend

run: build
	./vod-tender-backend

backend-backend:
	cd backend && go build -o ../vod-tender-backend

backend-clean:
	rm -f vod-tender-backend
	cd backend && go clean

docker-build:
	docker build -t vod-tender .

docker-run:
	docker run --env-file backend/.env --rm vod-tender

docker-clean:
	docker rmi vod-tender || true
