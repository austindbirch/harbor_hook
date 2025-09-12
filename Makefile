SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker/docker-compose.yaml --env-file deploy/docker/.env

# Build variables
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-w -s -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.Version=$(VERSION) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.GitCommit=$(GIT_COMMIT) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.BuildTime=$(BUILD_TIME)"

.PHONY: proto build build-cli install-cli uninstall-cli lint up up-fast down down-clean logs logs-gateway logs-obs clean clean-all tidy test-cli certs demo-gateway token help

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

# Generate TLS certificates for Envoy and mTLS
certs:
	@echo "Generating TLS certificates..."
	cd deploy/docker/envoy/certs && ./generate_certs.sh

up:
	@echo "Starting docker containers with full rebuild..."
	$(COMPOSE) up --build --force-recreate -d
	@echo "✅ All containers started successfully!"

down:
	@echo "Stopping docker containers and cleaning up..."
	$(COMPOSE) down -v --remove-orphans
	@echo "Pruning unused Docker resources..."
	docker system prune -f --volumes
	@echo "Removing dangling images..."
	docker image prune -f
	@echo "✅ Cleanup completed successfully!"

# Fast up without rebuild (for quick restarts when no code changes)
up-fast:
	@echo "Starting docker containers (no rebuild)..."
	$(COMPOSE) up -d
	@echo "✅ All containers started successfully!"

# Clean shutdown without pruning (faster for quick restarts)
down-clean:
	@echo "Stopping docker containers..."
	$(COMPOSE) down -v --remove-orphans
	@echo "✅ Containers stopped successfully!"

# Demo Phase 4: Gateway and Security
demo-gateway:
	@echo "Running Phase 4 Gateway and Security demo..."
	./scripts/gateway/demo.sh

# Generate JWT token for testing
token:
	@echo "Generating JWT token..."
	@if [ -z "$(TENANT)" ]; then \
		echo "Usage: make token TENANT=tn_123 [TTL=3600]"; \
		exit 1; \
	fi
	./scripts/gateway/get_token.sh $(TENANT) $(TTL)

logs:
	@echo "Viewing docker logs..."
	$(COMPOSE) logs -f ingest worker fake-receiver

# View logs for gateway services only
logs-gateway:
	@echo "Viewing gateway logs..."
	$(COMPOSE) logs -f envoy jwks-server

# View logs for observability services only
logs-obs:
	@echo "Viewing observability logs..."
	$(COMPOSE) logs -f prometheus grafana tempo loki promtail

# Enhanced clean - more aggressive cleanup
clean-all:
	@echo "Performing comprehensive Docker cleanup..."
	$(COMPOSE) down -v --remove-orphans --rmi all
	docker system prune -af --volumes
	docker builder prune -f
	@echo "✅ Comprehensive cleanup completed!"

# Basic clean (preserved for compatibility)
clean:
	@echo "Basic Docker cleanup..."
	docker system prune -f

tidy:
	@echo "Tidying go.mod"
	go mod tidy

# Help target to document all available commands
help:
	@echo "Harbor Hook Development Commands"
	@echo "================================"
	@echo ""
	@echo "🏗️  Building & Development:"
	@echo "  build        - Build Go services"
	@echo "  build-cli    - Build harborctl CLI tool"
	@echo "  proto        - Generate protobuf code"
	@echo "  lint         - Run golangci-lint"
	@echo "  tidy         - Run go mod tidy"
	@echo "  certs        - Generate TLS certificates"
	@echo ""
	@echo "🚀 Docker Operations:"
	@echo "  up           - Start containers with full rebuild and cleanup"
	@echo "  up-fast      - Start containers without rebuild (quick restart)"
	@echo "  down         - Stop containers with comprehensive cleanup"
	@echo "  down-clean   - Stop containers without pruning (quick stop)"
	@echo "  clean        - Basic Docker system prune"
	@echo "  clean-all    - Comprehensive Docker cleanup (removes everything)"
	@echo ""
	@echo "🧪 Testing & Demo:"
	@echo "  test-cli     - Test harborctl CLI"
	@echo "  demo-gateway - Run Phase 4 gateway demo"
	@echo "  token        - Generate JWT token (Usage: make token TENANT=tn_123)"
	@echo ""
	@echo "📋 Monitoring:"
	@echo "  logs         - View main service logs"
	@echo "  logs-gateway - View gateway service logs only"
	@echo ""
	@echo "🔧 CLI Management:"
	@echo "  install-cli  - Install harborctl to /usr/local/bin"
	@echo "  uninstall-cli- Remove harborctl from /usr/local/bin"
	@echo ""
	@echo "💡 Quick workflows:"
	@echo "  Fresh start:  make down && make up"
	@echo "  Code changes: make down-clean && make up"
	@echo "  Quick test:   make up-fast"