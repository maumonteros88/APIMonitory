# APIMonitory

APIMonitory is an API health monitor for GOIAR: it periodically probes a list of
HTTP endpoints and tracks their uptime, latency, and failure history. The Go
backend serves both the JSON API and the built frontend from a single binary.

## Quickstart (Docker)

```bash
cp .env.example .env
# edit .env and fill in POSTGRES_PASSWORD and API_TOKEN

make up
```

Open `http://localhost:8080` and paste the token you set as `API_TOKEN` when
prompted.

## Local development (no Docker for the app, Docker only for Postgres)

Run each of these in its own terminal:

```bash
make dev-db   # starts Postgres in Docker
make dev-api  # go run ./cmd/api, against dev-db
make dev-web  # Vite dev server, proxies /api to :8080
```

`dev-api` reads `DATABASE_URL`/`API_TOKEN` from `.env` (or the environment) the
same way the Docker image does.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | — | Postgres connection string, e.g. `postgres://user:pass@host:5432/db?sslmode=disable` |
| `API_TOKEN` | yes | — | Bearer token required by every `/api/*` route except `/healthz` |
| `PORT` | no | `8080` | HTTP port the server listens on |
| `ASPNETCORE_PORT` | no | — | Set by IIS (ASP.NET Core Module). When present, the server listens on `127.0.0.1:<port>` and `PORT` is ignored |
| `CHECK_INTERVAL` | no | `60s` | How often each monitor is probed |
| `CHECK_TIMEOUT` | no | `5s` | Per-probe HTTP timeout |
| `RETRY_DELAY` | no | `10s` | Delay before retrying a failed probe |
| `FAILURE_THRESHOLD` | no | `2` | Consecutive failures before a monitor is marked down |
| `DEGRADED_LATENCY` | no | `2s` | Latency above which a healthy response is marked degraded |
| `MAX_CONCURRENCY` | no | `20` | Max number of probes run concurrently |
| `APP_TIMEZONE` | no | `America/Argentina/Buenos_Aires` | Timezone used for daily stats bucketing |

## Tests

```bash
make test
```

Runs the Go test suite (`apps/api`) and the frontend checks/tests
(`apps/web`); fails if either fails.

## Deploy

Production runs on Windows Server / IIS through the ASP.NET Core Module
(out of process). Once the server is set up, a deploy is:

```bash
git push             # deploy.sh only deploys pushed commits
./deploy/deploy.sh   # build, sign, publish a GitHub Release, wait
```

The server installs signed releases on its own within about 2 minutes,
and `GET /healthz` reports the live `commit` and `build`. Server setup,
rollback and details: [deploy/iis/README.md](deploy/iis/README.md).

## Architecture

The Go backend (`apps/api`) serves the JSON API under `/api/*` and the
SvelteKit frontend (`apps/web`) as a statically-built SPA embedded into the
same binary via `go:embed`. The frontend is built with `adapter-static` and
copied into `apps/api/internal/webui/dist` at build time; there's no separate
frontend server or build-time API URL to configure — everything runs behind
one HTTP server and one port.
