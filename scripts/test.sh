#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# A clean fixture tree may contain no ignored inputs, even if force-added.
bash scripts/check-fixture-ignores.sh
bash scripts/test-t005a.sh
bash scripts/test-ci-tested-tree.sh
bash scripts/test-export-public.sh
bash scripts/benchmark/test-run.sh
export CGO_ENABLED=0
go vet ./...
go vet -tags graphveincorpus ./...
go test ./... "$@"
go vet -tags graphveinprofile ./...
go test -tags graphveinprofile ./cmd/profile ./internal/store/sqlstore "$@"
# The website generator is its own module (site/, ADR 0091).
(cd site && go vet ./... && go test ./... "$@")
# After go test, so the host build reuses the compiled packages.
bash scripts/test-release.sh
