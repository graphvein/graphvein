#!/usr/bin/env bash
# The javac declaration oracle of T021 (docs/lang/best-effort.md "Oracle").
# Never run by CI; opt-in like the Postgres oracles.
#
# 1. TestJavaDeclsOracle (phase index) indexes ROOT and writes, under
#    --scratch, the scanned .java files, the refused (BE-2) files and every
#    emitted type, method and constructor with its owner path and span.
# 2. JavaDecls.java, compiled into --scratch, lists the same declarations
#    from javac's parser (JavacTask.parse() only: no attribution, no
#    annotation processing; nothing of ROOT runs).
# 3. TestJavaDeclsOracle (phase compare) compares kind, owner path, start
#    and end outside the refused files; any disagreement fails it.
#    Overload and repeated-name suffixes and import resolution are not part
#    of the comparison.
#
# Usage:
#   bash scripts/oracle/java_decls.sh --scratch DIR --root ROOT [--out FILE]
#
#   --root ROOT  the repository to index (a fixture or a corpus checkout;
#                nothing is written into it)
#   --out FILE   the comparison's counts as JSON
#
# Writes only under --scratch and --out. Needs go and a JDK 21 or later
# (javac, java). Bash 3.2 compatible.
set -euo pipefail

die() { printf 'java_decls: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch="" root="" out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$root" ] || die "--scratch and --root are required"
command -v javac >/dev/null || die "javac not found"
command -v java >/dev/null || die "java not found"
command -v go >/dev/null || die "go not found"
root=$(cd "$root" && pwd)
mkdir -p "$scratch/classes"
scratch=$(cd "$scratch" && pwd)
if [ -n "$out" ]; then
  out="$(cd "$(dirname "$out")" && pwd)/$(basename "$out")"
fi

javac -d "$scratch/classes" "$here/JavaDecls.java"

cd "$repo_root"
GRAPHVEIN_JAVA_ORACLE_PHASE=index GRAPHVEIN_JAVA_ORACLE_ROOT="$root" GRAPHVEIN_JAVA_ORACLE_SCRATCH="$scratch" \
  go test -count=1 -run '^TestJavaDeclsOracle$' ./internal/index >"$scratch/index.log" 2>&1 \
  || { cat "$scratch/index.log" >&2; die "indexing failed"; }

java -cp "$scratch/classes" JavaDecls "$root" "$scratch/files.txt" >"$scratch/javac.tsv"

GRAPHVEIN_JAVA_ORACLE_PHASE=compare GRAPHVEIN_JAVA_ORACLE_SCRATCH="$scratch" GRAPHVEIN_JAVA_ORACLE_OUT="$out" \
  go test -count=1 -v -run '^TestJavaDeclsOracle$' ./internal/index
