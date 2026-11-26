#!/usr/bin/env bash
# Vulnerability scanning (ADR 0086). Needs network access: govulncheck
# downloads the Go vulnerability database from vuln.go.dev and npm audit asks
# the npm registry, so scripts/test.sh (offline) does not run it.
#
#   bash scripts/vulncheck.sh go    # govulncheck, binary mode, on bin/graphvein
#   bash scripts/vulncheck.sh web   # npm audit on web/package-lock.json
#   bash scripts/vulncheck.sh       # both
#
# go: builds bin/graphvein first if it is missing, installs the pinned
# govulncheck with the repository's Go toolchain into bin/tools, scans the
# binary and fails on a vulnerable symbol linked into it unless a reviewed,
# unexpired entry in scripts/vulncheck/allowlist.txt covers it. Binary mode,
# because govulncheck's source mode cannot type-check Go 1.27 sources yet.
# web: fails on high or critical advisories in production dependencies and
# reports every other advisory without failing.
# Runs in Git Bash, bash 3.2 (macOS) and CI's Linux job.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

govulncheck_version=v1.10.0
what=${1:-all}

run_go() {
  local exe bin tools toolchain scan
  exe=$(go env GOEXE)
  bin="bin/graphvein$exe"
  if [[ ! -f "$bin" ]]; then
    bash scripts/build.sh
  fi
  tools="$(pwd)/bin/tools"
  mkdir -p "$tools"
  # GOBIN must be absolute for Go; in Git Bash give it the Windows form.
  if command -v cygpath >/dev/null 2>&1; then
    tools=$(cygpath -m "$tools")
  fi
  # The toolchain go.mod selects; without the pin `go install pkg@version`
  # would pick one from golang.org/x/vuln's own go.mod.
  toolchain=$(go env GOVERSION | tr -d '\r')
  GOTOOLCHAIN=$toolchain GOBIN=$tools go install "golang.org/x/vuln/cmd/govulncheck@$govulncheck_version"
  scan="$tools/govulncheck-graphvein.json"
  # With -format json govulncheck exits 0 on findings; the gate decides.
  "$tools/govulncheck$exe" -mode=binary -format json "$bin" > "$scan"
  go run ./scripts/vulncheck -allowlist scripts/vulncheck/allowlist.txt "$scan"
}

run_web() {
  (
    cd web
    printf 'vulncheck: npm audit, production dependencies (fails on high or critical)\n'
    npm audit --omit=dev --audit-level=high
    printf 'vulncheck: npm audit, all dependencies (report only)\n'
    if ! npm audit; then
      printf 'vulncheck: advisories above are reported, not failed: none is high or critical in a production dependency\n'
    fi
  )
}

case "$what" in
  go) run_go ;;
  web) run_web ;;
  all) run_go; run_web ;;
  *)
    printf 'usage: bash scripts/vulncheck.sh [go|web|all]\n' >&2
    exit 2
    ;;
esac
