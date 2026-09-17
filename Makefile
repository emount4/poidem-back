.DEFAULT_GOAL := help
COMPOSE := docker compose

.PHONY: help init run build test vet fmt up down logs ps db-up db-shell

help:
	@echo "init      - create .env from .env.example if missing"
	@echo "up/down   - start or stop app and PostgreSQL (keep data)"
	@echo "logs/ps   - show container logs or status"
	@echo "db-up     - start PostgreSQL only"
	@echo "db-shell  - open psql inside PostgreSQL"
	@echo "run/build - run locally or build to bin/"
	@echo "test/vet/fmt - check or format Go code"

init:
ifeq ($(OS),Windows_NT)
	@if not exist .env copy .env.example .env
else
	@test -f .env || cp .env.example .env
endif

run:
	go run ./cmd/api

build:
	go build -o bin/ ./cmd/api

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

up: init
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

db-up: init
	$(COMPOSE) up -d --wait db

db-shell:
	$(COMPOSE) exec db psql
