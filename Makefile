SHELL := /bin/bash

.PHONY: dev-db dev-api dev-web generate test up

# Start only the Postgres service from docker-compose, for local (non-Docker)
# development of the API with `go run`.
dev-db:
	docker compose up -d db

# Run the API with `go run` against the dev-db Postgres instance.
# Reads DATABASE_URL/API_TOKEN from the environment, falling back to .env.
# Variables already exported in the shell take precedence over .env: we
# stash them before sourcing and restore them after, since `source` would
# otherwise silently overwrite an exported value with an empty or stale
# one from .env.
dev-api:
	set -eu; \
	_API_TOKEN="$${API_TOKEN-}"; _POSTGRES_PASSWORD="$${POSTGRES_PASSWORD-}"; _DATABASE_URL="$${DATABASE_URL-}"; \
	set -a; [ -f .env ] && source .env; set +a; \
	[ -n "$$_API_TOKEN" ] && API_TOKEN="$$_API_TOKEN"; \
	[ -n "$$_POSTGRES_PASSWORD" ] && POSTGRES_PASSWORD="$$_POSTGRES_PASSWORD"; \
	[ -n "$$_DATABASE_URL" ] && DATABASE_URL="$$_DATABASE_URL"; \
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
# Each app's "version" is just how many commits have touched its folder --
# it goes up on its own, with nothing to remember to bump.
up:
	GIT_COMMIT="$$(git rev-parse --short HEAD)" \
	WEB_VERSION="$$(git rev-list --count HEAD -- apps/web)" \
	API_VERSION="$$(git rev-list --count HEAD -- apps/api)" \
	docker compose up --build
