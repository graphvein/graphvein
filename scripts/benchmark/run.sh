#!/usr/bin/env bash
# Agent benchmark driver: runs Claude Code, Codex and opencode (an open model
# through OpenRouter) on the frozen question set, with and without the
# Graphvein MCP server, and writes per-run rows, final answers, a manifest
# and a summary. Method: docs/evidence/benchmark/method.md.
#
# Runs in Git Bash on Windows, bash 3.2 on macOS and bash on Linux. Needs go,
# git and tar; curl only for OpenRouter runs. No jq or python.
#
# API keys come only from the environment: OPENROUTER_API_KEY (opencode) and
# ANTHROPIC_API_KEY (Claude Code on API billing, run with --bare). Neither is
# accepted as a flag, written to a file or printed.
#
#   bash scripts/benchmark/run.sh --help
set -euo pipefail

case $- in
  *x*) echo "gvbench: do not run with set -x (it would print the API keys)" >&2; exit 1 ;;
esac

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
# shellcheck source=agents.sh
. "$here/agents.sh"

usage() {
  cat <<'EOF'
Usage: bash scripts/benchmark/run.sh [flags]

CLI paths (flag or environment variable; never looked up on PATH):
  --claude-bin PATH      GV_BENCH_CLAUDE      Claude Code CLI
  --codex-bin PATH       GV_BENCH_CODEX       Codex CLI
  --opencode-bin PATH    GV_BENCH_OPENCODE    opencode CLI
  --graphvein-bin PATH   GV_BENCH_GRAPHVEIN   graphvein (index build; default MCP command)
  --mcp-command PATH     GV_BENCH_MCP_COMMAND MCP server command (default: graphvein bin)
  --mcp-arg ARG          GV_BENCH_MCP_ARGS    MCP server argument, repeatable (env: space-separated;
                                              default "mcp"); {workspace} {store} {corpus} expand
  --go-bin PATH          GV_BENCH_GO          (default: go)
  --curl-bin PATH        GV_BENCH_CURL        (default: curl)
  --git-bin PATH         GV_BENCH_GIT         (default: git)

Plan:
  --agents LIST          claude,codex,opencode (default: all three); also codex-visible:
                         Codex with its MCP tools shown instead of deferred (mcp mode only)
  --questions LIST       BQ ids, comma-separated, or "all" (default)
  --question-file FILE   default docs/evidence/benchmark/questions.jsonl
  --modes LIST           base,mcp (default)
  --reps N               repetitions (default 3)
  --agent-reps SPEC      per-agent repetitions overriding --reps, e.g. codex=1
  --seed N               shuffle seed (default 20261123)
  --no-shuffle           keep the listed order
  --rerun LIST           run only these run ids (comma-separated; * matches any text), e.g.
                         opencode-base-bq-21-r1,codex-visible-mcp-*; for a new label that
                         re-runs selected runs of an earlier one
  --timeout DURATION     per run, Go syntax (default 10m)
  --max-turns N          Claude --max-turns and opencode steps (default 60)
  --model-claude ID      Claude model (default claude-opus-5-5)
  --model-codex ID       Codex model (default gpt-6.2-sol; fallback gpt-6.1-sol)
  --model-opencode ID    OpenRouter model (default: the price file's model)
  --codex-config KEY=VAL extra codex -c override, repeatable (default: web_search="disabled")

Corpora:
  --corpus-fastapi DIR   git checkout; the pinned commit is exported from it
  --corpus-langflow DIR  git checkout
  --workspace-fastapi DIR / --workspace-langflow DIR
                         use a prepared directory as the workspace instead

OpenRouter (key only from OPENROUTER_API_KEY):
  --cap-usd X            total hard cap (default 8)
  --run-cap-usd X        per-run ceiling at worst-case prices (default 0.45)
  --prices FILE          committed price file (default scripts/benchmark/prices/<model>.json)
  --step-context N       prompt-token bound of one model call (default: the model's context)
  --step-output N        output-token bound of one call (default 32768)

Claude Code on API billing (only when ANTHROPIC_API_KEY is set; runs use --bare):
  --claude-cap-usd X     GV_BENCH_CLAUDE_CAP_USD total cap of Claude API spend (default 60)
  --claude-run-cap-usd X GV_BENCH_CLAUDE_RUN_CAP_USD per-run ceiling (default 3; also
                         passed to Claude Code as --max-budget-usd)
  --claude-prices FILE   Anthropic price file (default scripts/benchmark/prices/<model>.json)
  --claude-step-context N prompt-token bound of one call (default: the model's context)
  --claude-step-output N output-token bound of one call (default 32768; set as
                         CLAUDE_CODE_MAX_OUTPUT_TOKENS)

Output and control:
  --label NAME           run label (default: date + "run")
  --out DIR              results (default docs/evidence/benchmark/runs/<label>)
  --work DIR             transcripts, workspaces, index stores (default $HOME/graphvein-bench/<label>)
  --resume               continue a run whose --out already has rows
  --allow-api-billing    allow Claude/Codex runs on an API-key sign-in
  --accept-unconfirmed-signin
                         run an agent whose sign-in kind the check cannot place
  --dry-run              preflight and print the plan, run nothing
  --standin              use the stand-in agents (no CLI, no sign-in, no spend)
EOF
}

claude_bin=${GV_BENCH_CLAUDE:-}
codex_bin=${GV_BENCH_CODEX:-}
opencode_bin=${GV_BENCH_OPENCODE:-}
graphvein_bin=${GV_BENCH_GRAPHVEIN:-}
mcp_command=${GV_BENCH_MCP_COMMAND:-}
go=${GV_BENCH_GO:-go}
curl=${GV_BENCH_CURL:-curl}
git=${GV_BENCH_GIT:-git}
mcp_args=()
if [ -n "${GV_BENCH_MCP_ARGS:-}" ]; then
  for a in $GV_BENCH_MCP_ARGS; do mcp_args+=("$a"); done
fi
mcp_args_given=0
agents=claude,codex,opencode
questions=all
question_file=$repo/docs/evidence/benchmark/questions.jsonl
modes=base,mcp
reps=3
agent_reps=
rerun=
seed=20261123
shuffle=1
timeout=10m
max_turns=60
model_claude=claude-opus-5-5
model_codex=gpt-6.2-sol
model_opencode=
codex_config=()
codex_config_given=0
corpus_fastapi=
corpus_langflow=
workspace_fastapi=
workspace_langflow=
cap_usd=8
run_cap_usd=0.45
prices=
step_context=
step_output=32768
label=
out=
work=
resume=0
allow_api_billing=0
accept_unconfirmed=0
dry_run=0
standin=0
settle=${GV_BENCH_SETTLE_SECONDS:-5}
claude_cap_usd=${GV_BENCH_CLAUDE_CAP_USD:-60}
claude_run_cap_usd=${GV_BENCH_CLAUDE_RUN_CAP_USD:-3}
claude_prices=
claude_step_context=
claude_step_output=32768

need() { [ $# -ge 2 ] || die "$1 needs a value"; }
while [ $# -gt 0 ]; do
  case $1 in
    --claude-bin) need "$@"; claude_bin=$2; shift ;;
    --codex-bin) need "$@"; codex_bin=$2; shift ;;
    --opencode-bin) need "$@"; opencode_bin=$2; shift ;;
    --graphvein-bin) need "$@"; graphvein_bin=$2; shift ;;
    --mcp-command) need "$@"; mcp_command=$2; shift ;;
    --mcp-arg)
      need "$@"
      if [ "$mcp_args_given" = 0 ]; then mcp_args=(); mcp_args_given=1; fi
      mcp_args+=("$2"); shift ;;
    --go-bin) need "$@"; go=$2; shift ;;
    --curl-bin) need "$@"; curl=$2; shift ;;
    --git-bin) need "$@"; git=$2; shift ;;
    --agents) need "$@"; agents=$2; shift ;;
    --questions) need "$@"; questions=$2; shift ;;
    --question-file) need "$@"; question_file=$2; shift ;;
    --modes) need "$@"; modes=$2; shift ;;
    --reps) need "$@"; reps=$2; shift ;;
    --agent-reps) need "$@"; agent_reps=$2; shift ;;
    --seed) need "$@"; seed=$2; shift ;;
    --no-shuffle) shuffle=0 ;;
    --rerun) need "$@"; rerun=$2; shift ;;
    --timeout) need "$@"; timeout=$2; shift ;;
    --max-turns) need "$@"; max_turns=$2; shift ;;
    --model-claude) need "$@"; model_claude=$2; shift ;;
    --model-codex) need "$@"; model_codex=$2; shift ;;
    --model-opencode) need "$@"; model_opencode=$2; shift ;;
    --codex-config)
      need "$@"
      codex_config+=(--codex-config "$2"); codex_config_given=1; shift ;;
    --corpus-fastapi) need "$@"; corpus_fastapi=$2; shift ;;
    --corpus-langflow) need "$@"; corpus_langflow=$2; shift ;;
    --workspace-fastapi) need "$@"; workspace_fastapi=$2; shift ;;
    --workspace-langflow) need "$@"; workspace_langflow=$2; shift ;;
    --cap-usd) need "$@"; cap_usd=$2; shift ;;
    --run-cap-usd) need "$@"; run_cap_usd=$2; shift ;;
    --prices) need "$@"; prices=$2; shift ;;
    --claude-cap-usd) need "$@"; claude_cap_usd=$2; shift ;;
    --claude-run-cap-usd) need "$@"; claude_run_cap_usd=$2; shift ;;
    --claude-prices) need "$@"; claude_prices=$2; shift ;;
    --claude-step-context) need "$@"; claude_step_context=$2; shift ;;
    --claude-step-output) need "$@"; claude_step_output=$2; shift ;;
    --step-context) need "$@"; step_context=$2; shift ;;
    --step-output) need "$@"; step_output=$2; shift ;;
    --label) need "$@"; label=$2; shift ;;
    --out) need "$@"; out=$2; shift ;;
    --work) need "$@"; work=$2; shift ;;
    --resume) resume=1 ;;
    --allow-api-billing) allow_api_billing=1 ;;
    --accept-unconfirmed-signin) accept_unconfirmed=1 ;;
    --dry-run) dry_run=1 ;;
    --standin) standin=1 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown flag $1 (see --help)" ;;
  esac
  shift
done

if [ "$mcp_args_given" = 0 ] && [ -z "${GV_BENCH_MCP_ARGS:-}" ]; then
  mcp_args=(mcp)
fi
if [ "$codex_config_given" = 0 ]; then
  codex_config=(--codex-config 'web_search="disabled"')
fi
[ -n "$label" ] || label="$(date -u +%Y-%m-%d)-run"
case $label in
  *[!A-Za-z0-9._-]*) die "--label may contain only letters, digits, '.', '_' and '-'" ;;
esac
[ -n "$out" ] || out=$repo/docs/evidence/benchmark/runs/$label
[ -n "$work" ] || work=${HOME:-/tmp}/graphvein-bench/$label
[ -n "$prices" ] || prices=$here/prices/deepseek-v4.1-flash-1017.json
[ -f "$question_file" ] || die "question file not found: $question_file"
[ -f "$prices" ] || die "price file not found: $prices"
[ -n "$claude_prices" ] || claude_prices=$here/prices/$model_claude.json

# Claude auth path: with ANTHROPIC_API_KEY set, every Claude run uses --bare
# (which reads only that key) and bills the key's API credits under the
# Claude cap; without it the CLI's own sign-in is used as before.
if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
  claude_auth=api_key_bare
else
  claude_auth=subscription
fi

mkdir -p "$out" "$work/bin" "$work/transcripts" "$work/prompts" "$work/config" "$work/ws" "$work/store"
out=$(abs_dir "$out")
work=$(abs_dir "$work")
if [ -s "$out/runs.jsonl" ] && [ "$resume" != 1 ]; then
  die "$out already has runs.jsonl; pass --resume to continue it or choose another --label/--out"
fi
case $work/ in
  "$repo"/*) die "--work must be outside the repository (transcripts and workspaces are not committed)" ;;
esac

# Build the Go helper once (standard library only; no network code).
exe=$("$go" env GOEXE)
gvbench=$work/bin/gvbench$exe
log "building gvbench"
(cd "$repo" && "$go" build -o "$(native_path "$gvbench")" ./scripts/benchmark/gvbench) || die "building gvbench failed"

qfile=$(native_path "$question_file")
pfile=$(native_path "$prices")
manifest=$(native_path "$out/manifest.json")
runs=$(native_path "$out/runs.jsonl")
# Manifest values never carry the machine's home directory or the
# repository's location: both are replaced by placeholders, in POSIX and in
# native (Windows) spelling, so committed results hold no personal paths.
manifest_home=$HOME
manifest_home_native=$(native_path "$HOME")
manifest_repo=$(pwd)
manifest_repo_native=$(native_path "$manifest_repo")
mset() {
  local v=$2
  v=${v//"$manifest_repo_native"/<repo>}
  v=${v//"$manifest_repo"/<repo>}
  v=${v//"$manifest_home_native"/<home>}
  v=${v//"$manifest_home"/<home>}
  gv manifest --file "$manifest" --key "$1" --value "$v"
}
mjson() { gv manifest --file "$manifest" --key "$1" --value "$2" --json; }

[ -n "$model_opencode" ] || model_opencode=$(gv prices show --file "$pfile" --field model)
[ "$model_opencode" = "$(gv prices show --file "$pfile" --field model)" ] ||
  die "--model-opencode $model_opencode is not the price file's model; the cap needs that model's prices"
preferred_endpoints=$(gv prices show --file "$pfile" --field preferred)
[ -n "$step_context" ] || step_context=$(gv prices show --file "$pfile" --field context)

if [ "$questions" = all ]; then
  questions=$(gv question --questions "$qfile" | tr '\n' ',' | sed 's/,$//')
fi
for q in $(printf '%s' "$questions" | tr ',' ' '); do
  gv question --questions "$qfile" --id "$q" --field corpus >/dev/null || die "unknown question $q"
done

if [ "$standin" = 1 ]; then
  log "stand-in mode: no agent CLI runs, nothing is spent"
  export GV_BENCH_STANDIN_QUESTIONS=$qfile
  export GV_BENCH_STANDIN_PRICES=$pfile
  case ",$agents," in
    *,opencode,*) [ -n "${GV_BENCH_STANDIN_KEYFILE:-}" ] && [ -f "$GV_BENCH_STANDIN_KEYFILE" ] ||
      die "stand-in opencode runs need GV_BENCH_STANDIN_KEYFILE (a key-usage JSON file)" ;;
  esac
fi

started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if [ "$resume" != 1 ] || [ ! -f "$out/manifest.json" ]; then
  mset started_at "$started_at"
  mset label "$label"
  mjson stand_in "$([ "$standin" = 1 ] && echo true || echo false)"
  mset question_file "${question_file#"$repo"/}"
  mset question_file_sha256 "$(gv sha256 "$qfile")"
  mset plan.agents "$agents"
  mset plan.questions "$questions"
  mset plan.modes "$modes"
  mjson plan.reps "$reps"
  mjson plan.seed "$seed"
  mjson plan.shuffle "$([ "$shuffle" = 1 ] && echo true || echo false)"
  mset limits.timeout "$timeout"
  mjson limits.max_turns "$max_turns"
  mjson openrouter.cap_usd "$cap_usd"
  mjson openrouter.run_ceiling_usd "$run_cap_usd"
  mjson openrouter.step_context_tokens "$step_context"
  mjson openrouter.step_output_tokens "$step_output"
  mset openrouter.model "$model_opencode"
  mset openrouter.preferred_endpoints "$preferred_endpoints"
  gv manifest --file "$manifest" --key openrouter.prices --value-file "$pfile" --json
  mset prompt.preamble_and_instruction "built by gvbench prompt; identical for every agent and mode"
  mset graphvein.commit "$("$git" -C "$repo" rev-parse HEAD 2>/dev/null || echo unknown)"
fi

# --- Preflight ---------------------------------------------------------------
active=""
signin_billed_claude=false
cpfile=
signin_billed_codex=false
harness_claude="Claude Code"
harness_codex="Codex CLI"
harness_opencode="opencode"
version_claude=unknown
version_codex=unknown
version_opencode=unknown
version_codex_visible=unknown
signin_billed_codex_visible=false
codex_visible_catalog=$work/codex-visible-catalog.json
codex_visible_variant="MCP tools visible: model catalog with supports_search_tool=false, so no tool-search deferral (mcp mode only)"

preflight_agent() {
  local agent=$1 rc=0 verdict signout format avar
  format=$(agent_format "$agent")
  avar=$(printf '%s' "$agent" | sed 's/-/_/g')
  agent_prefix "$agent"
  if [ "$standin" != 1 ]; then
    case $agent in
      claude) check_exec "Claude Code" "$claude_bin" --claude-bin GV_BENCH_CLAUDE || return 1
        [ -n "$model_claude" ] || { log "claude: --model-claude is required (results must name the model)"; return 1; } ;;
      codex|codex-visible) check_exec "Codex" "$codex_bin" --codex-bin GV_BENCH_CODEX || return 1
        [ -n "$model_codex" ] || { log "codex: --model-codex is required (results must name the model)"; return 1; } ;;
      opencode) check_exec "opencode" "$opencode_bin" --opencode-bin GV_BENCH_OPENCODE || return 1 ;;
    esac
  fi
  local version
  version=$(capture_version ${prefix[@]+"${prefix[@]}"} --version)
  eval "version_$avar=\$version"
  mset "agents.$agent.version" "$version"
  signout=$work/signin-$agent.txt
  if [ "$agent" = claude ] && [ "$claude_auth" = api_key_bare ]; then
    # --bare never reads the CLI's sign-in, so there is nothing to classify:
    # the environment key is the credential and every run is API billing.
    verdict='{"agent":"claude","kind":"api_key_env","ok":true,"billed":true,"detail":"ANTHROPIC_API_KEY set; runs use --bare and bill API credits"}'
    mjson agents.claude.signin "$verdict"
    log "claude: ANTHROPIC_API_KEY is set; runs use --bare and are billed to API credits (cap $claude_cap_usd USD)"
    signin_billed_claude=true
    claude_price_check || return 1
    return 0
  fi
  case $format in
    claude|codex)
      if [ "$agent" = claude ]; then
        ${prefix[@]+"${prefix[@]}"} auth status >"$signout" 2>&1 </dev/null || rc=$?
      else
        ${prefix[@]+"${prefix[@]}"} login status >"$signout" 2>&1 </dev/null || rc=$?
      fi
      local flags=()
      [ "$allow_api_billing" = 1 ] && flags+=(--allow-api-billing)
      [ "$accept_unconfirmed" = 1 ] && flags+=(--accept-unconfirmed-signin)
      if verdict=$(gv signin --agent "$format" --output "$(native_path "$signout")" --exit "$rc" ${flags[@]+"${flags[@]}"}); then
        rc=0
      else
        rc=1
      fi
      mjson "agents.$agent.signin" "$verdict"
      log "$agent sign-in: $verdict"
      case $verdict in
        *'"billed":true'*) eval "signin_billed_$avar=true" ;;
      esac
      [ "$rc" = 0 ] || return 1
      if [ "$agent" = codex-visible ]; then
        prepare_codex_visible || return 1
      fi
      if [ "$agent" = claude ] && [ "$signin_billed_claude" = true ]; then
        claude_auth=api_signin
        claude_price_check || return 1
      fi
      ;;
    opencode)
      if [ "$standin" != 1 ] && [ -z "${OPENROUTER_API_KEY:-}" ]; then
        log "opencode: OPENROUTER_API_KEY is not set"
        return 1
      fi
      local keyf=$work/key.json remaining
      key_read "$keyf" || { log "opencode: reading the OpenRouter key usage failed"; return 1; }
      remaining=$(gv keyusage --key-json "$(native_path "$keyf")" --field limit_remaining) || return 1
      mset "agents.opencode.key_limit_remaining_at_start" "$remaining"
      if [ "$remaining" = none ]; then
        log "opencode: the OpenRouter key has no credit limit; a dedicated key with a limit equal to the cap is the backstop (method.md)"
      fi
      if [ "$standin" = 1 ]; then
        mset agents.opencode.price_check "skipped (stand-in)"
      else
        local live=$work/endpoints-live.json pc=0
        "$curl" -sS --fail --max-time 30 -o "$live" "https://openrouter.ai/api/v1/models/$model_opencode/endpoints" 2>/dev/null || { rm -f "$live"; log "opencode: live price read failed; using the committed file only"; }
        if [ -f "$live" ]; then
          gv prices check --file "$pfile" --endpoints "$(native_path "$live")" || pc=1
          mset agents.opencode.live_prices_sha256 "$(gv sha256 "$(native_path "$live")")"
        else
          gv prices check --file "$pfile" || pc=1
        fi
        [ "$pc" = 0 ] || return 1
        mset agents.opencode.price_check "ok at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
      fi
      ;;
  esac
  return 0
}

# prepare_codex_visible writes the model catalog of the codex-visible agent:
# Codex's bundled catalog (`codex debug models --bundled`, no network) with
# supports_search_tool false for every model. Codex 0.171.0 defers all MCP
# tools behind tool search when the model supports it, and the feature that
# did this (tool_search_always_defer_mcp_tools) is "removed" and fixed on,
# so `--disable` cannot change it; the catalog override can.
prepare_codex_visible() {
  local raw=$work/codex-models-bundled.json
  if ! ${prefix[@]+"${prefix[@]}"} debug models --bundled >"$raw" 2>"$raw.stderr" </dev/null; then
    log "codex-visible: codex debug models --bundled failed: $(head -n 1 "$raw.stderr")"
    return 1
  fi
  gv codex-catalog --in "$(native_path "$raw")" --out "$(native_path "$codex_visible_catalog")" >/dev/null || return 1
  mset agents.codex-visible.variant "$codex_visible_variant"
  mset agents.codex-visible.catalog_sha256 "$(gv sha256 "$(native_path "$codex_visible_catalog")")"
  mset agents.codex-visible.bundled_catalog_sha256 "$(gv sha256 "$(native_path "$raw")")"
  return 0
}

# claude_price_check: an API-billed Claude run needs the Anthropic price
# file of the pinned model, or the cap could not book a missing cost.
claude_price_check() {
  if [ ! -f "$claude_prices" ]; then
    log "claude: API billing needs the price file $claude_prices (--claude-prices)"
    return 1
  fi
  cpfile=$(native_path "$claude_prices")
  if [ "$(gv prices show --file "$cpfile" --field model)" != "$model_claude" ]; then
    log "claude: $claude_prices is not the price file of $model_claude; the cap needs that model's prices"
    return 1
  fi
  [ -n "$claude_step_context" ] || claude_step_context=$(gv prices show --file "$cpfile" --field context)
  mset anthropic.auth_path "$claude_auth"
  mset anthropic.model "$model_claude"
  mjson anthropic.cap_usd "$claude_cap_usd"
  mjson anthropic.run_ceiling_usd "$claude_run_cap_usd"
  mjson anthropic.step_context_tokens "$claude_step_context"
  mjson anthropic.step_output_tokens "$claude_step_output"
  gv manifest --file "$manifest" --key anthropic.prices --value-file "$cpfile" --json
  return 0
}

for agent in $(printf '%s' "$agents" | tr ',' ' '); do
  case $agent in claude|codex|codex-visible|opencode) ;; *) die "unknown agent $agent" ;; esac
  if preflight_agent "$agent"; then
    active="$active $agent"
    mset "agents.$agent.preflight" ok
  else
    mset "agents.$agent.preflight" failed
    log "$agent: preflight failed; its runs are recorded as not_run_preflight"
  fi
done
mset agents.claude.model "$model_claude"
mset agents.claude.auth_path "$claude_auth"
mset agents.codex.model "$model_codex"
case ",$agents," in *,codex-visible,*) mset agents.codex-visible.model "$model_codex" ;; esac
mset agents.opencode.model "openrouter/$model_opencode"

# --- Graphvein identity --------------------------------------------------------
# record_graphvein_identity records what a reader of the published results
# can check: the full `graphvein version` output, the SHA-256 of the binary,
# and, for the commit the binary was built from (its version output names
# it; otherwise the driver checkout's HEAD), the extractor version and the
# Git tree id of the public snapshot of that commit. The private commit id
# is an internal reference only; the public commit with the same tree id
# is the public name of the code (method.md); public-tree.sh prints it.
record_graphvein_identity() {
  local vout="" vcommit="" commit
  if [ "$standin" != 1 ]; then
    vout=$("$graphvein_bin" version 2>&1 </dev/null) || vout=""
    mset graphvein.version_output "$(printf '%s' "$vout" | tr '\r\n' '  ' | sed 's/  */ /g; s/ $//')"
    mset graphvein.binary_sha256 "$(gv sha256 "$(native_path "$graphvein_bin")")"
    vcommit=$(printf '%s\n' "$vout" | sed -n 's/^commit: *//p' | head -n 1 | tr -d '\r')
  else
    mset graphvein.binary_sha256 "not used (stand-in)"
  fi
  commit=""
  if [ -n "$vcommit" ] && [ "$vcommit" != unknown ]; then
    commit=$("$git" -C "$repo" rev-parse --verify --quiet "$vcommit^{commit}" 2>/dev/null) || commit=""
  fi
  [ -n "$commit" ] || commit=$("$git" -C "$repo" rev-parse --verify --quiet HEAD 2>/dev/null) || commit=""
  if [ -z "$commit" ]; then
    mset graphvein.identity.public_tree "unknown (no git checkout)"
    return 0
  fi
  mset graphvein.build_commit "$commit"
  local lt=$work/public-tree.ls-tree ex=$work/public-tree.exclude tree ev
  if "$git" -C "$repo" ls-tree -r -z "$commit" >"$lt" 2>/dev/null &&
    "$git" -C "$repo" show "$commit:scripts/public-exclude.txt" >"$ex" 2>/dev/null &&
    tree=$(gv public-tree --ls-tree "$(native_path "$lt")" --exclude "$(native_path "$ex")"); then
    mset graphvein.identity.public_tree "$tree"
  else
    mset graphvein.identity.public_tree "unknown (computing the public tree of $commit failed)"
  fi
  rm -f "$lt" "$ex"
  ev=$("$git" -C "$repo" show "$commit:internal/index/index.go" 2>/dev/null | sed -n 's/^const Version = "\(.*\)"$/\1/p' | head -n 1) || ev=""
  mset graphvein.identity.extractor_version "${ev:-unknown}"
}

case ",$modes," in
  *,mcp,*)
    if [ "$standin" != 1 ]; then
      check_exec graphvein "$graphvein_bin" --graphvein-bin GV_BENCH_GRAPHVEIN || die "mcp mode needs graphvein to build the index"
      mset graphvein.version "$(capture_version "$graphvein_bin" version)"
    fi
    [ -n "$mcp_command" ] || mcp_command=${graphvein_bin:-graphvein}
    mset graphvein.mcp_command "$mcp_command ${mcp_args[*]}"
    record_graphvein_identity
    ;;
esac

# --- Workspaces and index -------------------------------------------------------
corpora=""
for q in $(printf '%s' "$questions" | tr ',' ' '); do
  c=$(gv question --questions "$qfile" --id "$q" --field corpus)
  case " $corpora " in *" $c "*) ;; *) corpora="$corpora $c" ;; esac
done

ws_of() { printf '%s\n' "$work/ws/$1"; }

prepare_workspace() {
  local c=$1 given="" checkout="" commit ws d
  case $c in
    fastapi) given=$workspace_fastapi; checkout=$corpus_fastapi ;;
    langflow) given=$workspace_langflow; checkout=$corpus_langflow ;;
    *) die "no workspace flags for corpus $c" ;;
  esac
  commit=$(gv question --questions "$qfile" --id "$(first_question_of "$c")" --field commit)
  if [ -n "$given" ]; then
    ws=$(abs_dir "$given")
    mset "corpora.$c.workspace" "prepared directory (--workspace-$c)"
  else
    [ -n "$checkout" ] || die "corpus $c: pass --corpus-$c DIR (a git checkout) or --workspace-$c DIR"
    ws=$work/ws/$c
    if [ ! -f "$work/ws/$c.digest" ]; then
      rm -rf "$ws"
      mkdir -p "$ws"
      "$git" -C "$checkout" cat-file -e "$commit^{commit}" 2>/dev/null || die "corpus $c: commit $commit is not in $checkout"
      log "exporting $c at $commit"
      # Symbolic links are left out of every workspace: Windows cannot create
      # them without developer mode (and dangling ones not at all), and
      # skipping them on every system keeps workspaces identical across
      # machines. The manifest records how many were skipped.
      skip=$work/ws/$c.symlinks
      "$git" -C "$checkout" ls-tree -r --full-tree "$commit" | awk -F '\t' '$1 ~ /^120000 / { print $2 }' >"$skip"
      "$git" -C "$checkout" archive --format=tar "$commit" | tar -x -X "$skip" -C "$ws" || die "exporting $c failed"
      mset "corpora.$c.symlinks_skipped" "$(wc -l <"$skip" | tr -d ' ')"
      gv readonly "$(native_path "$ws")"
      gv digest "$(native_path "$ws")" >"$work/ws/$c.digest"
    fi
    mset "corpora.$c.workspace" "git archive of the pinned commit, read-only"
  fi
  d=$(gv digest "$(native_path "$ws")")
  mset "corpora.$c.commit" "$commit"
  mset "corpora.$c.digest" "${d% *}"
  mset "corpora.$c.files" "${d#* }"
  eval "ws_$c=\$ws"
  eval "digest_$c=\${d% *}"
}

first_question_of() {
  local q
  for q in $(printf '%s' "$questions" | tr ',' ' '); do
    if [ "$(gv question --questions "$qfile" --id "$q" --field corpus)" = "$1" ]; then
      printf '%s\n' "$q"
      return 0
    fi
  done
}

index_corpus() {
  local c=$1 ws=$2 db=$work/store/$1.db t0 t1
  if [ "$standin" = 1 ]; then
    mset "corpora.$c.index" "not built (stand-in)"
    return 0
  fi
  if [ -f "$db" ]; then
    mset "corpora.$c.index" "reused $db"
    return 0
  fi
  log "indexing $c"
  t0=$(date +%s)
  GRAPHVEIN_SQLITE_PATH=$(native_path "$db") "$graphvein_bin" index "$(native_path "$ws")" --repository "$c" >"$work/store/$c.index.log" 2>&1 ||
    die "indexing $c failed; see $work/store/$c.index.log"
  t1=$(date +%s)
  mjson "corpora.$c.index_seconds" "$((t1 - t0))"
  mset "corpora.$c.index_peak_memory" "not measured by the script"
}

for c in $corpora; do
  prepare_workspace "$c"
  case ",$modes," in *,mcp,*) eval "index_corpus \"\$c\" \"\$ws_$c\"" ;; esac
done

timeout_ms=$(gv duration-ms "$timeout") || die "--timeout: invalid duration $timeout"

# --- Plan --------------------------------------------------------------------
plan=$work/plan.txt
plan_flags=(--agents "$agents" --modes "$modes" --ids "$questions" --reps "$reps" --seed "$seed")
[ -n "$agent_reps" ] && plan_flags+=(--agent-reps "$agent_reps")
mset plan.reps "$reps"
mset plan.agent_reps "${agent_reps:-none}"
[ "$shuffle" = 1 ] || plan_flags+=(--no-shuffle)
[ "$resume" = 1 ] && plan_flags+=(--done "$runs")
if [ -n "$rerun" ]; then
  plan_flags+=(--only "$rerun")
  mset plan.rerun "$rerun"
fi
gv plan ${plan_flags[@]+"${plan_flags[@]}"} >"$plan"
log "plan: $(wc -l <"$plan" | tr -d ' ') runs (agents:$active)"
if [ "$dry_run" = 1 ]; then
  cat "$plan"
  exit 0
fi

usage_start=0
case " $active " in
  *" opencode "*)
    if [ -f "$work/usage-start.txt" ]; then
      usage_start=$(cat "$work/usage-start.txt")
    else
      usage_start=$(key_usage) || die "reading the OpenRouter key usage failed"
      printf '%s\n' "$usage_start" >"$work/usage-start.txt"
    fi
    mset openrouter.usage_at_start "$usage_start"
    ;;
esac

stopped_rate=""
cap_stopped=0
claude_cap_stopped=0
standin_flag=()
[ "$standin" = 1 ] && standin_flag=(--standin)

harness_of() {
  case $1 in
    claude) printf '%s\n' "$harness_claude" ;;
    codex|codex-visible) printf '%s\n' "$harness_codex" ;;
    opencode) printf '%s\n' "$harness_opencode" ;;
  esac
}

version_of() {
  case $1 in
    claude) printf '%s\n' "$version_claude" ;;
    codex) printf '%s\n' "$version_codex" ;;
    codex-visible) printf '%s\n' "$version_codex_visible" ;;
    opencode) printf '%s\n' "$version_opencode" ;;
  esac
}

model_of() {
  case $1 in
    claude) printf '%s\n' "$model_claude" ;;
    codex|codex-visible) printf '%s\n' "$model_codex" ;;
    opencode) printf 'openrouter/%s\n' "$model_opencode" ;;
  esac
}

notrun() {
  # notrun AGENT REP MODE QID OUTCOME DETAIL
  gv notrun --out "$(native_path "$out")" --agent "$1" --harness "$(harness_of "$1")" \
    --harness-version "$(version_of "$1")" --model "$(model_of "$1")" --mode "$3" --question "$4" \
    --corpus "$(gv question --questions "$qfile" --id "$4" --field corpus)" --rep "$2" \
    --outcome "$5" --detail "$6" ${standin_flag[@]+"${standin_flag[@]}"} $(claude_row_flags "$1")
}

# claude_row_flags AGENT prints the auth-path and billing flags of a Claude
# row (words without spaces, so plain word splitting is safe).
claude_row_flags() {
  [ "$1" = claude ] || return 0
  if [ "$signin_billed_claude" = true ]; then
    printf -- '--auth-path %s --api-billing\n' "$claude_auth"
  else
    printf -- '--auth-path %s\n' "$claude_auth"
  fi
}

# budget_ok: may the next OpenRouter attempt start? Stops the agent (and
# records the decision) when its worst case no longer fits under the cap.
budget_ok() {
  local keyf=$work/key.json decision rc=0
  key_read "$keyf" || die "reading the OpenRouter key usage failed"
  decision=$(gv budget --prices "$pfile" --cap "$cap_usd" --run-ceiling "$run_cap_usd" \
    --step-context "$step_context" --step-output "$step_output" --usage-start "$usage_start" \
    --key-json "$(native_path "$keyf")" --runs "$runs") || rc=$?
  if [ "$rc" = 3 ]; then
    log "OpenRouter cap: $decision"
    mjson openrouter.stop_decision "$decision"
    cap_stopped=1
    return 1
  fi
  [ "$rc" = 0 ] || die "budget check failed"
  return 0
}

# claude_budget_ok: may the next API-billed Claude attempt start? The same
# rule as the OpenRouter cap: the per-run ceiling plus two model calls at the
# full bound must fit in what is left of the Claude cap; spent is the sum of
# every billed Claude attempt recorded so far.
claude_budget_ok() {
  local decision rc=0
  decision=$(gv budget --agent claude --prices "$cpfile" --cap "$claude_cap_usd" --run-ceiling "$claude_run_cap_usd" \
    --step-context "$claude_step_context" --step-output "$claude_step_output" --runs "$runs") || rc=$?
  if [ "$rc" = 3 ]; then
    log "Claude cap: $decision"
    mjson anthropic.stop_decision "$decision"
    claude_cap_stopped=1
    return 1
  fi
  [ "$rc" = 0 ] || die "Claude budget check failed"
  return 0
}

# kill_tree PID ends a run's process and everything it started. The run was
# started under `set -m`, so on macOS and Linux it leads its own process
# group; in Git Bash the native process tree is ended with taskkill /T.
kill_tree() {
  local pid=$1 winpid
  if [ -r "/proc/$pid/winpid" ] && command -v taskkill >/dev/null 2>&1; then
    winpid=$(cat "/proc/$pid/winpid")
    taskkill //T //F //PID "$winpid" >/dev/null 2>&1 || true
  fi
  kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
}

# run_cli TRANSCRIPT STDERR STDIN CEILING PRICES runs `cmd` in the workspace
# with `run_env` (and without the variables in `run_unset`), polls the
# deadline and (CEILING > 0: opencode, or Claude on API billing) the cost of
# the streamed calls at PRICES every 0.2 s, and kills the process tree on
# either. Sets cli_exit, cli_wall_ms, cli_started_at, cli_timed_out,
# cli_capped and cli_watched.
run_cli() {
  local trf=$1 erf=$2 inf=$3 ceiling=$4 wprices=${5:-} t0 t1 deadline pid verdict state endf rc=0
  state=$trf.watch
  endf=$trf.end
  rm -f "$state" "$endf"
  : >"$trf"
  cli_started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  cli_timed_out=0
  cli_capped=0
  cli_watched=0
  t0=$(gv now-ms)
  deadline=$((t0 + timeout_ms))
  set -m
  (
    cd "$ws" || exit 97
    for e in ${run_unset[@]+"${run_unset[@]}"}; do unset "$e"; done
    for e in ${run_env[@]+"${run_env[@]}"}; do export "$e"; done
    export GV_BENCH_QUESTION_ID="$qid"
    rc=0
    ${cmd[@]+"${cmd[@]}"} <"$inf" >"$trf" 2>"$erf" || rc=$?
    "$gvbench" now-ms >"$endf"
    exit "$rc"
  ) &
  pid=$!
  set +m
  local watchflags=(--format "$agent" --transcript "$(native_path "$trf")" --state "$(native_path "$state")" --deadline-ms "$deadline")
  if [ "$ceiling" != 0 ]; then
    watchflags+=(--prices "$wprices" --run-ceiling "$ceiling")
  fi
  while kill -0 "$pid" 2>/dev/null; do
    verdict=$(gv watch ${watchflags[@]+"${watchflags[@]}"}) || verdict="ok 0"
    case $verdict in
      timeout*) cli_timed_out=1; kill_tree "$pid"; break ;;
      cost*) cli_capped=1; kill_tree "$pid"; break ;;
    esac
    sleep 0.2
  done
  wait "$pid" 2>/dev/null || rc=$?
  t1=$(gv now-ms)
  if [ -s "$endf" ] && [ "$cli_timed_out" = 0 ] && [ "$cli_capped" = 0 ]; then
    t1=$(cat "$endf")
  fi
  # One last poll prices the steps written after the previous one.
  verdict=$(gv watch ${watchflags[@]+"${watchflags[@]}"} --deadline-ms 0) || verdict="ok 0"
  cli_watched=${verdict#* }
  cli_exit=$rc
  cli_wall_ms=$((t1 - t0))
  rm -f "$state" "$endf"
}

run_one() {
  # run_one AGENT REP MODE QID: one run with up to two retries of an
  # invalid attempt. Prints the outcome line of the last attempt.
  local agent=$1 rep=$2 mode=$3 qid=$4 attempt=1 id result
  corpus=$(gv question --questions "$qfile" --id "$qid" --field corpus)
  eval "ws=\$ws_$corpus"
  eval "local digest_before=\$digest_$corpus"
  store=$work/store/$corpus.db
  local prompt=$work/prompts/$qid.txt
  [ -f "$prompt" ] || gv prompt --questions "$qfile" --id "$qid" >"$prompt"
  id=$agent-$mode-$(printf '%s' "$qid" | tr 'A-Z' 'a-z')-r$rep
  while :; do
    local tr=$work/transcripts/$id-a$attempt.jsonl
    local er=$work/transcripts/$id-a$attempt.stderr
    local cfg=$work/config/$id-a$attempt
    case $agent in
      claude) build_claude "$mode" "$cfg.json" ;;
      codex|codex-visible) build_codex "$mode" "$agent" ;;
      opencode) build_opencode "$mode" "$cfg.json" "$prompt" ;;
    esac
    local before_usage=0 watch_ceiling=0 watch_prices=""
    if [ "$agent" = claude ] && [ "$signin_billed_claude" = true ]; then
      if ! claude_budget_ok; then
        if [ "$attempt" = 1 ]; then
          notrun "$agent" "$rep" "$mode" "$qid" not_run_cap "Claude cap reached"
        fi
        printf 'not_run_cap\n' >"$work/last-result.txt"
        return 0
      fi
      watch_ceiling=$claude_run_cap_usd
      watch_prices=$cpfile
    fi
    if [ "$agent" = opencode ]; then
      if ! budget_ok; then
        if [ "$attempt" = 1 ]; then
          notrun "$agent" "$rep" "$mode" "$qid" not_run_cap "total cap reached"
        fi
        printf 'not_run_cap\n' >"$work/last-result.txt"
        return 0
      fi
      watch_ceiling=$run_cap_usd
      watch_prices=$pfile
      before_usage=$(key_usage) || before_usage=unknown
    fi
    local infile=/dev/null
    [ "$prompt_on_stdin" = 1 ] && infile=$prompt
    run_cli "$tr" "$er" "$infile" "$watch_ceiling" "$watch_prices"
    local after
    after=$(gv digest "$(native_path "$ws")")
    local recflags=(--agent "$agent" --mode "$mode" --question "$qid" --rep "$rep" --attempt "$attempt"
      --questions "$qfile" --transcript "$(native_path "$tr")" --stderr "$(native_path "$er")"
      --exit-code "$cli_exit" --wall-ms "$cli_wall_ms" --started-at "$cli_started_at" --watched-usd "$cli_watched"
      --workspace "$(native_path "$ws")" --digest-before "$digest_before" --digest-after "${after% *}"
      --out "$(native_path "$out")")
    [ "$standin" = 1 ] && recflags+=(--standin)
    [ "$cli_timed_out" = 1 ] && recflags+=(--timed-out)
    [ "$cli_capped" = 1 ] && recflags+=(--cost-capped)
    case $agent in
      claude) recflags+=(--harness "$harness_claude" --harness-version "$version_claude" --model "$model_claude" --auth-path "$claude_auth")
        if [ "$signin_billed_claude" = true ]; then
          recflags+=(--signin-billed --prices "$cpfile" --step-context "$claude_step_context" --step-output "$claude_step_output")
        fi ;;
      codex) recflags+=(--harness "$harness_codex" --harness-version "$version_codex" --model "$model_codex") ;;
      codex-visible) recflags+=(--harness "$harness_codex" --harness-version "$version_codex_visible" --model "$model_codex" --variant "$codex_visible_variant") ;;
      opencode)
        recflags+=(--harness "$harness_opencode" --harness-version "$version_opencode" --model "openrouter/$model_opencode" --prices "$pfile")
        local settled billed
        settled=$(key_settle)
        case $settled in
          unsettled*|*unknown*) ;;
          *) if [ "$before_usage" != unknown ]; then
               billed=$(float_sub "$settled" "$before_usage")
               recflags+=(--billed-usd "$billed" --cost-settled)
             fi ;;
        esac
        recflags+=(--providers "$(generation_providers "$tr")")
        ;;
    esac
    local rc=0
    result=$(gv record ${recflags[@]+"${recflags[@]}"}) || rc=$?
    if [ "$rc" = 4 ]; then
      die "an API key appeared in the output of $id; stopped without writing it (transcript kept in $work/transcripts; delete it)"
    fi
    [ "$rc" = 0 ] || die "recording $id failed"
    log "$id attempt $attempt: $result"
    if [ "${after% *}" != "$digest_before" ]; then
      die "the workspace of $corpus changed during $id; later runs would be invalid. Delete $work/ws and start again."
    fi
    case $result in
      invalid*) if [ "$attempt" -lt 3 ]; then attempt=$((attempt + 1)); continue; fi ;;
    esac
    printf '%s\n' "$result" >"$work/last-result.txt"
    return 0
  done
}

while read -r agent rep mode qid <&3; do
  case " $active " in
    *" $agent "*) ;;
    *) notrun "$agent" "$rep" "$mode" "$qid" not_run_preflight "preflight failed"; continue ;;
  esac
  case " $stopped_rate " in
    *" $agent "*) notrun "$agent" "$rep" "$mode" "$qid" not_run_rate_limit "an earlier run of this agent hit a rate limit; resume later with --resume"; continue ;;
  esac
  if [ "$agent" = claude ] && [ "$claude_cap_stopped" = 1 ]; then
    notrun "$agent" "$rep" "$mode" "$qid" not_run_cap "Claude cap reached"
    continue
  fi
  if [ "$agent" = opencode ]; then
    if [ "$cap_stopped" = 1 ]; then
      notrun "$agent" "$rep" "$mode" "$qid" not_run_cap "total cap reached"
      continue
    fi
  fi
  run_one "$agent" "$rep" "$mode" "$qid"
  case $(cat "$work/last-result.txt") in
    "agent_error rate_limit"*)
      stopped_rate="$stopped_rate $agent"
      log "$agent hit a rate limit; its remaining runs are recorded as not_run_rate_limit" ;;
  esac
done 3<"$plan"

mset finished_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
gv report --runs "$runs" --out "$(native_path "$out/summary.md")"
log "done: $out (transcripts in $work/transcripts, not committed)"
