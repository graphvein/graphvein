#!/usr/bin/env bash
# The GORM oracle of T018g5 (ADR 0063). Never run by CI.
#
# It builds scratch Go modules from scripts/oracle/gorm_oracle/ at the
# pinned GORM versions, copies the model packages each run needs into them
# with gorm_oracle/copytypes.go (types, constants, literal variables and
# GORM's schema methods only: no corpus function runs), and runs every
# run of the preset on its dialects: GORM's own AutoMigrate (or
# Migrator().CreateTable) creates the tables in a fresh database (postgres:
# gorm_oracle_<run> on the scratch Postgres 17 cluster of
# scripts/cloud-postgres.sh, dropped after the run; sqlite: a scratch file
# through the pure-Go driver github.com/glebarez/sqlite), and
# the catalog is dumped as JSON. Then compare:
#
#   GRAPHVEIN_GORM_ORACLE=<out.json> GRAPHVEIN_GORM_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/gorm -run TestGormOracle -v
#
# Usage:
#   bash scripts/oracle/gorm_catalog.sh --scratch DIR --pg URL --out FILE \
#     --root ROOT (--fixture | --corpus)
#
#   --fixture  the modules of testdata/fixtures/gorm-models (ROOT is that
#        directory): every module that claims a table (not dynamic/ or v1/);
#        lite/ at gorm 1.25.12, rev2old/ with gorm.io/driver/postgres
#        v1.5.4, the others at gorm 1.31.2.
#   --corpus   gotify and realworld-gin at their pins (ROOT holds the
#        checkouts gotify/ and realworld-gin/, scripts/cloud-corpus.sh).
#   --pg URL   a role that may create databases.
#
# Modules come from the Go module proxy into DIR (GOMODCACHE, GOPATH and
# GOCACHE under --scratch; set GOPROXY to seed them from a local cache).
# CGO is off: no C compiler is needed. Writes only under --scratch and
# --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'gorm_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
scratch="" pg="" out="" root="" preset=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --pg) pg=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --fixture) preset=fixture; shift ;;
    --corpus) preset=corpus; shift ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$out" ] && [ -n "$root" ] && [ -n "$preset" ] && [ -n "$pg" ] ||
  die "--scratch, --pg, --out, --root and --fixture or --corpus are required"
command -v go >/dev/null || die "go is not on PATH"
root=$(cd "$root" && pwd)
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
out_dir=$(cd "$(dirname "$out")" && pwd)
out="$out_dir/$(basename "$out")"

export GOMODCACHE="$scratch/gomod" GOPATH="$scratch/gopath" GOCACHE="$scratch/gocache"
export GOFLAGS=-modcacherw CGO_ENABLED=0 GOWORK=off GOPRIVATE=example.invalid
# Binaries carry the platform's executable suffix (.exe on Windows, where
# Git Bash runs this script).
exe=$(go env GOEXE)

# The pins: gotify's go.sum (gorm 1.31.2, postgres 1.6.3, pgx 5.10.0,
# inflection 1.0.0) and realworld's (gorm 1.25.12, postgres 1.5.11);
# glebarez/sqlite 1.11.0 for both (task file, "Oracle design").
pins131="gorm.io/gorm@v1.31.2 gorm.io/driver/postgres@v1.6.3 github.com/glebarez/sqlite@v1.11.0 github.com/jackc/pgx/v5@v5.10.0 github.com/jinzhu/inflection@v1.0.0"
pins125="gorm.io/gorm@v1.25.12 gorm.io/driver/postgres@v1.5.11 github.com/glebarez/sqlite@v1.11.0 github.com/jinzhu/inflection@v1.0.0"
# rev2old's pins (T018g5 Revision 2, A7): a postgres driver before v1.5.6.
pinsold="gorm.io/gorm@v1.31.2 gorm.io/driver/postgres@v1.5.4 github.com/glebarez/sqlite@v1.11.0 github.com/jinzhu/inflection@v1.0.0"

# strip copies a source file of gorm_oracle/ without its build constraint
# (which keeps it out of the graphvein module).
strip() { sed '1{/^\/\/go:build ignore$/d;}' "$1" >"$2"; }

bin="$scratch/bin"
mkdir -p "$bin" "$scratch/copytypes"
strip "$here/gorm_oracle/copytypes.go" "$scratch/copytypes/main.go"
(cd "$scratch/copytypes" && { [ -f go.mod ] || go mod init example.invalid/copytypes >/dev/null 2>&1; } &&
  go build -o "$bin/copytypes$exe" .) || die "building copytypes failed"

# build TARGET PINS KEY=SRC:PKGS... : a scratch module for one run file.
# Each KEY=SRC:PKGS copies the packages or files PKGS (comma-separated,
# "..." for all)
# of the module directory SRC (relative to ROOT) to corpus/<key>/ with the
# module's own import path rewritten.
build() {
  target=$1 pins=$2
  shift 2
  mod="$scratch/$target"
  rm -rf "$mod"
  mkdir -p "$mod"
  for f in main.go catalog.go "runs_$target.go"; do
    strip "$here/gorm_oracle/$f" "$mod/$f"
  done
  (cd "$mod" && go mod init example.invalid/gormoracle >/dev/null 2>&1) || die "$target: go mod init failed"
  for p in $pins; do
    (cd "$mod" && go mod edit -require="$p") || die "$target: go mod edit $p failed"
  done
  for spec in "$@"; do
    key=${spec%%=*} rest=${spec#*=}
    src=${rest%%:*} pkgs=${rest#*:}
    modpath=$(cd "$root/$src" && go mod edit -json | sed -n 's/^[[:space:]]*"Path": "\(.*\)",*$/\1/p' | head -n 1)
    [ -n "$modpath" ] || die "$target: no module path in $root/$src/go.mod"
    # shellcheck disable=SC2086
    "$bin/copytypes$exe" -src "$root/$src" -dst "$mod/corpus/$key" -rewrite "$modpath=example.invalid/gormoracle/corpus/$key" $(printf '%s' "$pkgs" | tr ',' ' ') ||
      die "$target: copying $src failed"
  done
  (cd "$mod" && go mod tidy >/dev/null && go build -o "$bin/oracle-$target$exe" .) || die "$target: build failed"
  for p in $pins; do
    case "$p" in gorm.io/gorm@*) ;; *) continue ;; esac
    got=$(cd "$mod" && go list -m -f '{{.Version}}' gorm.io/gorm)
    [ "$got" = "${p#*@}" ] || die "$target: gorm $got, want ${p#*@}"
  done
  targets="$targets $target"
}

targets=""
case "$preset" in
  fixture)
    # buildsel/: only its model package (its mixed/ directory holds two
    # packages, which the go command cannot build).
    specs="fixture/buildsel=buildsel:model"
    for m in ctlite dialvar named nocfg nolower pg rel relignore relorder relsplit rev2 rev2alter rev2create rev2ddl rev2lite runtime; do
      specs="$specs fixture/$m=$m:..."
    done
    # shellcheck disable=SC2086
    build fixture131 "$pins131" $specs
    build fixture125 "$pins125" "fixture/lite=lite:..."
    build fixtureold "$pinsold" "fixture/rev2old=rev2old:..."
    ;;
  corpus)
    build gotify "$pins131" "gotify=gotify:model"
    # realworld's users and articles packages hold request types that need
    # gin and the common package: only their model files are copied.
    build realworld "$pins125" "realworld=realworld-gin:users/models.go,articles/models.go"
    ;;
esac

mkdir -p "$scratch/runs"
records=()
for target in $targets; do
  lines=$("$bin/oracle-$target$exe" -list)
  while IFS='|' read -r n dialect; do
    [ -n "$n" ] || continue
    record="$scratch/runs/$n-$dialect.json"
    dsn="$pg"
    [ "$dialect" = sqlite ] && dsn="$scratch/runs/$n.sqlite"
    printf 'gorm_catalog: run %s (%s)\n' "$n" "$dialect" >&2
    "$bin/oracle-$target$exe" -run "$n" -dialect "$dialect" -dsn "$dsn" -out "$record" || die "run $n on $dialect failed"
    rm -f "$scratch/runs/$n.sqlite"
    records+=("$record")
  done <<EOF2
$lines
EOF2
done
[ ${#records[@]} -gt 0 ] || die "no runs"
first=${targets# }
first=${first%% *}
"$bin/oracle-$first$exe" -merge "$out" "${records[@]}"
printf 'gorm_catalog: %d runs written to %s\n' "${#records[@]}" "$out" >&2
