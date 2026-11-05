SHELL := /bin/bash

.PHONY: dev-db dev-api dev-web generate test up build-windows release-windows

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

# Cross-compile the API for Windows (IIS deploy, see deploy/iis/README.md)
# into build/windows/api.exe. The frontend is whatever is already committed
# in apps/api/internal/webui/dist. The footer's front/API version and commit
# are inlined into that dist when the frontend is built (see `up` and the
# Dockerfile), not into the Go binary, so there is nothing to inject here:
# they only change when the embedded dist is rebuilt and committed.
build-windows:
	mkdir -p build/windows
	cd apps/api && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -o ../../build/windows/api.exe ./cmd/api

# Release build for Windows: rebuilds the frontend with the real build
# identity (same git computations as `up`, plus "-dirty" when the working
# tree has uncommitted or untracked changes) and cross-compiles a self-contained api.exe into
# build/windows/api.exe. The same commit string is also linked into the
# binary (-X main.commit) and reported by GET /healthz, together with
# BUILD_ID (-X main.build, default "dev"): a unique id per release that
# deploy/deploy.sh passes in (make release-windows BUILD_ID=<tag>).
# -trimpath and -buildvcs=false keep local paths and VCS data out of the
# public exe. Used by deploy/deploy.sh.
#
# The fresh frontend is embedded from a TEMPORARY copy of apps/api under
# build/release/api (git-ignored), never from the committed
# apps/api/internal/webui/dist. The commit hash is inlined into the built
# assets, so writing the build into the committed dist would change its
# files on every release and leave the working tree dirty. apps/web/build
# is git-ignored too, so nothing tracked is touched.
release-windows:
	set -euo pipefail; \
	GIT_COMMIT="$$(git rev-parse --short HEAD)"; \
	if [ -n "$$(git status --porcelain)" ]; then GIT_COMMIT="$$GIT_COMMIT-dirty"; fi; \
	WEB_VERSION="$$(git rev-list --count HEAD -- apps/web)"; \
	API_VERSION="$$(git rev-list --count HEAD -- apps/api)"; \
	echo "release: commit $$GIT_COMMIT, front v$$WEB_VERSION, API v$$API_VERSION"; \
	rm -rf apps/web/build build/release; \
	rm -f build/windows/api.exe; \
	PUBLIC_GIT_COMMIT="$$GIT_COMMIT" \
	PUBLIC_WEB_VERSION="$$WEB_VERSION" \
	PUBLIC_API_VERSION="$$API_VERSION" \
	pnpm -C apps/web build; \
	mkdir -p build/release/api build/windows; \
	tar -C apps/api --exclude=./internal/webui/dist --exclude=./tmp -cf - . \
		| tar -C build/release/api -xf -; \
	mkdir -p build/release/api/internal/webui/dist; \
	cp -R apps/web/build/. build/release/api/internal/webui/dist/; \
	cd build/release/api && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -trimpath -buildvcs=false \
		-ldflags "-X main.commit=$$GIT_COMMIT -X main.build=$(if $(BUILD_ID),$(BUILD_ID),dev)" \
		-o ../../windows/api.exe ./cmd/api; \
	echo "release: built build/windows/api.exe"
