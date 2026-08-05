.PHONY: build up down logs test unit multiarch

build:
	docker compose build

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f web worker

test:
	docker compose --profile tools run --rm test-sender

unit:
	docker run --rm -v "$$PWD:/src" -w /src golang:1.24-bookworm go test ./...

multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -t tsingest:$${APP_VERSION:-0.1.0} .
