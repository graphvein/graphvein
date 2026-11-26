#!/usr/bin/env bash
# Next.js App Router route handler and server action shape oracle (T029
# SH-N). Opt-in, never run by CI; see README.md. Writes only under
# --scratch.
#
#   bash scripts/oracle/nextjs_shape/nextjs_shape.sh --scratch DIR \
#     [--sets "next14 next15 next16"] [--snapshot snap.ndjson] [--port 54400]
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
scratch=""
sets="next14 next15 next16"
snapshot=""
port=54400
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --sets) sets=$2; shift 2 ;;
    --snapshot) snapshot=$2; shift 2 ;;
    --port) port=$2; shift 2 ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
if [ -z "$scratch" ]; then
  echo "--scratch is required" >&2
  exit 2
fi
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
export NEXT_TELEMETRY_DISABLED=1

if [ -z "$snapshot" ]; then
  rm -rf "$scratch/fixture"
  cp -R "$repo/testdata/fixtures/nextjs-shape" "$scratch/fixture"
  (cd "$repo" && go run ./cmd/graphvein index -emit "$scratch/snap.ndjson" "$scratch/fixture" > "$scratch/index.log" 2>&1)
  snapshot="$scratch/snap.ndjson"
fi

status=0
for set in $sets; do
  inst="$scratch/$set"
  mkdir -p "$inst"
  if [ ! -d "$inst/node_modules" ]; then
    cp "$here/$set/package.json" "$here/$set/package-lock.json" "$inst/"
    (cd "$inst" && npm ci --ignore-scripts --no-audit --no-fund > "$inst/npm.log" 2>&1)
  fi
  app="$inst/app"
  rm -rf "$app"
  cp -R "$repo/testdata/fixtures/nextjs-shape/web" "$app"
  rm -f "$app/package.json"
  # The fixture's own next.config.js (its redirects and headers rules) is
  # kept; the oracle only adds the option that skips type errors.
  mv "$app/next.config.js" "$app/fixture.next.config.js"
  cat > "$app/next.config.js" <<'CONFIG'
module.exports = { ...require('./fixture.next.config.js'), typescript: { ignoreBuildErrors: true } };
CONFIG
  mkdir -p "$app/app/oracle-actions"
  cat > "$app/app/oracle-actions/page.tsx" <<'PAGE'
// Oracle-only page: renders each server action as a form action so its
// action id appears in the HTML.
import { move, rename, signup, subscribe } from '../actions';

export const dynamic = 'force-dynamic';

export default function Page() {
  const forms: [string, any][] = [['subscribe', subscribe], ['rename', rename], ['signup', signup], ['move', move]];
  return (
    <main>
      {forms.map(([name, action]) => (
        <div id={'action-' + name} key={name}>
          <form action={action}>
            <button type="submit">{name}</button>
          </form>
        </div>
      ))}
    </main>
  );
}
PAGE
  echo "== $set: next build" >&2
  (cd "$app" && "$inst/node_modules/.bin/next" build > "$inst/build.log" 2>&1) || { echo "$set: build failed, see $inst/build.log" >&2; status=1; continue; }
  # The server runs in its own process group so that stopping it stops
  # next-server too (it outlives a killed `next start` otherwise).
  setsid bash -c 'cd "$1" && exec "$2" start -H 127.0.0.1 -p "$3"' _ "$app" "$inst/node_modules/.bin/next" "$port" > "$inst/start.log" 2>&1 &
  pid=$!
  for _ in $(seq 1 60); do
    if curl -s -o /dev/null "http://127.0.0.1:$port/api/plain"; then break; fi
    sleep 1
  done
  set +e
  node "$here/oracle.mjs" --snapshot "$snapshot" --app "$app" --base "http://127.0.0.1:$port" \
    --cases "$here/cases.json" --out "$inst/result.json" --label "$set" > "$inst/oracle.log" 2>&1
  rc=$?
  set -e
  kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  echo "== $set: oracle exit $rc ($inst/result.json)" >&2
  if [ "$rc" -ne 0 ]; then status=1; fi
done
exit $status
