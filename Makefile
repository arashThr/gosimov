.DEFAULT_GOAL := help
BIN := bin/server
DEV_COMPOSE := docker compose -f compose.dev.yml

## Development

.PHONY: dev
dev: ## Run the app with live reload (air)
	go tool air

.PHONY: generate
generate: ## Generate Go code from templ files
	go tool templ generate

.PHONY: build
build: generate ## Build the server binary into bin/
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/server

.PHONY: run
run: build ## Build and run the server
	./$(BIN)

.PHONY: test
test: generate ## Run tests (set TEST_POSTGRES_URL to also test against Postgres)
	go test ./...

.PHONY: lint
lint: generate ## Run go vet and check formatting
	go vet ./...
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))" || (gofmt -l $$(git ls-files '*.go'); exit 1)

.PHONY: fmt
fmt: ## Format Go and templ files
	gofmt -w $$(git ls-files '*.go')
	go tool templ fmt .

.PHONY: migration
migration: ## Create a migration for both engines: make migration name=add_posts
	@test -n "$(name)" || (echo "usage: make migration name=add_posts"; exit 1)
	@dir=internal/platform/database/migrations; \
	last=$$(ls $$dir/postgres | grep -o '^[0-9]*' | sort -n | tail -1); \
	next=$$(printf "%05d" $$((10#$$last + 1))); \
	for engine in postgres sqlite; do \
		f=$$dir/$$engine/$${next}_$(name).sql; \
		printf -- "-- +goose Up\n\n-- +goose Down\n" > $$f; \
		echo "created $$f"; \
	done

.PHONY: services
services: ## Start local Postgres and Mailpit (http://localhost:8025)
	$(DEV_COMPOSE) up -d

.PHONY: services-down
services-down: ## Stop local services
	$(DEV_COMPOSE) down

## Production (run on the server)

.PHONY: up
up: ## Build and start the production stack (app, Postgres, Caddy)
	docker compose up -d --build

.PHONY: down
down: ## Stop the production stack
	docker compose down

.PHONY: logs
logs: ## Follow application logs
	docker compose logs -f app

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"} /^## / {printf "\n\033[1m%s\033[0m\n", substr($$0, 4)} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
