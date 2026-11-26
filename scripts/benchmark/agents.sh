# Agent adapters of the benchmark driver; sourced by run.sh. Each function
# fills the global array `cmd` with the full argument vector of one run, and
# `run_env` with extra KEY=VALUE environment entries. No eval; arrays are
# expanded as ${arr[@]+"${arr[@]}"} so bash 3.2 with set -u accepts them
# when empty.
#
# Read-only settings per agent:
#   claude   --restricted, file tools Read/Grep/Glob only, dontAsk, no
#            prompts, strict MCP config (base mode: an empty server list).
#            On API billing (ANTHROPIC_API_KEY set) also --bare, which reads
#            only that key, plus --max-budget-usd and an output bound.
#
# Each CLI sees only its own API key: `run_unset` lists the variables the
# run's environment drops.
#   codex    read-only sandbox, user config and rules ignored, ephemeral,
#            approval policy never, web search off (value provisional).
#   codex-visible  codex (mcp mode only) with a model catalog whose models
#            do not offer tool search, so MCP tools are shown, not deferred.
#   opencode project and global config, stored logins and state left out
#            (XDG config, data and state homes in the work directory), edit/bash/webfetch
#            and paths outside the workspace denied, no permission left at "ask", file tools
#            and graphvein_* allowed, preferred OpenRouter endpoints first.

# agent_format AGENT: the CLI an agent id runs (codex-visible is Codex).
agent_format() {
  case $1 in
    codex-visible) printf 'codex\n' ;;
    *) printf '%s\n' "$1" ;;
  esac
}

# agent_prefix AGENT: the CLI itself, or the stand-in that imitates it.
agent_prefix() {
  case $1 in
    claude) bin=$claude_bin ;;
    codex|codex-visible) bin=$codex_bin ;;
    opencode) bin=$opencode_bin ;;
  esac
  if [ "$standin" = 1 ]; then
    prefix=("$gvbench" standin --format "$(agent_format "$1")" --)
  else
    prefix=("$bin")
  fi
}

# mcp_config_args: the agentconfig flags describing the MCP server for the
# current corpus. Placeholders {workspace}, {store} and {corpus} expand.
mcp_config_args() {
  local a
  mcpflags=(--mcp-command "$mcp_command"
    --var "workspace=$(native_path "$ws")"
    --var "store=$(native_path "$store")"
    --var "corpus=$corpus"
    --mcp-env "GRAPHVEIN_SQLITE_PATH={store}")
  for a in ${mcp_args[@]+"${mcp_args[@]}"}; do
    mcpflags+=(--mcp-arg "$a")
  done
}

build_claude() {
  local mode=$1 cfg=$2
  local allowed="Read Grep Glob"
  agent_prefix claude
  mcpflags=()
  if [ "$mode" = mcp ]; then
    mcp_config_args
    allowed="$allowed mcp__graphvein"
  fi
  gv agentconfig --format claude --mode "$mode" --out "$(native_path "$cfg")" ${mcpflags[@]+"${mcpflags[@]}"}
  cmd=(${prefix[@]+"${prefix[@]}"} -p
    --output-format stream-json --verbose
    --no-session-persistence
    --restricted
    --tools "Read,Grep,Glob"
    --allowedTools "$allowed"
    --permission-mode dontAsk
    --permission-prompts none
    --max-turns "$max_turns"
    --mcp-config "$(native_path "$cfg")" --strict-mcp-config)
  if [ -n "$model_claude" ]; then
    cmd+=(--model "$model_claude")
  fi
  run_env=()
  run_unset=(OPENROUTER_API_KEY)
  if [ "$claude_auth" = api_key_bare ]; then
    # --bare skips hooks, plugins, auto-memory, keychain and OAuth and
    # CLAUDE.md auto-discovery; --add-dir names the workspace so its own
    # CLAUDE.md still loads, as in the subscription path (provisional).
    cmd+=(--bare --add-dir "$(native_path "$ws")")
  else
    run_unset+=(ANTHROPIC_API_KEY)
  fi
  if [ "$signin_billed_claude" = true ]; then
    cmd+=(--max-budget-usd "$claude_run_cap_usd")
    run_env+=("CLAUDE_CODE_MAX_OUTPUT_TOKENS=$claude_step_output")
  fi
  prompt_on_stdin=1
}

# build_codex MODE [AGENT]: AGENT codex-visible adds the model catalog
# without the search tool (prepare_codex_visible), so Codex shows the
# graphvein tools to the model instead of deferring them behind tool search.
build_codex() {
  local mode=$1 agent=${2:-codex} overrides line
  agent_prefix codex
  mcpflags=()
  if [ "$mode" = mcp ]; then
    mcp_config_args
  fi
  overrides=$work/codex-overrides.txt
  gv agentconfig --format codex --mode "$mode" ${mcpflags[@]+"${mcpflags[@]}"} ${codex_config[@]+"${codex_config[@]}"} >"$overrides"
  cmd=(${prefix[@]+"${prefix[@]}"} exec --json
    --sandbox read-only
    --ignore-user-config --ignore-rules
    --ephemeral --skip-git-repo-check
    -C "$(native_path "$ws")")
  # Codex 0.171.0 enables built-in tool sources that --ignore-user-config
  # does not remove: the ChatGPT apps MCP server (codex_apps), plugins,
  # browser and computer use, image generation, sub-agents and tool
  # suggestions. Both modes run without them, so the only MCP server in
  # MCP mode is Graphvein's and base mode has none. Names from
  # `codex features list` of that version.
  for line in apps plugins remote_plugin browser_use browser_use_external computer_use in_app_browser image_generation multi_agent tool_suggest; do
    cmd+=(--disable "$line")
  done
  # On Windows, Codex 0.171.0 runs commands only inside a configured Windows
  # sandbox; without one, read-only mode rejects every command ("blocked by
  # policy") and the agent cannot read the workspace. The unelevated
  # restricted-token sandbox needs no administrator setup.
  case $(uname -s) in
    MINGW*|MSYS*|CYGWIN*) cmd+=(-c 'windows.sandbox="unelevated"') ;;
  esac
  if [ -n "$model_codex" ]; then
    cmd+=(-m "$model_codex")
  fi
  while IFS= read -r line || [ -n "$line" ]; do
    [ -n "$line" ] && cmd+=(-c "$line")
  done <"$overrides"
  if [ "$agent" = codex-visible ]; then
    cmd+=(-c "model_catalog_json=\"$(native_path "$codex_visible_catalog")\"")
  fi
  cmd+=(-)
  run_env=()
  # Codex's npm launcher puts its bundled ripgrep (vendor/<target>/codex-path)
  # on PATH; the driver runs the native binary directly, so it does the same.
  # Without it `rg` does not exist in Codex's shell.
  if [ "$standin" != 1 ] && [ -d "$(dirname "$bin")/../codex-path" ]; then
    run_env+=("PATH=$(cd "$(dirname "$bin")/../codex-path" && pwd):$PATH")
  fi
  run_unset=(OPENROUTER_API_KEY ANTHROPIC_API_KEY)
  prompt_on_stdin=1
}

build_opencode() {
  local mode=$1 cfg=$2 prompt=$3
  agent_prefix opencode
  mcpflags=()
  if [ "$mode" = mcp ]; then
    mcp_config_args
  fi
  gv agentconfig --format opencode --mode "$mode" --out "$(native_path "$cfg")" \
    --model "$model_opencode" --steps "$max_turns" --preferred "$preferred_endpoints" \
    --step-output "$step_output" --context "$step_context" ${mcpflags[@]+"${mcpflags[@]}"}
  cmd=(${prefix[@]+"${prefix[@]}"} run --format json --pure
    -m "openrouter/$model_opencode"
    --dir "$(native_path "$ws")"
    "$(cat "$prompt")")
  # An empty XDG config home keeps the developer's global opencode config
  # (and any MCP servers it lists) out of both modes. The driver's own data
  # and state homes (in the work directory, never logged in) keep out a
  # login stored by `opencode auth login` (auth.json in the data directory,
  # which opencode prefers over OPENROUTER_API_KEY) and the developer's
  # sessions and recent-model state, so the run bills only the key the
  # driver passes. The cache (provider packages, model catalogue, no
  # credentials) stays shared so runs do not download it again.
  mkdir -p "$work/opencode-xdg-config" "$work/opencode-xdg-data" "$work/opencode-xdg-state"
  run_env=("OPENCODE_CONFIG=$(native_path "$cfg")"
    "XDG_CONFIG_HOME=$(native_path "$work/opencode-xdg-config")"
    "XDG_DATA_HOME=$(native_path "$work/opencode-xdg-data")"
    "XDG_STATE_HOME=$(native_path "$work/opencode-xdg-state")"
    OPENCODE_DISABLE_PROJECT_CONFIG=1
    OPENCODE_DISABLE_AUTOUPDATE=1)
  run_unset=(ANTHROPIC_API_KEY)
  prompt_on_stdin=0
}
