#!/usr/bin/env bash
# Axum route shape oracle (T029): builds the route-shape-axum
# fixture against axum 0.8.8 and the route-shape-axum03 fixture against
# axum 0.3.4 (crates.io, pinned by the Cargo.lock files here), serves each
# on 127.0.0.1 and compares Graphvein's claimed shapes (the fixture goldens)
# with real requests and serde_json output (oracle.py).
#
# Usage: scripts/oracle/axum_shape/run.sh <scratch>
#
# Never run by CI or the default tests. Needs cargo and python3, and, on
# the first build only, crates.io. Writes only under <scratch> (target and
# cargo home); runs only the fixture crates and their pinned dependencies.
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <scratch>" >&2
	exit 2
fi
scratch=$1
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
mkdir -p "$scratch/target" "$scratch/cargo-home"
status=0
for v in 08 03; do
	(
		cd "$here/axum$v"
		CARGO_HOME="$scratch/cargo-home" CARGO_TARGET_DIR="$scratch/target" cargo build --locked --quiet
	)
	golden="$root/testdata/fixtures/route-shape-axum.golden.json"
	version=0.8
	port=54391
	if [ "$v" = 03 ]; then
		golden="$root/testdata/fixtures/route-shape-axum03.golden.json"
		version=0.3
		port=54392
	fi
	python3 -I "$here/oracle.py" "$scratch/target/debug/axum-shape-oracle$v" "$golden" "$version" "$port" > "$scratch/oracle$v.log" 2>&1 || status=1
	tail -n 1 "$scratch/oracle$v.log"
	if grep -q '^FAIL\|^CTXF' "$scratch/oracle$v.log"; then
		grep '^FAIL\|^CTXF' "$scratch/oracle$v.log"
		status=1
	fi
done
exit "$status"
