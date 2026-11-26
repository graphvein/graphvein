#!/usr/bin/env bash
# Opt-in measurement only; never called by scripts/test.sh.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
pin=504c02fc47e76087b82b0e7cbe4186e9cdd916d4
corpus=spike/parsing/.corpus/langflow
if ! head=$(git -C "$corpus" rev-parse HEAD); then
  echo "Cannot verify Langflow HEAD; no measurements run." >&2
  exit 1
fi
if [ "$head" != "$pin" ]; then
  echo "Langflow pin mismatch: expected $pin, got $head; stopping." >&2
  exit 1
fi
if ! status=$(git -C "$corpus" status --porcelain); then
  echo "Cannot verify Langflow clean status; stopping." >&2
  exit 1
fi
if [ -n "$status" ]; then
  echo "Langflow checkout is dirty; stopping without modifying it." >&2
  exit 1
fi
samples=${GRAPHVEIN_PROFILE_SAMPLES:-3}
case "$samples" in ''|*[!0-9]*|0) echo "GRAPHVEIN_PROFILE_SAMPLES must be positive" >&2; exit 1;; esac
out=${GRAPHVEIN_PROFILE_OUT:-${TMPDIR:-/tmp}/graphvein-profile}
bin=$(mktemp -d "${TMPDIR:-/tmp}/graphvein-profile-bin.XXXXXX")
trap 'rm -rf "$bin"' EXIT
native_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s\n' "$1"; fi
}
# Default GC limits, existing dependencies only, no network.
unset GOGC GOMEMLIMIT GRAPHVEIN_POSTGRES_URL
export GOPROXY=off GOSUMDB=off CGO_ENABLED=0
go build -o "$bin/profile.exe" -tags graphveinprofile ./cmd/profile
go build -o "$bin/graphvein.exe" ./cmd/graphvein
go build -o "$bin/replay.exe" scripts/replay-indexer.go
"$bin/profile.exe" "$(native_path "$out")" "$(native_path "$bin/graphvein.exe")" "$(native_path "$bin/replay.exe")" "$samples"
