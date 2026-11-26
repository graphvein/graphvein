#!/usr/bin/env bash
# The deploy-config oracle of T026 (ADR 0079). Never run by CI.
#
# Builds scripts/oracle/deploy_oracle/main.go in a scratch Go module at the
# pinned versions (compose-go v2.16.2 and BuildKit v0.34.2, both
# Apache-2.0, from the Go module proxy), then feeds it every compose file
# (row DC-S1 names) and Dockerfile (row DC-F1 names) under ROOT, skipping
# .git and node_modules. The oracle parses only: no Docker daemon, no
# network beyond the module proxy, nothing in the files runs. Compare:
#
#   GRAPHVEIN_DEPLOY_ORACLE=<out.json> GRAPHVEIN_DEPLOY_ORACLE_ROOT=<root> \
#     go test ./internal/extract/deploy -run TestDeployOracle -v
#
# Usage:
#   bash scripts/oracle/deploy_config.sh --scratch DIR --root ROOT --out FILE
#
# Writes only under --scratch and --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'deploy_config: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
scratch="" root="" out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$root" ] && [ -n "$out" ] || die "usage: --scratch DIR --root ROOT --out FILE"
[ -d "$root" ] || die "no directory $root"

mod="$scratch/deploy_oracle"
mkdir -p "$mod"
export GOPATH="$scratch/gopath" GOCACHE="$scratch/gocache" GOFLAGS=-modcacherw
# The build constraint keeps the source out of the graphvein module.
sed '1{/^\/\/go:build ignore$/d;}' "$here/deploy_oracle/main.go" > "$mod/main.go"
if [ ! -f "$mod/go.mod" ]; then
  (cd "$mod" && go mod init deployoracle >/dev/null 2>&1)
fi
(cd "$mod" && go get github.com/compose-spec/compose-go/v2@v2.16.2 github.com/moby/buildkit@v0.34.2 >/dev/null 2>&1 && go mod tidy >/dev/null 2>&1 && go build -o deploy_oracle .) || die "building the oracle failed"

list="$scratch/deploy_files.txt"
(cd "$root" && find . \( -name .git -o -name node_modules \) -prune -o -type f -print) | sed 's|^\./||' | while IFS= read -r p; do
  base=${p##*/}
  case "$base" in
    *.dockerignore) ;;
    compose.yml|compose.yaml|compose.*.yml|compose.*.yaml|docker-compose*.yml|docker-compose*.yaml) printf '%s\n' "$p" ;;
    Dockerfile|Dockerfile.*|*.Dockerfile) printf '%s\n' "$p" ;;
  esac
done | sort > "$list"
"$mod/deploy_oracle" "$root" < "$list" > "$out"
printf 'deploy_config: %s files read into %s\n' "$(wc -l < "$list" | tr -d ' ')" "$out"
