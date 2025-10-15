SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker/docker-compose.yaml --env-file deploy/docker/.env

# Build variables
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-w -s -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.Version=$(VERSION) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.GitCommit=$(GIT_COMMIT) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.BuildTime=$(BUILD_TIME)"

.PHONY: proto build lint install-cli uninstall-cli certs token up down up-full down-full restart logs logs-gateway logs-obs clean help kind-up-and-test kind-down

# Go commands
proto:
	@echo "Generating protos with buf..."
	buf generate

build:
	@echo "Building with Go..."
	go build ./...

lint:
	@echo "Linting code with golangci-lint..."
	golangci-lint run

# Harborctl commands
install-cli:
	@echo "Building harborctl CLI..."
	go build $(LDFLAGS) -o bin/harborctl ./cmd/harborctl
	@echo "Installing harborctl to /usr/local/bin..."
	sudo cp bin/harborctl /usr/local/bin/
	./bin/harborctl version
	./bin/harborctl --help
	@echo "harborctl installed successfully!"

uninstall-cli:
	@echo "Removing harborctl from /usr/local/bin..."
	sudo rm -f /usr/local/bin/harborctl
	@echo "harborctl uninstalled successfully!"

# Security/TLS commands
certs:
	@echo "Generating TLS certificates..."
	cd deploy/docker/envoy/certs && ./generate_certs.sh

token:
	@echo "Generating JWT token..."
	@if [ -z "$(TENANT)" ]; then \
		echo "Usage: make token TENANT=tn_123 [TTL=3600]"; \
		exit 1; \
	fi
	./scripts/gateway/get_token.sh $(TENANT) $(TTL)

# Docker commands
up:
	@echo "Starting docker containers..."
	$(COMPOSE) up --build -d --quiet-pull --quiet-build
	@echo "✅ All containers started successfully!"

down:
	@echo "Stopping docker containers and cleaning up..."
	$(COMPOSE) down --remove-orphans
	@echo "✅ Containers stopped successfully!"

up-full:
	@echo "Starting docker containers with full rebuild..."
	$(COMPOSE) up --build -d --quiet-pull --quiet-build
	@echo "✅ All containers started successfully!"

down-full:
	@echo "Stopping docker containers and cleaning up..."
	$(COMPOSE) down -v --remove-orphans --rmi all
	@echo "✅ Containers stopped successfully!"
	@echo "Pruning unused Docker resources..."
	docker system prune -a -f --volumes
	@echo "✅ Cleanup completed successfully!"

restart: down up

logs:
	@echo "Viewing docker logs..."
	$(COMPOSE) logs -f ingest worker fake-receiver

logs-gateway:
	@echo "Viewing gateway logs..."
	$(COMPOSE) logs -f envoy jwks-server

logs-obs:
	@echo "Viewing observability logs..."
	$(COMPOSE) logs -f prometheus grafana tempo loki promtail

clean:
	@echo "Docker cleanup..."
	docker system prune -af --volumes
	docker builder prune -f

# Kubernetes commands
kind-up-and-test:
	@echo "Creating Kind cluster..."
	kind create cluster --name harborhook
	@echo "Generating TLS certificates..."
	cd deploy/docker/envoy/certs && ./generate_certs.sh && cd ../../../..
	@echo "Creating TLS secret..."
	kubectl create secret generic harborhook-certs \
		--from-file=ca.crt=./deploy/docker/envoy/certs/ca.crt \
		--from-file=server.crt=./deploy/docker/envoy/certs/server.crt \
		--from-file=server.key=./deploy/docker/envoy/certs/server.key \
		--from-file=client.crt=./deploy/docker/envoy/certs/client.crt \
		--from-file=client.key=./deploy/docker/envoy/certs/client.key
	@echo "Updating Helm dependencies..."
	helm dependency update ./charts/harborhook
	@echo "Installing Harborhook chart..."
	helm install harborhook ./charts/harborhook
	@echo "Waiting for deployments to be ready..."
	kubectl wait --for=condition=Available --timeout=5m deployment --all
	@echo "✅ Harborhook cluster is up and running!"

kind-down:
	@echo "Deleting Kind cluster..."
	kind delete cluster --name harborhook

# Help target to document all available commands
help:
	@echo "Harborhook Development Commands"
	@echo "================================"
	@echo ""
	@echo "🏗️  Building & Development:"
	@echo "  proto        - Generate protobuf code"
	@echo "  build        - Build Go services"
	@echo "  lint         - Run golangci-lint"
	@echo ""
	@echo "🔧 CLI Management:"
	@echo "  install-cli  - Install harborctl to /usr/local/bin"
	@echo "  uninstall-cli- Remove harborctl from /usr/local/bin"
	@echo ""
	@echo "🧪 Security and Tokens:"
	@echo "  certs        - Generate TLS certificates"
	@echo "  token        - Generate JWT token (Usage: make token TENANT=tn_123)"
	@echo ""
	@echo "🚀 Docker Operations:"
	@echo "  up           - Start containers without full rebuild"
	@echo "  down         - Stop containers with basic pruning"
	@echo "  up-full      - Start containers with full rebuild"
	@echo "  down-full    - Stop containers with full cleanup"
	@echo "  restart      - Restart all containers"
	@echo "  logs         - View main service logs"
	@echo "  logs-gateway - View gateway service logs only"
	@echo "  logs-obs     - View observability service logs only"
	@echo "  clean        - Basic Docker system prune"
	@echo ""
	@echo "☸️  Kubernetes Operations:"
	@echo "  kind-up-and-test - Create a Kind cluster and deploy Harborhook"
	@echo "  kind-down    - Delete the Kind cluster"
	@echo ""
