#!/usr/bin/env bash
# Offline check of scripts/release-archives.sh: builds the release archive
# of the host system only (the six-target matrix, archive contents and
# SHA256SUMS of all six are tested with fake binaries in scripts/release),
# then checks SHA256SUMS and, for a .tar.gz, that the unpacked binary
# reports the given version. Run by scripts/test.sh.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
if command -v cygpath >/dev/null 2>&1; then
  tmp=$(cygpath -m "$tmp")
fi
goos=$(go env GOHOSTOS | tr -d '\r')
goarch=$(go env GOHOSTARCH | tr -d '\r')
host="$goos/$goarch"
# Without -trimpath the build reuses the packages the test run compiled.
RELEASE_TARGETS=$host RELEASE_GOFLAGS='' BUILD_DATE=2026-11-20T10:00:00Z \
  bash scripts/release-archives.sh v0.0.0-test "$tmp/dist" >/dev/null
count=$(ls "$tmp/dist" | wc -l | tr -d ' ')
if [ "$count" != 2 ]; then
  echo "test-release: expected one archive and SHA256SUMS, found:" >&2
  ls "$tmp/dist" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp/dist" && sha256sum -c SHA256SUMS >/dev/null)
else
  (cd "$tmp/dist" && shasum -a 256 -c SHA256SUMS >/dev/null)
fi
name="graphvein-v0.0.0-test-$goos-$goarch"
if [ -f "$tmp/dist/$name.tar.gz" ]; then
  tar -xzf "$tmp/dist/$name.tar.gz" -C "$tmp"
  for f in graphvein LICENSE NOTICE THIRD_PARTY_NOTICES README.md; do
    [ -f "$tmp/$name/$f" ] || { echo "test-release: $f missing from the archive" >&2; exit 1; }
  done
  first=$("$tmp/$name/graphvein" version | head -1)
  if [ "$first" != 'graphvein v0.0.0-test' ]; then
    echo "test-release: version printed $first" >&2
    exit 1
  fi
elif [ -f "$tmp/dist/$name.zip" ]; then
  # Windows: the archive's contents are checked by the scripts/release
  # tests; list them here too where unzip is installed.
  if command -v unzip >/dev/null 2>&1; then
    unzip -Z1 "$tmp/dist/$name.zip" | tr -d '\r' > "$tmp/zip.txt"
    for f in graphvein.exe LICENSE NOTICE THIRD_PARTY_NOTICES README.md; do
      grep -qxF "$name/$f" "$tmp/zip.txt" || { echo "test-release: $f missing from the archive" >&2; exit 1; }
    done
  fi
else
  echo "test-release: no archive named $name" >&2
  ls "$tmp/dist" >&2
  exit 1
fi
# A non-empty output folder is refused.
if bash scripts/release-archives.sh v0.0.0-test "$tmp/dist" >/dev/null 2>&1; then
  echo 'test-release: a non-empty output folder was accepted' >&2
  exit 1
fi
printf 'Release archive checks passed.\n'
