# Deploying APIMonitory on IIS

APIMonitory runs on Windows Server 2022 / IIS 10 behind the ASP.NET Core
Module V2 (ANCM) in **OutOfProcess** mode: IIS terminates HTTP(S), and ANCM
launches `api.exe`, picks a port, passes it in `ASPNETCORE_PORT` and
reverse-proxies to it. The API listens on `127.0.0.1:<that port>` only when
`ASPNETCORE_PORT` is set (otherwise on `:PORT`, default `:8080`). No ARR or
HttpPlatformHandler is needed, and nothing is installed into IIS.

The server is shared with other sites, so every script here only touches
its own app pool, site and scheduled task. None of them runs `iisreset` or
edits server-level config.

| File | Purpose |
|---|---|
| `web.config` | ANCM handler (OutOfProcess), 12 h request timeout, no dynamic compression (site-wide, so SSE isn't buffered) |
| `setup-iis.ps1` | One-time, idempotent: app pool, site, HTTP binding, pool env vars, ACLs, keep-alive task |
| `update.ps1` | Swaps `api.exe` (with a `.bak`) behind `app_offline.htm`, then warms the site up |
| `keepalive.ps1` | Requests `/healthz` with the site's Host header so ANCM (re)launches `api.exe` |
| `pull-release.ps1`, `setup-autoupdate.ps1` | Pull-based signed auto-update from GitHub Releases (section 8) |

## 1. Build

From the repo root (needs Go only, no Docker):

```bash
make build-windows   # -> build/windows/api.exe
```

The frontend is embedded from the committed
`apps/api/internal/webui/dist`. The footer ("Front vX · API vY · commit Z")
is inlined into that dist when the frontend is built (see `make up` / the
Dockerfile), not into the Go binary. To refresh it, rebuild and commit the
embedded dist first; `make build-windows` does not do that.
`make release-windows` (used by `deploy/deploy.sh`, section 8) does
rebuild the frontend with the real footer, from a temporary copy that
leaves the committed dist alone.

## 2. Copy to the server

Copy `build/windows/api.exe` and the four files in `deploy/iis/` to the
server. Default layout (outside `wwwroot` on purpose: a folder nested under
another site's root could map our `web.config` into that site and launch a
second `api.exe`, i.e. a second scheduler):

```text
C:\inetpub\APIMonitory\
    keepalive.ps1        (copied here by setup-iis.ps1, not served)
    web\                 (site root)
        api.exe
        web.config
        logs\            (created by setup-iis.ps1)
```

Put `api.exe` and `web.config` in `web\`. Keep the scripts in any folder
you like; `setup-iis.ps1` and `update.ps1` expect `keepalive.ps1` next to
them.

Do not copy another site's `web.config`: those are InProcess (.NET) and
produce HTTP 500.30 with a Go binary.

## 3. Run the setup (once)

In an elevated PowerShell:

```powershell
.\setup-iis.ps1 -HostName apimonitor.example.com
```

Optional: `-SiteName`, `-PhysicalPath`, `-AppTimezone` (only needed to
override the app's own default, `America/Argentina/Buenos_Aires`).

Before changing anything, it aborts if: the site already exists with a
different physical path, the pool is used by another site, or another site
already owns the `*:80:<host>` binding. A typo in `-SiteName` therefore
cannot modify someone else's site or pool.

It asks for `DATABASE_URL` and `API_TOKEN` (hidden input) and stores them,
plus `APP_TIMEZONE` if given, as **app pool environment variables** in
`applicationHost.config`, never in the site folder or in `web.config`.
Re-running the script is safe: it updates in place and keeps other
bindings. It recycles only this pool.

It also:

- sets the pool to No Managed Code, `AlwaysRunning`, no idle timeout, no
  periodic recycle, a single worker process (`maxProcesses = 1`) and **no
  overlapped recycle** (an old `api.exe` never drains while SSE streams are
  open, so two schedulers would run at once);
- enables preload on the site;
- grants the pool identity read+execute on the site folder and modify on
  `logs\`;
- registers the **keep-alive scheduled task** (below).

### Keep-alive task

ANCM only relaunches `api.exe` on the next request: after a crash, an
update, a pool recycle or a `web.config` edit, nothing runs until someone
hits the site, and the scheduler (the actual monitoring) stays dead.
`setup-iis.ps1` registers the task `<SiteName> keep-alive` (default
`APIMonitory keep-alive`), running as SYSTEM every 2 minutes, indefinitely:

```text
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass
  -File C:\inetpub\APIMonitory\keepalive.ps1 -HostName <host> -TimeoutSeconds 30
```

It requests `http://127.0.0.1/healthz` with the site's Host header, which
needs no token and is served by `api.exe` through ANCM. Check it with
`Get-ScheduledTaskInfo -TaskName "APIMonitory keep-alive"` (`LastTaskResult`
0 means OK). To remove it: `Unregister-ScheduledTask -TaskName "APIMonitory keep-alive" -Confirm:$false`.

## 4. DNS and HTTPS

1. Create the DNS A record for the host name pointing to the server.
2. Issue the certificate with `wacs.exe`, using the same IIS flow already
   used for the other sites on this server (pick this site, let win-acme
   create the HTTPS binding and the renewal task).

`setup-iis.ps1` deliberately creates only the HTTP binding.

## 5. First-run checks

- `http://<host>/healthz` returns 200 (no token needed). If it returns 502.5
  or 500.x, read `logs\stdout_*.log` in the site folder: a missing or
  invalid `DATABASE_URL` / `API_TOKEN` shows up there, and migrations run at
  startup.
- Open the site over HTTPS and enter the token.
- SSE through ANCM: with the dashboard open, trigger a check (or wait for
  the interval) and confirm the UI updates live without a reload. From a
  shell: `curl.exe -N -H "Authorization: Bearer <token>" https://<host>/api/events`
  should print heartbeats about every 25 s and events as they happen.
- Stopping or recycling the pool: ANCM asks `api.exe` to shut down and may
  hard-kill it after `shutdownTimeLimit` (20 s in `web.config`; the app's
  own graceful shutdown waits up to 10 s), so it may not exit cleanly. Open
  SSE clients reconnect on their own.

## 6. Update

Build, copy the new `api.exe` to the server (any folder), then:

```powershell
.\update.ps1 -SourceExe C:\temp\api.exe -HostName apimonitor.example.com
```

It requires `api.exe` and `web.config` to already be in the site folder,
drops `app_offline.htm` (ANCM stops `api.exe`), waits until the exe is
unlocked (60 s timeout), saves the current exe as `api.exe.bak`, swaps it,
removes `app_offline.htm`, and runs `keepalive.ps1` so ANCM launches the new
`api.exe` right away (it fails loudly if the site does not answer). If the
swap fails, the old exe stays in place. To roll back, copy `api.exe.bak`
to another path first and pass that copy as `-SourceExe` (the script
overwrites `api.exe.bak` with the current exe before copying the
source; see section 8).

If `update.ps1` warns that it could not remove `app_offline.htm`, the site
stays offline until you delete that file by hand.

A `web.config` edit is picked up by IIS on save, but `api.exe` only
restarts on the next request: run `keepalive.ps1 -HostName <host>` after
editing. To change an environment variable, re-run `setup-iis.ps1`.

## 7. Operations notes

- **stdout logs**: ANCM writes one `logs\stdout_*.log` per process start,
  with no rotation. The volume is small (the scheduler only logs warnings),
  so this is fine for now. Delete old files now and then, or set
  `stdoutLogEnabled="false"` in `web.config` once things are stable.
- **Secrets**: they live as pool env vars in
  `C:\Windows\System32\inetsrv\config\applicationHost.config`, not in the
  site folder. Check who can read that file:

  ```powershell
  icacls C:\Windows\System32\inetsrv\config\applicationHost.config
  ```

  If `IIS_IUSRS` or `Users` can read it, the secrets are readable by other
  app pools on this shared server. Treat them as exposed in that case.
- Don't copy other sites' `web.config` files, and don't put anything of
  this site under another site's root.

## 8. Automated deploy via signed GitHub Releases

`deploy/deploy.sh` builds the frontend and the API on your Mac, signs
`api.exe` with your deploy key and publishes it as a GitHub Release. The
server **pulls**: a scheduled task checks the latest release every 2
minutes and installs it only if the signature verifies. Nothing connects
into the server (no SSH, no open ports), and the repo is public so the
server needs no token. It assumes the site is already set up (sections 3
to 4) and that `C:\inetpub\APIMonitory\deploy` holds `update.ps1` and
`keepalive.ps1`.

| File | Purpose |
|---|---|
| `pull-release.ps1` | Run by the task: fetch `version.txt`, download and verify `api.exe`, run `update.ps1` |
| `setup-autoupdate.ps1` | One-time, idempotent: trusted key, folders, ACLs, scheduled task |

### One-time setup on the Mac

```bash
brew install gh
gh auth login                       # needs permission to create releases
cp deploy/deploy.env.example deploy/deploy.env   # git-ignored; edit it
ssh-add --apple-use-keychain ~/.ssh/apimonitory_deploy   # load the key into ssh-agent
```

`deploy.env` holds `DEPLOY_PUBLIC_URL`, `DEPLOY_SIGNING_KEY` (default
`~/.ssh/apimonitory_deploy`) and optionally `DEPLOY_TIMEOUT` (seconds,
default 300). If the key is loaded in ssh-agent it signs through the
agent with no passphrase prompt.

Copy the **public** key to the clipboard:

```bash
pbcopy < ~/.ssh/apimonitory_deploy.pub
```

### One-time setup on the server

Copy `setup-autoupdate.ps1` and `pull-release.ps1` to the server (same
folder), then in an **elevated Windows PowerShell 5.1**:

```powershell
.\setup-autoupdate.ps1 -PublicKey (Get-Clipboard)
```

Optional: `-Repo` (default `maumonteros88/APIMonitory`), `-HostName`,
`-Root`, `-SshKeygen` (default `C:\Program Files\OpenSSH\ssh-keygen.exe`).
It refuses to continue if `ssh-keygen` is missing or older than OpenSSH
8.1 (`-Y verify` needs it). It then:

- writes `<root>\allowed_signers` with only your key, valid only for the
  namespace `apimonitory-deploy`;
- copies `pull-release.ps1` to `<root>` and creates `<root>\state` and
  `<root>\incoming`;
- locks `allowed_signers`, `pull-release.ps1`, `state`, `incoming`,
  `<root>\deploy` and `<root>\keepalive.ps1` (recursively) to SYSTEM +
  Administrators with inheritance removed: SYSTEM runs all of them, so
  nobody else may change what it executes or trusts. `<root>` and
  `<root>\web` are left alone (the app pool needs them); the script only
  warns if other accounts can write to `<root>`;
- registers the task `APIMonitory auto-update` (SYSTEM, every 2 min,
  no overlap, 10 min limit), replacing it if it exists.

### Deploying

```bash
./deploy/deploy.sh                # --allow-dirty to skip the clean-tree check
```

Needs `gh` authenticated and `HEAD` pushed to `origin`. It stops at the
first failure:

1. `make release-windows`: front with the real footer (front/API
   version and commit, `-dirty` if the tree is not clean) and
   `build/windows/api.exe`, with the commit and a unique build id (the
   release tag, `make release-windows BUILD_ID=<tag>`) linked in, so
   `GET /healthz` returns
   `{"status":"ok","commit":"<commit>","build":"<tag>"}`. The exe is built
   with `-trimpath -buildvcs=false`, so it carries no local paths. The
   fresh frontend is embedded from a temporary copy under
   `build/release/`; the committed `apps/api/internal/webui/dist` is
   never touched.
2. Signs `api.exe` (`ssh-keygen -Y sign -n apimonitory-deploy`).
3. Writes `version.txt` (`<commit> <tag>`).
4. `gh release create deploy-<UTC time>-<sha>` with `api.exe`,
   `api.exe.sig` and `version.txt`, marked latest.
5. Deletes `deploy-*` releases (and tags) beyond the last 5.
6. Polls `<DEPLOY_PUBLIC_URL>/healthz` until `build` equals the new tag
   (so a redeploy of the same commit is told apart), printing progress. Expect up to about 2 to 3 minutes (task interval plus
   warm-up).

On the server, per run the task reads
`https://github.com/<repo>/releases/latest/download/version.txt` (a plain
download, no API rate limit). If that version is already in
`<root>\state\deployed.txt` or `failed.txt` it exits silently. Otherwise
it downloads `api.exe` and `api.exe.sig` from that same release, verifies
the signature, runs `deploy\update.ps1` (offline page, swap, `api.exe.bak`,
warm-up) and records the version in `deployed.txt`. A bad signature or a
failed update records it in `failed.txt`, so it is not retried in a loop;
publishing a new release (or deleting `failed.txt`) retries.

**Auto-rollback**: `update.ps1` swaps the exe first and warms the site up
after. If it fails, the task compares the SHA256 of `web\api.exe` with the
verified download. Equal means the broken build is live, so it runs
`update.ps1` again with `web\api.exe.bak` (copied aside) and logs
`ROLLED BACK to previous exe` (or `ROLLBACK FAILED` plus manual steps).
Different means the failure came before the swap and nothing is touched.
The version goes to `failed.txt` either way.

### Watching it

```powershell
Get-Content C:\inetpub\APIMonitory\autoupdate.log -Tail 30 -Wait
Get-ScheduledTaskInfo -TaskName "APIMonitory auto-update"
Start-ScheduledTask   -TaskName "APIMonitory auto-update"   # run now
```

The log keeps about the last 500 lines. If `deploy.sh` times out, read it
first: it says whether the signature was rejected, the download failed or
`update.ps1` failed.

### Rollback

- **Automatic**: a failed install already rolls back (see above).
- **Preferred, to go back deliberately**: deploy an older pushed commit. It becomes a new release,
  so the server installs it like any other:

  ```bash
  git switch --detach <old-sha> && ./deploy/deploy.sh; git switch -
  ```

- **Manual, on the server**: `update.ps1` overwrites `api.exe.bak` with
  the current exe before it copies the source, so do not pass
  `api.exe.bak` itself as `-SourceExe`. Copy it aside first:

  ```powershell
  $root = 'C:\inetpub\APIMonitory'
  Copy-Item "$root\web\api.exe.bak" "$root\incoming\rollback.exe"
  $src = "$root\incoming\rollback.exe"
  & "$root\deploy\update.ps1" -SourceExe $src -HostName apimonitory.dev.goiar.com
  ```

  The task does not know about a manual rollback: the next release (or
  the one already latest, if you delete `state\deployed.txt`) installs
  again.

### Trust model

Only binaries signed by the Mac key are installed. `allowed_signers`
trusts that single key, scoped to the `apimonitory-deploy` namespace, so
a compromised GitHub account or release cannot make the server run an
unsigned or differently signed exe. Limits: the signature covers
`api.exe`, not `version.txt`, so someone who controls the repo's releases
could replay an older signed build (a downgrade) or withhold updates; and
whoever holds the private key can deploy. Keep the key passphrase
protected and in the agent, and rotate it by re-running
`setup-autoupdate.ps1 -PublicKey ...` with the new public key.
