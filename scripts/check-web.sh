#!/usr/bin/env bash
# Checks the web UI (web/) and that the committed build in
# internal/server/ui/static matches a clean build of it (ADR 0041).
# Needs the Node and npm versions pinned in web/.nvmrc and web/package.json.
# Runs in Git Bash, bash 3.2 (macOS) and CI's Linux job.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
root=$(pwd)
static=internal/server/ui/static
cd web

want_node="v$(tr -d '\r\n' < .nvmrc)"
have_node=$(node --version | tr -d '\r')
if [[ "$have_node" != "$want_node" ]]; then
  printf 'check-web: node %s found, web/.nvmrc pins %s\n' "$have_node" "$want_node" >&2
  exit 1
fi
want_npm=$(node -p 'require("./package.json").packageManager.replace(/^npm@/, "")' | tr -d '\r')
have_npm=$(npm --version | tr -d '\r')
if [[ "$have_npm" != "$want_npm" ]]; then
  printf 'check-web: npm %s found, web/package.json pins npm@%s\n' "$have_npm" "$want_npm" >&2
  exit 1
fi

npm ci
npm run licences
npm run typecheck
npm run lint
npm test
# The build also writes third-party-licenses.txt and checks the bundled
# build tools (scripts/third-party-licenses.mjs).
npm run build

cd "$root"
# --ignored: a generated file that an ignore rule hides (shown as "!!")
# would be missing from the commit, so it counts as drift too.
drift=$(git status --porcelain --ignored --untracked-files=all -- "$static")
if [[ -n "$drift" ]]; then
  printf '%s\n' "$drift" >&2
  git diff --stat -- "$static" >&2
  printf 'Committed UI assets differ from a clean build of web/; run bash scripts/check-web.sh and commit %s\n' "$static" >&2
  exit 1
fi
printf 'check-web: ok\n'
