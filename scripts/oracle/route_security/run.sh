#!/usr/bin/env bash
# Route security oracle (T029). Opt-in, never run by CI.
#
# 1. TestRouteSecurityExport writes the security claims Graphvein stores for
#    testdata/fixtures/route-shape-security (claims.json).
# 2. The fixture apps are built against the real libraries and served on
#    127.0.0.1: the Express app with the npm versions pinned in its
#    package.json (installed with --ignore-scripts), the gin and chi routers
#    with the Go module versions pinned in go/go.sum, the Axum router with
#    the crates pinned in axum/Cargo.lock.
# 3. probe.py sends requests with and without each claimed credential form,
#    and checks that the paths oracle.json lists as served without any
#    credential answer 200 and carry no fact requirement.
#
# Usage: bash scripts/oracle/route_security/run.sh SCRATCH
# Writes only under SCRATCH; every server is stopped by its process group.
# Bash 3.2 compatible. Needs go, cargo, node/npm and python3.
set -euo pipefail

die() { printf 'route_security oracle: %s\n' "$*" >&2; exit 1; }

[ $# -eq 1 ] || die "usage: run.sh SCRATCH"
mkdir -p "$1"
scratch=$(cd "$1" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
fixture="$repo/testdata/fixtures/route-shape-security"
pids=""
cleanup() {
	for p in $pids; do
		kill -TERM -- "-$p" 2>/dev/null || true
	done
}
trap cleanup EXIT

(cd "$repo" && GRAPHVEIN_ROUTE_SECURITY_OUT="$scratch/claims.json" go test ./internal/index -run '^TestRouteSecurityExport$' -count=1 >/dev/null)

export GOPATH="$scratch/gopath" GOMODCACHE="$scratch/gopath/pkg/mod" CARGO_HOME="$scratch/cargo-home" npm_config_cache="$scratch/npm-cache"
rm -rf "$scratch/express"
cp -R "$fixture/express" "$scratch/express"
cp "$here/express/oracle-server.js" "$scratch/express/oracle-server.js"
(cd "$scratch/express" && npm install --ignore-scripts --no-audit --no-fund --silent >/dev/null)
(cd "$here/go" && go build -o "$scratch/goserver" .)
(cd "$here/axum" && CARGO_TARGET_DIR="$scratch/target" cargo build --locked --quiet 2>/dev/null)

# Ports: five from ROUTE_SECURITY_PORT_BASE (default 54401). A port that
# already answers belongs to another process: stop rather than probe it.
base=${ROUTE_SECURITY_PORT_BASE:-54401}
p1=$base p2=$((base + 1)) p3=$((base + 2)) p4=$((base + 3)) p5=$((base + 4))
for port in $p1 $p2 $p3 $p4 $p5; do
	if python3 -I -c "import socket,sys; s=socket.socket(); s.settimeout(1); sys.exit(s.connect_ex(('127.0.0.1', $port)) != 0)" 2>/dev/null; then
		die "port $port is already in use (set ROUTE_SECURITY_PORT_BASE)"
	fi
done

start() {
	setsid "$@" >>"$scratch/servers.log" 2>&1 &
	pids="$pids $!"
}
: >"$scratch/servers.log"
# sh is the session leader setsid starts and execs node, so the pid is the
# process group the cleanup stops.
start sh -c 'cd "$1" && PORT="$2" exec node oracle-server.js' sh "$scratch/express" "$p1"
start "$scratch/goserver" $p2 $p3 $p4
start "$scratch/target/debug/route-security-oracle" $p5

for port in $p1 $p2 $p3 $p4 $p5; do
	i=0
	until python3 -I -c "import socket,sys; s=socket.socket(); s.settimeout(1); sys.exit(s.connect_ex(('127.0.0.1', $port)))" 2>/dev/null; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || die "port $port did not open (see $scratch/servers.log)"
		sleep 1
	done
done

python3 -I "$here/probe.py" "$scratch/claims.json" "$here/oracle.json" "$scratch/servers.log" express=$p1 gin=$p2 chi=$p3 served=$p4 axum=$p5 | tee "$scratch/probe.log"
