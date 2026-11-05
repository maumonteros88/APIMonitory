#!/usr/bin/env bash
# One-command deploy: build front + API, sign api.exe and publish it as a
# GitHub Release. The server polls the latest release, verifies the
# signature against the public key it trusts and installs the binary
# (see deploy/iis/pull-release.ps1). This script then waits until
# GET <public-url>/healthz reports the new commit.
# Settings come from deploy/deploy.env (see deploy.env.example).
set -euo pipefail

SIGN_NAMESPACE="apimonitory-deploy"
KEEP_RELEASES=5

usage() {
  cat <<'EOF'
Usage: deploy/deploy.sh [--allow-dirty] [--help]

Builds the frontend and the Windows API (make release-windows), signs
api.exe with your deploy key, publishes it as a GitHub Release
(deploy-<UTC time>-<sha>), keeps the last 5 deploy releases and waits
until <DEPLOY_PUBLIC_URL>/healthz reports the new build.

Needs: gh (authenticated), ssh-keygen (OpenSSH >= 8.1), make, go, pnpm,
and HEAD pushed to origin.

Options:
  --allow-dirty  deploy even if the git working tree has changes
                 (the footer/healthz commit gets a "-dirty" suffix)
  -h, --help     show this help

Config: deploy/deploy.env (copy deploy/deploy.env.example).
EOF
}

die() {
  echo "deploy: $*" >&2
  exit 1
}

step() {
  echo
  echo "==> $*"
}

allow_dirty=0
for arg in "$@"; do
  case "$arg" in
    --allow-dirty) allow_dirty=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown argument: $arg"
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

# --- config ---------------------------------------------------------------
env_file="$script_dir/deploy.env"
[ -f "$env_file" ] ||
  die "missing $env_file (copy deploy/deploy.env.example and edit it)"
# shellcheck disable=SC1090
. "$env_file"

for var in DEPLOY_PUBLIC_URL DEPLOY_SIGNING_KEY; do
  [ -n "${!var-}" ] || die "$var is not set in $env_file"
done
public_url="${DEPLOY_PUBLIC_URL%/}"
timeout="${DEPLOY_TIMEOUT:-300}"
[[ "$timeout" =~ ^[0-9]+$ ]] || die "DEPLOY_TIMEOUT must be seconds"

# A quoted "~" in deploy.env is not expanded by the shell.
signing_key="${DEPLOY_SIGNING_KEY/#\~/$HOME}"

# --- preflight ------------------------------------------------------------
command -v gh >/dev/null ||
  die "gh is not installed (brew install gh, then gh auth login)"
gh auth status >/dev/null 2>&1 ||
  die "gh is not authenticated (run: gh auth login)"
command -v ssh-keygen >/dev/null || die "ssh-keygen not found"
[ -f "$signing_key" ] || die "signing key not found: $signing_key"

head_sha="$(git rev-parse HEAD)"
short_sha="$(git rev-parse --short HEAD)"
commit="$short_sha"
if [ -n "$(git status --porcelain)" ]; then
  if [ "$allow_dirty" -ne 1 ]; then
    git status --short >&2
    die "working tree is dirty; commit/stash or pass --allow-dirty"
  fi
  commit="$short_sha-dirty"
fi

git fetch --quiet origin || die "git fetch origin failed"
on_origin=0
while IFS= read -r ref; do
  case "$ref" in
    refs/remotes/origin/HEAD) ;;
    refs/remotes/origin/?*) on_origin=1 ;;
  esac
done < <(git for-each-ref --contains "$head_sha" \
  --format='%(refname)' refs/remotes/origin)
[ "$on_origin" -eq 1 ] ||
  die "HEAD $short_sha is not on an origin branch: push first"

# Unique id of this release. It is the tag, it is linked into api.exe
# (make BUILD_ID=...) and /healthz reports it, so the final check tells
# this exact build from any other build of the same commit.
tag="deploy-$(date -u +%Y%m%d-%H%M%S)-$short_sha"

echo "Deploying commit $commit as $tag ($public_url)"

# --- 1. build ---------------------------------------------------------------
step "[1/6] Build frontend + API (make release-windows)"
make release-windows BUILD_ID="$tag"
out_dir="$repo_root/build/windows"
exe="$out_dir/api.exe"
[ -s "$exe" ] || die "build did not produce $exe"

# --- 2. sign ----------------------------------------------------------------
step "[2/6] Sign api.exe (namespace $SIGN_NAMESPACE)"
# If the key is loaded in ssh-agent, sign through the agent by pointing
# ssh-keygen at the PUBLIC key: no passphrase prompt. Otherwise sign with
# the private key file (ssh-keygen asks for its passphrase if it has one).
sign_with="$signing_key"
if [ -f "$signing_key.pub" ]; then
  blob="$(cut -d' ' -f2 "$signing_key.pub")"
  if ssh-add -L 2>/dev/null | grep -qF "$blob"; then
    sign_with="$signing_key.pub"
    echo "Using the key held by ssh-agent"
  fi
fi
rm -f "$exe.sig"
ssh-keygen -Y sign -f "$sign_with" -n "$SIGN_NAMESPACE" "$exe"
[ -s "$exe.sig" ] || die "signing did not produce $exe.sig"

# --- 3. version file --------------------------------------------------------
step "[3/6] Write version.txt ($commit $tag)"
# One line, compared as a whole by the server against what it deployed.
# The tag makes every release unique, so re-deploying the same commit (for
# example after a failed install) is a new version for the server.
echo "$commit $tag" >"$out_dir/version.txt"

# --- 4. release -------------------------------------------------------------
step "[4/6] Publish GitHub Release $tag"
gh release create "$tag" \
  --target "$head_sha" \
  --latest \
  --title "$tag" \
  --notes "Deploy of $commit (signed with namespace $SIGN_NAMESPACE)." \
  "$exe" "$exe.sig" "$out_dir/version.txt"

# --- 5. prune ---------------------------------------------------------------
step "[5/6] Keep the last $KEEP_RELEASES deploy releases"
old_tags=""
if ! old_tags="$(gh release list --limit 100 --json tagName,createdAt \
  --jq "[.[] | select(.tagName | startswith(\"deploy-\"))]
        | sort_by(.createdAt) | reverse | .[$KEEP_RELEASES:]
        | .[].tagName")"; then
  echo "deploy: warning: gh release list failed, not pruning" >&2
  old_tags=""
elif [ -z "$old_tags" ]; then
  echo "Nothing to prune."
fi
if [ -n "$old_tags" ]; then
  while IFS= read -r old; do
    [ -n "$old" ] || continue
    if gh release delete "$old" --cleanup-tag --yes; then
      echo "Deleted $old"
    else
      echo "deploy: warning: could not delete $old" >&2
    fi
  done <<<"$old_tags"
fi

# --- 6. wait for the server -------------------------------------------------
step "[6/6] Wait for $public_url/healthz to report build $tag (max ${timeout}s)"
echo "The server checks for a new release every ~2 minutes."
start=$SECONDS
live="(no answer yet)"
live_build="(no answer yet)"
while :; do
  body="$(curl -fsS --max-time 10 "$public_url/healthz" 2>/dev/null || true)"
  if [ -n "$body" ]; then
    live="(none)"
    live_build="(none)"
    if [[ "$body" =~ \"commit\"[[:space:]]*:[[:space:]]*\"([^\"]*)\" ]]; then
      live="${BASH_REMATCH[1]}"
    fi
    if [[ "$body" =~ \"build\"[[:space:]]*:[[:space:]]*\"([^\"]*)\" ]]; then
      live_build="${BASH_REMATCH[1]}"
    fi
  fi
  elapsed=$((SECONDS - start))
  if [ "$live_build" = "$tag" ]; then
    echo "[${elapsed}s] live build: $live_build (commit $live)"
    echo
    echo "Deployed $commit as release $tag."
    exit 0
  fi
  [ "$elapsed" -lt "$timeout" ] || break
  echo "[${elapsed}s] live build: $live_build, waiting for $tag"
  sleep 5
done
echo >&2
die "timed out after ${timeout}s (live build: $live_build). Release $tag is published;" \
  "check C:\\inetpub\\APIMonitory\\autoupdate.log on the server"
