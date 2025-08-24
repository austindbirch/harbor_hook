SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yaml --env-file deploy/docker/.env

.PHONY: proto build lint up down logs clean tidy

proto:
	@echo "Generating protos with buf..."
	buf generate

build:
	@echo "Building with Go..."
	go build ./...

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