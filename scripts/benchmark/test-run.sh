#!/usr/bin/env bash
# Offline end-to-end test of the benchmark driver in stand-in mode: no agent
# CLI, no sign-in, no network, no spend. Covers a correct answer, a wrong
# answer, a malformed answer, a timeout, a rate-limit stop, the per-run cost
# ceiling, the total cap ("not run (cap)"), a run served outside the
# preferred endpoints, token capture and the D5 MCP result sizes; then the
# Claude API-billing path: --bare chosen when ANTHROPIC_API_KEY is set, the
# reported cost booked, a missing cost booked at its worst case, the Claude
# cap stop, and the key kept out of every file and log.
# Run by scripts/test.sh. Bash 3.2 compatible; runs in Git Bash.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

tmp=$(mktemp -d)
cleanup() {
  # The stand-in workspaces are made writable again before removal.
  chmod -R u+w "$tmp" 2>/dev/null || true
  rm -rf -- "$tmp"
}
trap cleanup EXIT

mkdir -p "$tmp/ws-fastapi/backend/app" "$tmp/ws-langflow/src"
printf 'router = APIRouter()\n' >"$tmp/ws-fastapi/backend/app/main.py"
printf 'flows\n' >"$tmp/ws-langflow/src/README.md"
printf '{"data":{"label":"stand-in","usage":1.0,"limit":null,"limit_remaining":null}}\n' >"$tmp/key.json"
cat >"$tmp/scenarios.txt" <<'EOF'
# format question mode scenario
claude BQ-1 base correct
claude BQ-1 mcp wrong
claude BQ-3 base hang
claude BQ-3 mcp malformed
codex BQ-3 base ratelimit
opencode BQ-1 mcp elsewhere
opencode BQ-3 base costly
EOF

# A login stored by `opencode auth login` in the user's data directory: the
# driver must give opencode empty XDG homes, or the opencode stand-in, like
# the real CLI, would use it instead of OPENROUTER_API_KEY (and fails).
mkdir -p "$tmp/user-data/opencode" "$tmp/user-state" "$tmp/user-config"
printf '{"openrouter":{"type":"api","key":"stored-login"}}\n' >"$tmp/user-data/opencode/auth.json"

out=$tmp/out
log=$tmp/run.log
if ! GV_BENCH_STANDIN_KEYFILE="$tmp/key.json" GV_BENCH_STANDIN_SCENARIOS="$tmp/scenarios.txt" \
  GV_BENCH_SETTLE_SECONDS=0 OPENROUTER_API_KEY= ANTHROPIC_API_KEY= \
  XDG_DATA_HOME="$tmp/user-data" XDG_STATE_HOME="$tmp/user-state" XDG_CONFIG_HOME="$tmp/user-config" \
  bash scripts/benchmark/run.sh --standin --agents claude,codex,opencode \
  --questions BQ-1,BQ-3,BQ-12 --reps 1 --no-shuffle --timeout 3s \
  --workspace-fastapi "$tmp/ws-fastapi" --workspace-langflow "$tmp/ws-langflow" \
  --cap-usd 0.26 --run-cap-usd 0.2 --step-context 10000 --step-output 1000 \
  --label standin-test --out "$out" --work "$tmp/work" >"$log" 2>&1; then
  cat "$log" >&2
  echo "test-run: the stand-in run failed" >&2
  exit 1
fi

fails=0
row() {
  grep "\"run_id\":\"$1\"" "$out/runs.jsonl" | tail -n 1
}
expect() {
  # expect RUN_ID TEXT...: every TEXT occurs in the run's last row.
  local id=$1 r t
  shift
  r=$(row "$id")
  if [ -z "$r" ]; then
    echo "test-run: no row for $id" >&2
    fails=$((fails + 1))
    return 0
  fi
  for t in "$@"; do
    case $r in
      *"$t"*) ;;
      *) echo "test-run: $id lacks $t: $r" >&2; fails=$((fails + 1)) ;;
    esac
  done
}

expect claude-base-bq-1-r1 '"outcome":"answered"' '"correct":true' '"score":1' '"input_tokens":12' '"cache_read_tokens":38100' '"output_tokens":510' '"tool_calls":2' '"cost_kind":"estimate_not_billed"' '"auth_path":"subscription"' '"billing":"subscription"' '"api_key_source":"none"'
expect claude-mcp-bq-1-r1 '"outcome":"answered"' '"correct":false' '"mcp_server_status":"connected"' '"mcp_tool_calls":1' '"mcp_both_copies":1'
expect claude-base-bq-3-r1 '"outcome":"timeout"'
expect claude-mcp-bq-3-r1 '"outcome":"no_answer"' '"detail":"no_json_block"'
expect claude-base-bq-12-r1 '"outcome":"answered"' '"correct":true' '"corpus":"langflow"'
expect codex-base-bq-1-r1 '"outcome":"answered"' '"correct":true' '"input_tokens":7250' '"cache_read_tokens":40960' '"reasoning_tokens":256' '"tool_calls":2'
expect codex-mcp-bq-1-r1 '"outcome":"answered"' '"mcp_tool_calls":1' '"mcp_both_copies":1'
expect codex-base-bq-3-r1 '"outcome":"agent_error"' '"error_kind":"rate_limit"'
expect codex-mcp-bq-3-r1 '"outcome":"not_run_rate_limit"'
expect codex-base-bq-12-r1 '"outcome":"not_run_rate_limit"'
expect opencode-base-bq-1-r1 '"outcome":"answered"' '"correct":true' '"cost_kind":"billed"' '"input_tokens":11700' '"cache_read_tokens":20200' '"served_elsewhere":false'
expect opencode-mcp-bq-1-r1 '"outcome":"answered"' '"served_elsewhere":true' '"mcp_tool_calls":1' '"mcp_results":1'
expect opencode-base-bq-3-r1 '"outcome":"cost_cap"'
expect opencode-mcp-bq-3-r1 '"outcome":"not_run_cap"'
expect opencode-base-bq-12-r1 '"outcome":"not_run_cap"'
expect opencode-mcp-bq-12-r1 '"outcome":"not_run_cap"'

for f in manifest.json summary.md answers/claude-base-bq-1-r1.json answers/claude-mcp-bq-3-r1.json; do
  [ -f "$out/$f" ] || { echo "test-run: $f missing" >&2; fails=$((fails + 1)); }
done
grep -q '"stand_in": true' "$out/manifest.json" || { echo "test-run: manifest lacks stand_in" >&2; fails=$((fails + 1)); }
grep -q 'not run' "$out/summary.md" || { echo "test-run: summary lacks not-run counts" >&2; fails=$((fails + 1)); }
grep -q 'Runs served outside the preferred endpoints' "$out/summary.md" || { echo "test-run: summary lacks the served-elsewhere list" >&2; fails=$((fails + 1)); }
if ls "$out" | grep -q -i transcript; then
  echo "test-run: transcripts must stay in the work directory" >&2
  fails=$((fails + 1))
fi
[ -d "$tmp/work/opencode-xdg-data" ] && [ -d "$tmp/work/opencode-xdg-state" ] ||
  { echo "test-run: opencode data and state homes were not isolated" >&2; fails=$((fails + 1)); }
rows=$(wc -l <"$out/runs.jsonl" | tr -d ' ')
[ "$rows" = 18 ] || { echo "test-run: expected 18 rows, got $rows" >&2; fails=$((fails + 1)); }

# A real-mode dry run without CLI paths fails the agent's preflight and
# names the flag, instead of looking the CLI up on PATH.
if ! GV_BENCH_CLAUDE= ANTHROPIC_API_KEY= bash scripts/benchmark/run.sh --agents claude --questions BQ-1 --modes base --reps 1 \
  --workspace-fastapi "$tmp/ws-fastapi" --label dry --out "$tmp/out-dry" --work "$tmp/work-dry" --dry-run >"$tmp/dry.log" 2>&1; then
  cat "$tmp/dry.log" >&2
  echo "test-run: dry run failed" >&2
  fails=$((fails + 1))
fi
grep -q 'pass --claude-bin or set GV_BENCH_CLAUDE' "$tmp/dry.log" || { echo "test-run: dry run did not report the missing CLI path" >&2; fails=$((fails + 1)); }

# --- codex-visible and --rerun ----------------------------------------------
# codex-visible runs only in mcp mode, with the rewritten model catalog (the
# stand-in refuses a catalog that still offers tool search); --rerun keeps
# only the named runs, here every codex-visible run and one opencode run.
vis_out=$tmp/out-vis
if ! GV_BENCH_STANDIN_KEYFILE="$tmp/key.json" GV_BENCH_SETTLE_SECONDS=0 OPENROUTER_API_KEY= ANTHROPIC_API_KEY= \
  XDG_DATA_HOME="$tmp/user-data" XDG_STATE_HOME="$tmp/user-state" XDG_CONFIG_HOME="$tmp/user-config" \
  bash scripts/benchmark/run.sh --standin --agents codex-visible,opencode --questions BQ-1,BQ-12 --reps 2 --timeout 3s \
  --workspace-fastapi "$tmp/ws-fastapi" --workspace-langflow "$tmp/ws-langflow" \
  --rerun 'codex-visible-*,opencode-base-bq-12-r2' \
  --label standin-r2 --out "$vis_out" --work "$tmp/work-vis" >"$tmp/run-vis.log" 2>&1; then
  cat "$tmp/run-vis.log" >&2
  echo "test-run: the codex-visible / --rerun stand-in run failed" >&2
  fails=$((fails + 1))
fi
for id in codex-visible-mcp-bq-1-r1 codex-visible-mcp-bq-1-r2 codex-visible-mcp-bq-12-r1 codex-visible-mcp-bq-12-r2; do
  r=$(grep "\"run_id\":\"$id\"" "$vis_out/runs.jsonl" | tail -n 1)
  case $r in
    *'"outcome":"answered"'*'"variant":"MCP tools visible'*|*'"variant":"MCP tools visible'*'"outcome":"answered"'*) ;;
    *) echo "test-run: $id is not an answered codex-visible run: $r" >&2; fails=$((fails + 1)) ;;
  esac
done
grep -q '"run_id":"opencode-base-bq-12-r2".*"outcome":"answered"' "$vis_out/runs.jsonl" || { echo "test-run: the rerun opencode row is missing" >&2; fails=$((fails + 1)); }
rows=$(wc -l <"$vis_out/runs.jsonl" | tr -d ' ')
[ "$rows" = 5 ] || { echo "test-run: --rerun: expected 5 rows, got $rows" >&2; fails=$((fails + 1)); }
grep -q '"codex-visible-base' "$vis_out/runs.jsonl" && { echo "test-run: codex-visible ran in base mode" >&2; fails=$((fails + 1)); }
for k in '"rerun": "codex-visible-\*,opencode-base-bq-12-r2"' '"catalog_sha256"' '"public_tree"' '"external_directory"'; do
  grep -q -e "$k" "$vis_out/manifest.json" "$tmp/work-vis/config/"*.json || { echo "test-run: $k not recorded" >&2; fails=$((fails + 1)); }
done

# --- Claude on API billing -------------------------------------------------
# A fake key selects the API path. Costs (see gvbench/claude_cost.go): BQ-1
# base reports 0.04125; BQ-1 mcp has no total_cost_usd and is booked from its
# tokens at the price file (0.034324); BQ-3 base hangs until the timeout and,
# with no result object, books two worst-case calls (2 x 0.01 = 0.02). An
# attempt needs ceiling 0.05 + 2 x 0.01 = 0.07 left of the 0.165 cap, so BQ-3
# mcp (0.069426 left) is not run.
fake_key=sk-ant-standin-0123456789abcdef
printf 'claude BQ-1 mcp nocost\nclaude BQ-3 base hang\n' >"$tmp/scenarios-api.txt"
api_out=$tmp/out-api
api_log=$tmp/run-api.log
api_rc=0
GV_BENCH_STANDIN_SCENARIOS="$tmp/scenarios-api.txt" OPENROUTER_API_KEY= ANTHROPIC_API_KEY="$fake_key" \
  bash scripts/benchmark/run.sh --standin --agents claude --questions BQ-1,BQ-3 --reps 1 --no-shuffle --timeout 3s \
  --workspace-fastapi "$tmp/ws-fastapi" --claude-cap-usd 0.165 --claude-run-cap-usd 0.05 \
  --claude-step-context 1000 --claude-step-output 100 \
  --label standin-api --out "$api_out" --work "$tmp/work-api" >"$api_log" 2>&1 || api_rc=$?
if [ "$api_rc" != 0 ]; then
  cat "$api_log" >&2
  echo "test-run: the API-billing stand-in run failed" >&2
  fails=$((fails + 1))
fi
expect_api() {
  # expect_api RUN_ID TEXT...: like expect, for the API-billing run.
  local id=$1 r t
  shift
  r=$(grep "\"run_id\":\"$id\"" "$api_out/runs.jsonl" | tail -n 1)
  for t in "$@"; do
    case $r in
      *"$t"*) ;;
      *) echo "test-run: $id lacks $t: $r" >&2; fails=$((fails + 1)) ;;
    esac
  done
}
expect_api claude-base-bq-1-r1 '"outcome":"answered"' '"auth_path":"api_key_bare"' '"billing":"API billing"' '"api_key_source":"ANTHROPIC_API_KEY"' '"cost_usd":0.04125' '"cost_kind":"api_billed"'
expect_api claude-mcp-bq-1-r1 '"outcome":"answered"' '"cost_usd":0.034324' '"cost_kind":"api_billed_worst_case"'
expect_api claude-base-bq-3-r1 '"outcome":"timeout"' '"cost_usd":0.02,' '"cost_kind":"api_billed_worst_case"'
expect_api claude-mcp-bq-3-r1 '"outcome":"not_run_cap"' '"auth_path":"api_key_bare"' '"billing":"API billing"'
grep -q '"auth_path": "api_key_bare"' "$api_out/manifest.json" || { echo "test-run: manifest lacks the Claude auth path" >&2; fails=$((fails + 1)); }
grep -q '"stop_decision"' "$api_out/manifest.json" || { echo "test-run: manifest lacks the Claude cap decision" >&2; fails=$((fails + 1)); }
grep -q '| claude | API billing | 3 |' "$api_out/summary.md" || { echo "test-run: summary lacks the Claude API spend" >&2; fails=$((fails + 1)); }
grep -q 'Claude cap' "$api_log" || { echo "test-run: log lacks the Claude cap stop" >&2; fails=$((fails + 1)); }

# The key never reaches a results file or the log; a transcript that
# contains it stops the run before anything is written.
leak_log=$tmp/run-leak.log
printf 'claude * * leak\n' >"$tmp/scenarios-leak.txt"
if GV_BENCH_STANDIN_SCENARIOS="$tmp/scenarios-leak.txt" OPENROUTER_API_KEY= ANTHROPIC_API_KEY="$fake_key" \
  bash scripts/benchmark/run.sh --standin --agents claude --questions BQ-1 --modes base --reps 1 --timeout 3s \
  --workspace-fastapi "$tmp/ws-fastapi" --label standin-leak --out "$tmp/out-leak" --work "$tmp/work-leak" >"$leak_log" 2>&1; then
  echo "test-run: a transcript with the API key must stop the run" >&2
  fails=$((fails + 1))
fi
grep -q 'an API key appeared in the output of claude-base-bq-1-r1' "$leak_log" || { echo "test-run: the leak run did not report the refused write" >&2; fails=$((fails + 1)); }
[ ! -s "$tmp/out-leak/runs.jsonl" ] || { echo "test-run: the leak run wrote a row" >&2; fails=$((fails + 1)); }
if grep -r -q -F -e "$fake_key" "$api_out" "$api_log" "$tmp/out-leak" "$leak_log"; then
  echo "test-run: the API key appears in a results file or log" >&2
  fails=$((fails + 1))
fi
redacted=$(printf 'curl: auth %s failed\n' "$fake_key" | ANTHROPIC_API_KEY="$fake_key" "$tmp/work-api/bin/gvbench" redact)
[ "$redacted" = 'curl: auth [REDACTED] failed' ] || { echo "test-run: redaction of ANTHROPIC_API_KEY failed: $redacted" >&2; fails=$((fails + 1)); }

if [ "$fails" != 0 ]; then
  echo "--- run log" >&2
  cat "$log" >&2
  echo "test-run: $fails failure(s)" >&2
  exit 1
fi
echo "test-run: OK"
