#!/usr/bin/env bash
# Rust structure oracle (T020 S4): builds the syn declaration oracle and the
# Go comparator in a scratch directory, then compares a Graphvein snapshot of
# a checkout with syn (declarations), cargo metadata (targets) and rustc's
# resolver (-Zunpretty=hir-tree; module tree and `use` targets).
#
# Usage: scripts/oracle/rust_structure/run.sh <checkout> <snapshot.ndjson> <name> <scratch>
#
# Never run by CI or the default tests. Needs a Rust toolchain (pinned:
# 1.97.0; RUSTC_BOOTSTRAP=1 enables the unstable -Z flag on stable) and, on
# the first build only, crates.io for the pinned syn, proc-macro2, quote and
# unicode-ident (Cargo.lock). Writes only under <scratch>; reads the checkout.
# It never runs build scripts, proc macros or any corpus code: every
# dependency of the checkout is replaced by an empty stub crate.
set -euo pipefail

if [ "$#" -ne 4 ]; then
	echo "usage: $0 <checkout> <snapshot.ndjson> <name> <scratch>" >&2
	exit 2
fi
checkout=$(cd "$1" && pwd)
snapshot=$2
name=$3
scratch=$4
here=$(cd "$(dirname "$0")" && pwd)
toolchain=${RUST_ORACLE_TOOLCHAIN:-$HOME/.rustup/toolchains/1.97.0-x86_64-unknown-linux-gnu/bin}
if [ ! -x "$toolchain/rustc" ]; then
	toolchain=$HOME/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin
fi
version=$("$toolchain/rustc" --version)
case "$version" in
"rustc 1.97.0 "*) ;;
*) echo "warning: oracle pinned to rustc 1.97.0, found $version (hir-tree output is unstable)" >&2 ;;
esac

mkdir -p "$scratch/target" "$scratch/cargo-home" "$scratch/work/$name"
(
	cd "$here"
	CARGO_HOME="$scratch/cargo-home" CARGO_TARGET_DIR="$scratch/target" PATH="$toolchain:$PATH" \
		"$toolchain/cargo" build --release --locked --quiet
)
go build -o "$scratch/compare" "$here/compare.go"

work="$scratch/work/$name"
rm -rf "$work"
mkdir -p "$work/meta"
cmp() { "$scratch/compare" -root "$checkout" -snapshot "$snapshot" -corpus "$name" -work "$work" "$@"; }

# 1. Declarations: syn over the raw bytes of the snapshot's Rust files.
cmp -phase files > "$work/files.txt"
"$scratch/target/release/rust-structure-oracle" "$checkout" < "$work/files.txt" > "$work/decls.jsonl"

# 2. Targets: cargo metadata per manifest, from scratch (no corpus
#    .cargo/config is read), offline, without dependencies; it runs nothing.
n=0
list_manifests() {
	if git -C "$checkout" rev-parse --is-inside-work-tree > /dev/null 2>&1; then
		git -C "$checkout" ls-files -co --exclude-standard
	else
		# A copy outside Git (the test's fixture copy): every manifest.
		(cd "$checkout" && find . -name Cargo.toml -not -path '*/target/*' | sed 's#^\./##')
	fi
}
list_manifests | grep -E '(^|/)Cargo\.toml$' | sort | while IFS= read -r manifest; do
	n=$((n + 1))
	printf '%s\n' "$manifest" > "$work/meta/$n.path"
	(cd "$scratch" && CARGO_HOME="$scratch/cargo-home" "$toolchain/cargo" metadata --no-deps --offline \
		--format-version 1 --manifest-path "$checkout/$manifest" > "$work/meta/$n.json" 2> "$work/meta/$n.err") || true
done

# 3. Module tree and use resolution: rustc -Zunpretty=hir-tree per target
#    and cfg configuration (compare.go: configs; the windows and macos
#    targets need the pinned toolchain's rust-std components
#    x86_64-pc-windows-msvc and x86_64-apple-darwin), every dependency an
#    empty stub crate.
cmp -phase plan
RUSTC="$toolchain/rustc" bash "$work/plan.sh"

# 4. Compare.
# Graphvein's crate roots (written by TestRustStructureOracle) are compared
# with cargo metadata's targets when present.
roots=()
if [ -f "$scratch/$name.roots.json" ]; then
	roots=(-roots "$scratch/$name.roots.json")
fi
cmp -phase compare -out "$scratch/$name.jsonl" ${roots[@]+"${roots[@]}"}
rm -rf "$work/hir"
