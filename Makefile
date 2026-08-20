.PHONY: help build run-api run-worker test test-race lint tidy docker-up docker-down docker-build migrate-up

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
	cd doki_backend && go mod tidy && go mod verify

build: ## Build all binaries (api, worker, migrate)
	@mkdir -p doki_backend/$(BIN_DIR)
	cd doki_backend && CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/doki-api ./cmd/api
	cd doki_backend && CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/doki-worker ./cmd/worker
	cd doki_backend && CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/doki-migrate ./cmd/migrate
	@echo "Build complete."

run-api: ## Run API server locally
	cd doki_backend && go run ./cmd/api

run-worker: ## Run background worker locally
	cd doki_backend && go run ./cmd/worker

test: ## Run unit tests
	cd doki_backend && go test -v ./...

test-race: ## Run all tests with Go race detector
	cd doki_backend && go test -race -v ./...

lint: ## Run golangci-lint with depguard import boundary verification
	cd doki_backend && golangci-lint run ./...

docker-up: ## Start local PostgreSQL and Redis dev stack
	cd doki_backend && docker compose up -d postgres redis

docker-down: ## Stop local Docker containers
	cd doki_backend && docker compose down

docker-build: ## Build production multi-stage distroless Docker image
	cd doki_backend && docker build -t doki-api:$(VERSION) -f Dockerfile --build-arg BIN=api .

migrate-up: ## Run SQL migrations against local database
	cd doki_backend && go run ./cmd/migrate
