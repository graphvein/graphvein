#!/usr/bin/env bash
# Builds the graphvein binary with its build metadata.
#
# Environment (all optional): VERSION (default dev), BUILD_DATE (default
# the current UTC time), REPORT_URL (default empty), OUTPUT (default
# bin/graphvein, plus .exe for a Windows target). GOOS and GOARCH select a
# cross-build target as usual for go build; GOFLAGS passes extra build flags
# (scripts/release-archives.sh sets -trimpath -buildvcs=false).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
export CGO_ENABLED=0
version=${VERSION:-dev}
commit=$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')
build_date=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
# REPORT_URL is the public wrong-fact issue form (ADR 0089). Empty, the
# default, keeps the UI's "Report a wrong fact" flag off.
report_url=${REPORT_URL:-}
# Reject whitespace/quotes so metadata cannot introduce extra linker arguments.
for value in "$version" "$commit" "$build_date"; do
  if [[ ! "$value" =~ ^[a-zA-Z0-9._:+/-]+$ ]]; then
    printf 'Invalid build metadata: use letters, digits or . _ : + / -\n' >&2
    exit 1
  fi
done
if [[ -n "$report_url" && ! "$report_url" =~ ^https://[a-zA-Z0-9._:+/-]+$ ]]; then
  printf 'Invalid REPORT_URL: an https URL of letters, digits or . _ : + / -\n' >&2
  exit 1
fi
output=${OUTPUT:-bin/graphvein$(go env GOEXE)}
mkdir -p "$(dirname "$output")"
go build -ldflags "-X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date -X main.reportURL=$report_url" -o "$output" ./cmd/graphvein
