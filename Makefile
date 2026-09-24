.DEFAULT_GOAL := help
COMPOSE := docker compose
REDOCLY := npx --yes @redocly/cli@2.54.2

ifeq ($(OS),Windows_NT)
REDOCLY := npx.cmd --yes @redocly/cli@2.54.2
endif

.PHONY: help init run build test vet fmt openapi-lint check up down logs ps db-up db-shell migrate-up migrate-down migrate-version

help:
	@echo "init      - create .env from .env.example if missing"
	@echo "up/down   - start or stop app and PostgreSQL (keep data)"
	@echo "logs/ps   - show container logs or status"
	@echo "db-up     - start PostgreSQL only"
	@echo "db-shell  - open psql inside PostgreSQL"
	@echo "run/build - run locally or build to bin/"
	@echo "test/vet/fmt - check or format Go code"
	@echo "openapi-lint/check - validate OpenAPI or run all checks"
	@echo "migrate-up/version - apply migrations or show version"
	@echo "migrate-down - roll back one migration (deletes its data)"

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

openapi-lint:
	$(REDOCLY) lint api/openapi.yaml

check: test vet openapi-lint

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

migrate-up: init
	$(COMPOSE) run --rm migrate up

migrate-down: init
	$(COMPOSE) run --rm migrate down 1

migrate-version: init
	$(COMPOSE) run --rm migrate version
