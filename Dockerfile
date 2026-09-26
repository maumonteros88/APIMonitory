# syntax=docker/dockerfile:1

# --- Stage 1: build the SvelteKit frontend into static assets ---
FROM node:22-alpine AS web-build
WORKDIR /repo
RUN corepack enable

# Copy manifests first so `pnpm install` is cached independently of source changes.
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --frozen-lockfile

COPY apps/web apps/web
RUN pnpm -C apps/web build

# --- Stage 2: build the Go binary, embedding the built frontend ---
FROM golang:1.26.4-alpine AS api-build
WORKDIR /repo/apps/api

COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download

COPY apps/api .

# Replace whatever dist/ was committed with a fresh build from stage 1, so
# the image never ships a stale or out-of-sync frontend.
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=web-build /repo/apps/web/build/. ./internal/webui/dist/

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api

# --- Stage 3: minimal runtime image ---
FROM alpine:3.22

# ca-certificates: the app makes outbound HTTPS probes to monitored URLs.
# wget: used by HEALTHCHECK below; there's no OS-level shell dependency otherwise.
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app && adduser -S -G app app

COPY --from=api-build /out/api /api

USER app
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -qO- http://127.0.0.1:${PORT:-8080}/healthz || exit 1

ENTRYPOINT ["/api"]
