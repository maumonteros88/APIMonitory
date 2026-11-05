# APIMonitory

Go API (`apps/api`) plus a SvelteKit SPA (`apps/web`) embedded in the same
binary, backed by Postgres. Dev setup and env vars: `README.md`.

## Commands

| Command | What it does |
|---|---|
| `make test` | Go tests plus web checks/tests. Run it before committing. |
| `make dev-db` / `make dev-api` / `make dev-web` | Local development |
| `./deploy/deploy.sh` | Production deploy (below) |

## Deploying to production

Production runs on Windows Server / IIS (ASP.NET Core Module, out of
process). Deploys are pull-based: nothing connects into the server.

1. Commit and push to `origin`. `deploy.sh` refuses a dirty tree or a
   HEAD that is not on an origin branch.
2. `./deploy/deploy.sh`: builds the front and the API, signs `api.exe`,
   publishes the GitHub Release `deploy-<UTC time>-<sha>` and waits until
   `<public url>/healthz` reports that build.
3. Done when it prints `Deployed <commit> as release <tag>` (2 to 4 min).
   The server's scheduled task installs it and rolls back on failure.

Rules:

- Run `deploy.sh` only when the user asks: it publishes a release and the
  server installs it within about 2 minutes.
- Signing uses the key held by ssh-agent. If it is not loaded (for example
  after a reboot) `ssh-keygen` prompts for the passphrase, which needs a
  TTY: ask the user to run `ssh-add --apple-use-keychain
  ~/.ssh/apimonitory_deploy` first, or to run `deploy.sh` themselves.
- Do not rebuild or commit `apps/api/internal/webui/dist` just to deploy:
  `make release-windows` builds the frontend into a temporary copy.
- DB migrations (goose, embedded) run when `api.exe` starts.
- `deploy.sh` ships only `api.exe`. Pool env vars, `web.config` and the
  scripts in `deploy/iis` need manual steps on the server: see
  "Changing server-side files" in `deploy/iis/README.md`.
- Rollback: deploy an older pushed commit
  (`git switch --detach <sha> && ./deploy/deploy.sh; git switch -`).
- If step 6 times out, the server's `autoupdate.log` says whether the
  signature, the download or the install failed.

## Public repo

This repository is public. Never commit secrets, `deploy/deploy.env`, the
server's IP, its public URL or notes about how the server is accessed.
Use placeholders (TEST-NET addresses such as `203.0.113.10`).
