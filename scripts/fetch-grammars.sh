#!/usr/bin/env bash
# Fetch for review only. No compiler, dependency changes or language registration.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
usage() { echo 'Usage: bash scripts/fetch-grammars.sh RUN_ID | --release TAG' >&2; exit 2; }
[[ $# -gt 0 ]] || usage
mode=run
if [[ "$1" == --release ]]; then
  [[ $# == 2 ]] || usage
  mode=release
  id=$2
  [[ "$id" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]] || usage
else
  [[ $# == 1 && "$1" =~ ^[0-9]+$ ]] || usage
  id=$1
fi
for tool in gh sha256sum mktemp; do
  command -v "$tool" >/dev/null || { echo "Required existing tool: $tool" >&2; exit 1; }
done
destination="grammars/runs/$mode-$id"
[[ ! -e "$destination" ]] || { echo "Refusing to overwrite $destination" >&2; exit 1; }
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
if [[ "$mode" == run ]]; then
  result=$(gh run view "$id" --json conclusion,workflowName --jq '[.conclusion,.workflowName] | join("|")')
  [[ "$result" == 'success|Grammar assets' ]] || { echo "Not a successful Grammar assets run: $result" >&2; exit 1; }
  gh run download "$id" --name grammar-assets --dir "$temporary"
else
  # Releases attach the individual files from grammar-assets, without repacking.
  gh release download "$id" --pattern SHA256SUMS --dir "$temporary"
fi
[[ -f "$temporary/SHA256SUMS" ]] || { echo 'Missing SHA256SUMS' >&2; exit 1; }
names=()
while IFS= read -r line || [[ -n "$line" ]]; do
  [[ "$line" =~ ^[0-9a-f]{64}\ [\ \*]([a-zA-Z0-9][a-zA-Z0-9._-]*)$ ]] || { echo 'Unsafe checksum entry' >&2; exit 1; }
  name=${BASH_REMATCH[1]}
  [[ "$name" != SHA256SUMS ]] || { echo 'Self-referencing checksum entry' >&2; exit 1; }
  for previous in ${names[@]+"${names[@]}"}; do
    [[ "$previous" != "$name" ]] || { echo 'Duplicate checksum entry' >&2; exit 1; }
  done
  names+=("$name")
  if [[ "$mode" == release ]]; then
    gh release download "$id" --pattern "$name" --dir "$temporary"
  fi
done < "$temporary/SHA256SUMS"
required=(manifest.yaml NOTICES.md UPSTREAM_NOTICES.txt LLVM-LICENSE.txt WASI-SDK-NOTICES.txt)
for language in html angular c_sharp dart javascript typescript tsx; do
  required+=("wasitter-$language.wasm" "$language-LICENSES.txt" "$language-build.json")
done
[[ ${#names[@]} == ${#required[@]} ]] || { echo 'Unexpected artifact inventory' >&2; exit 1; }
for expected in "${required[@]}"; do
  found=false
  for name in "${names[@]}"; do [[ "$name" != "$expected" ]] || found=true; done
  [[ "$found" == true && -f "$temporary/$expected" && ! -L "$temporary/$expected" ]] || { echo "Missing regular artifact: $expected" >&2; exit 1; }
done
(cd "$temporary" && sha256sum --strict --check SHA256SUMS)
mkdir -p grammars/runs
mkdir "$destination"
for name in "${names[@]}" SHA256SUMS; do cp "$temporary/$name" "$destination/$name"; done
printf 'Verified artifacts saved to %s; review before committing assets.\n' "$destination"
