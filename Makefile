SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker/docker-compose.yaml --env-file deploy/docker/.env

# Build variables
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-w -s -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.Version=$(VERSION) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.GitCommit=$(GIT_COMMIT) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.BuildTime=$(BUILD_TIME)"

.PHONY: proto build build-cli install-cli uninstall-cli lint up down logs clean tidy test-cli

proto:
	@echo "Generating protos with buf..."
	buf generate

build:
	@echo "Building with Go..."
	go build ./...

build-cli:
	@echo "Building harborctl CLI..."
	go build $(LDFLAGS) -o bin/harborctl ./cmd/harborctl

install-cli: build-cli
	@echo "Installing harborctl to /usr/local/bin..."
	sudo cp bin/harborctl /usr/local/bin/
	@echo "harborctl installed successfully!"

uninstall-cli:
	@echo "Removing harborctl from /usr/local/bin..."
	sudo rm -f /usr/local/bin/harborctl
	@echo "harborctl uninstalled successfully!"

test-cli: build-cli
	@echo "Testing harborctl CLI..."
	./bin/harborctl version
	./bin/harborctl --help

lint:
	@echo "Linting code with golangci-lint..."
	golangci-lint run

up:
	@echo "Starting docker containers..."
	$(COMPOSE) up --build -d

down:
	@echo "Stopping docker containers..."
	$(COMPOSE) down -v

logs:
	@echo "Viewing docker logs..."
	$(COMPOSE) logs -f ingest worker

clean:
	@echo "Cleaning up docker..."
	docker system prune -f

tidy:
	@echo "Tidying go.mod"
	go mod tidy