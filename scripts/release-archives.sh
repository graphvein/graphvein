#!/usr/bin/env bash
# Builds the release binaries and packs them into the release archives with
# SHA256SUMS (scripts/release, ADR 0080 for the notices in each archive).
# The release workflow (.github/workflows/release.yml) runs it on a tag;
# scripts/test-release.sh runs it offline for the host system.
#
# Usage: bash scripts/release-archives.sh VERSION OUTDIR
#
# OUTDIR must not exist or be empty; it receives one archive per target and
# SHA256SUMS, nothing else. Environment (all optional):
#   RELEASE_TARGETS  GOOS/GOARCH pairs separated by spaces (default: the six
#                    of `go run ./scripts/release -list`)
#   BUILD_DATE       build date and archive time (default: the committer
#                    time of HEAD in UTC, so a rebuild of a tag gives the
#                    same archives)
#   RELEASE_GOFLAGS  extra go build flags (default: -trimpath -buildvcs=false)
#   REPORT_URL       passed to scripts/build.sh
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
if [ $# -ne 2 ]; then
  echo "usage: bash scripts/release-archives.sh VERSION OUTDIR" >&2
  exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=$1
out=$2
if [ -e "$out" ] && [ -n "$(ls -A "$out")" ]; then
  echo "release-archives: $out exists and is not empty" >&2
  exit 1
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)
build_date=${BUILD_DATE:-$(cd "$root" && TZ=UTC git log -1 --date='format-local:%Y-%m-%dT%H:%M:%SZ' --format=%cd HEAD)}
goflags=${RELEASE_GOFLAGS--trimpath -buildvcs=false}
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
# The packer is a native program: on Windows (Git Bash) it gets C:/...
# paths, also inside its GOOS/GOARCH=PATH arguments, which Git Bash does
# not convert reliably.
if command -v cygpath >/dev/null 2>&1; then
  root=$(cygpath -m "$root")
  out=$(cygpath -m "$out")
  tmp=$(cygpath -m "$tmp")
fi
export CGO_ENABLED=0
(cd "$root" && GOOS= GOARCH= go build -o "$tmp/release$(GOOS= GOARCH= go env GOEXE)" ./scripts/release)
packer="$tmp/release$(GOOS= GOARCH= go env GOEXE)"
targets=${RELEASE_TARGETS:-$("$packer" -list | tr -d '\r')}
args=()
for target in $targets; do
  goos=${target%/*}
  goarch=${target#*/}
  if [ -z "$goos" ] || [ -z "$goarch" ] || [ "$goos/$goarch" != "$target" ]; then
    echo "release-archives: bad target $target (want GOOS/GOARCH)" >&2
    exit 1
  fi
  exe=graphvein
  [ "$goos" = windows ] && exe=graphvein.exe
  bin="$tmp/$goos-$goarch/$exe"
  echo "release-archives: building $target"
  GOOS=$goos GOARCH=$goarch GOFLAGS=$goflags VERSION=$version BUILD_DATE=$build_date OUTPUT=$bin \
    bash "$root/scripts/build.sh"
  args+=("$target=$bin")
done
if [ ${#args[@]} -eq 0 ]; then
  echo "release-archives: no targets" >&2
  exit 1
fi
"$packer" -version "$version" -date "$build_date" -out "$out" -root "$root" "${args[@]}"
