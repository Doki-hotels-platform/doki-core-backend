.PHONY: help build run-api run-worker test test-race lint tidy docker-up docker-down docker-build migrate-up migrate-down migrate-version migrate-verify

SHELL := /bin/bash
BIN_DIR := bin
API_BIN := $(BIN_DIR)/doki-api
WORKER_BIN := $(BIN_DIR)/doki-worker
MIGRATE_BIN := $(BIN_DIR)/doki-migrate

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "1.0.0-dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-w -s -X 'main.Version=$(VERSION)' -X 'main.BuildTime=$(BUILD_TIME)'"

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tidy: ## Download and tidy Go module dependencies
	go mod tidy
	go mod verify

build: ## Build all binaries (api, worker, migrate)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(API_BIN) ./cmd/api
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(WORKER_BIN) ./cmd/worker
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(MIGRATE_BIN) ./cmd/migrate
	@echo "Build complete: $(API_BIN), $(WORKER_BIN), $(MIGRATE_BIN)"

run-api: ## Run API server locally
	go run ./cmd/api

run-worker: ## Run background worker locally
	go run ./cmd/worker

test: ## Run unit tests
	go test -v ./...

test-race: ## Run all tests with Go race detector
	go test -race -v ./...

lint: ## Run golangci-lint with depguard import boundary verification
	golangci-lint run ./...

docker-up: ## Start local PostgreSQL and Redis dev stack
	docker compose up -d postgres redis

docker-down: ## Stop local Docker containers
	docker compose down

docker-build: ## Build production multi-stage distroless Docker image
	docker build -t doki-api:$(VERSION) -f Dockerfile --build-arg BIN=api .

migrate-up: ## Apply all pending database migrations
	go run ./cmd/migrate -up

migrate-down: ## Rollback 1 migration step
	go run ./cmd/migrate -down 1

migrate-version: ## Print current database migration version
	go run ./cmd/migrate -version

migrate-verify: ## Run SQL schema & constraint verification script against local postgres
	docker exec -i doki-postgres psql -U doki -d doki_db < test/verify_schema.sql
