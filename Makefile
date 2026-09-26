SHELL := /bin/bash

.PHONY: dev-db dev-api dev-web generate test up

# Start only the Postgres service from docker-compose, for local (non-Docker)
# development of the API with `go run`.
dev-db:
	docker compose up -d db

# Run the API with `go run` against the dev-db Postgres instance.
# Reads DATABASE_URL/API_TOKEN from the environment, falling back to .env.
dev-api:
	set -eu; \
	set -a; [ -f .env ] && source .env; set +a; \
	: "$${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env or the environment}"; \
	: "$${API_TOKEN:?set API_TOKEN in .env or the environment}"; \
	export DATABASE_URL="$${DATABASE_URL:-postgres://$${POSTGRES_USER:-apimonitory}:$${POSTGRES_PASSWORD}@localhost:$${POSTGRES_PORT:-5433}/$${POSTGRES_DB:-apimonitory}?sslmode=disable}"; \
	export API_TOKEN; \
	cd apps/api && go run ./cmd/api

# Run the SvelteKit dev server (Vite proxies /api to :8080).
dev-web:
	pnpm -C apps/web dev

# Regenerate Go code from SQL queries via sqlc.
generate:
	cd apps/api && sqlc generate

# Run the Go test suite and the frontend checks/tests. Fails if any step fails.
test:
	cd apps/api && go test ./...
	pnpm -C apps/web check
	pnpm -C apps/web test

# Build and start the full stack (Postgres + API with the embedded frontend).
up:
	docker compose up --build
