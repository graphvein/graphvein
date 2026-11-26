# Agent benchmark: method

This benchmark measures whether coding agents answer questions about a
codebase more correctly, and with fewer tokens, tool calls and less time,
when the Graphvein MCP server is attached. Every agent answers the same
frozen questions twice: once with its own file tools only (`base` mode) and
once with the Graphvein MCP server added (`mcp` mode). Answers are graded by
a script against facts verified from the source code. No language model
judges any answer.

The driver is [`scripts/benchmark/run.sh`](../../../scripts/benchmark/run.sh)
with its Go helper `scripts/benchmark/gvbench`. How to run it:
[README.md](README.md).
Results of the first full run:
[results-2026-11-24.md](results-2026-11-24.md).

## Agents, harnesses and models

| Agent id | Harness | Model | Billing |
|---|---|---|---|
| `claude` | Claude Code (`claude -p`) | `claude-opus-5-5` (default of `--model-claude`) | API billing: `ANTHROPIC_API_KEY` set, runs with `--bare`, cost = Claude Code's reported `total_cost_usd`, hard cap below. Without the key: subscription sign-in, and the cost column is the CLI's own list-price estimate, not a bill |
| `codex` | Codex CLI (`codex exec`) | `gpt-6.2-sol` (default of `--model-codex`, from the Codex CLI 0.171.0 model catalog; fallback `gpt-6.1-sol`) | ChatGPT sign-in; the CLI reports no cost |
| `codex-visible` | Codex CLI (`codex exec`), mcp mode only | as `codex` | as `codex`; a variant that shows Codex the Graphvein tools (see "Agent settings") |
| `opencode` | opencode (open source, MIT) | `deepseek/deepseek-v4.1-flash-1017` on OpenRouter | billed per token by OpenRouter; hard cap below |

Naming rules for every published result:

- A result always names the agent, the harness with the exact version its
  `--version` printed, and the model id. "Claude" or "the open model" alone
  is never enough; write for example "Claude Code 2.1.330, model X" or
  "opencode 1.19.6, `openrouter/deepseek/deepseek-v4.1-flash-1017`".
- For the OpenRouter model, a result also names the provider, endpoint and
  quantization that served it, as recorded per run (see "Serving provider").
  The preferred endpoint is DeepInfra fp8 (`deepinfra/fp8`), then Pearl
  Research fp8 (`pearl-research/fp8`). Runs with evidence of another
  endpoint are flagged `served_elsewhere` and listed in the summary; runs
  without evidence either way are marked `provider_unverified`.
- Every Claude row records its `auth_path` (`api_key_bare`, `subscription`,
  or `api_signin` for an API-key sign-in of the CLI itself) and its
  `billing` ("API billing" or "subscription"), and the credential Claude
  Code's init event names (`api_key_source`). Results mark API-billed Claude
  runs as "API billing" with their real cost.
- A Claude Code or Codex sign-in that is itself an API key (not the
  `ANTHROPIC_API_KEY` path) is refused unless the run passes
  `--allow-api-billing`; such Claude runs are API billing under the Claude
  cap as well.

## Questions and corpora

[`questions.jsonl`](questions.jsonl) holds 21 rows, BQ-1 to BQ-21, frozen:
the second benchmark point reuses the same rows and corpus commits so the
two points compare. Each row has the question, the answer schema, the
expected answer, the evidence it was verified from and its grading rule.

- 11 rows on the FastAPI full-stack template at
  `1762adac607a1b29cfc4da129557780beea71616`, 10 on langflow at
  `504c02fc47e76087b82b0e7cbe4186e9cdd916d4`.
- Every expected answer rests on the source code (and, where cheap, an
  independent oracle such as a generated client or an OpenAPI document),
  never on Graphvein output. Every row is answerable from the source alone,
  so the comparison is fair to `base` mode.
- Kinds: route to handler, route inventory, route to callers, change
  impact, table schema and table writers, and reviewer questions
  (conditional routes, security dependency, unresolved calls). Two rows
  expect an empty answer (BQ-7, BQ-17) and one has a misleading hook name
  (BQ-19).
- **Corrections.** "Frozen" means the questions and corpus commits do not
  change. An expected answer that is shown to be wrong against the pinned
  source is corrected in place, and the row's `correction` field records the
  date and the reason (BQ-18, 2026-11-24: a missed call site). Runs already
  recorded are then graded again from their committed answer files:
  `gvbench regrade --runs-dir <runs/label> --questions <file> --note TEXT`
  (README) rewrites `correct`, `score` and `grade` of each row whose grade
  changes, keeps the earlier grade in `grade_v1`, records the note and the
  question file's SHA-256 in `regraded`, and regenerates `summary.md`, which
  then lists the re-graded runs. Other rows are left byte for byte, and
  `--check` lists the changes without writing. Grading is deterministic, so
  the same command gives the same result anywhere.

## Workspace

- Per corpus the driver exports the pinned commit with `git archive` into a
  work directory outside any repository and removes write permission
  (on Windows the read-only attribute of every file). There is no `.git`
  directory, so history is not available in either mode.
- The corpus's own agent instruction files (langflow ships `AGENTS.md` and
  `CLAUDE.md`) are kept: realistic, and identical in both modes. Claude
  Code's `--bare` turns off `CLAUDE.md` auto-discovery, so on API billing the
  driver names the workspace with `--add-dir`, which `--bare` documents as
  the way to load a directory's `CLAUDE.md` (provisional, see below).
- A digest (SHA-256 over the sorted file list and every file's content) is
  taken before the first run and after every run. A change marks the run
  `invalid` and stops the benchmark, since every later run would see a
  modified workspace.

## Graphvein index and MCP server

The index is built once per corpus before any run, with
`graphvein index <workspace> --repository <corpus>` and
`GRAPHVEIN_SQLITE_PATH` pointing at a store outside the workspace, so file
tools cannot read it. Its wall time is recorded in the manifest and never
added to per-question time. In `mcp` mode each agent starts the MCP server
itself over stdio from its own configuration; the command (default
`graphvein mcp`) and its arguments come from the driver's flags and are
passed through unchanged, with `GRAPHVEIN_SQLITE_PATH` set to the corpus
store.

## Prompt

Identical for every agent and both modes; the only difference between modes
is the attached server:

```
You are answering a question about the repository in the current directory. Do not modify files.
Use any tools available to you.

<question>

End your reply with exactly one fenced ```json block containing an object with these keys and value shapes: <answer schema>
Write file paths relative to the repository root, and routes as the HTTP method followed by the full path including every router prefix.
```

`gvbench prompt --questions docs/evidence/benchmark/questions.jsonl --id BQ-n`
prints the exact text of a row.

## Agent settings

Every run is a fresh process and a fresh session, run in the workspace,
read-only, with no permission prompt possible:

- **Claude Code:** `-p --output-format stream-json --verbose
  --no-session-persistence --restricted --tools Read,Grep,Glob
  --allowedTools "Read Grep Glob [mcp__graphvein]" --permission-mode dontAsk
  --permission-prompts none --max-turns 60 --mcp-config <file>
  --strict-mcp-config`; the prompt on standard input. In `base` mode the MCP
  config lists no server, so no other MCP source loads. On API billing
  (`ANTHROPIC_API_KEY` set) also `--bare --add-dir <workspace>
  --max-budget-usd <per-run ceiling>` and the environment variable
  `CLAUDE_CODE_MAX_OUTPUT_TOKENS=<per-call output bound>`. `--bare` skips
  hooks, plugins, auto-memory, keychain reads and OAuth, and authenticates
  strictly with `ANTHROPIC_API_KEY`, so the machine's own Claude Code setup
  stays out of the run. Without the key the run uses the CLI's sign-in and
  none of these flags, as before.
- **Keys per CLI:** each run's environment drops the keys of the other
  harnesses: Claude runs never see `OPENROUTER_API_KEY` (and, on the
  subscription path, no `ANTHROPIC_API_KEY`), Codex runs see neither, and
  opencode runs never see `ANTHROPIC_API_KEY`.
- **Codex:** `exec --json --sandbox read-only --ignore-user-config
  --ignore-rules --ephemeral --skip-git-repo-check -C <workspace> -m <model>
  -c approval_policy="never" -c web_search="disabled"` and `--disable` for
  the built-in tool sources that the user configuration flag does not cover
  (`apps`, i.e. the ChatGPT apps MCP server `codex_apps`, `plugins`,
  `remote_plugin`, `browser_use`, `browser_use_external`, `computer_use`,
  `in_app_browser`, `image_generation`, `multi_agent`, `tool_suggest`), plus
  the `mcp_servers.graphvein.*` overrides in `mcp` mode; the prompt on
  standard input. The pilot found Codex calling `codex_apps` in base mode
  without these flags; such a run is recorded as invalid, never graded. On
  Windows the driver adds `-c windows.sandbox="unelevated"` (without a
  configured Windows sandbox, read-only mode rejects every command) and puts
  Codex's bundled ripgrep (`codex-path`) on PATH, as Codex's own launcher
  does. Codex reads files through shell commands inside its read-only
  sandbox.
- **Codex, `codex-visible` variant (mcp mode only).** Codex 0.171.0 runs
  `gpt-6.2-sol` in code mode and, when the model supports tool search (its
  bundled model catalog says so), defers every MCP tool behind that search:
  the model sees no Graphvein tool unless it searches for one. The feature
  that does this, `tool_search_always_defer_mcp_tools`, is listed by `codex
  features list` as "removed" and fixed on, so `--disable` does not change
  it. The variant therefore writes Codex's own bundled catalog (`codex debug
  models --bundled`, no network) with `supports_search_tool` false for every
  model and passes it with `-c model_catalog_json="<file>"`; with tool search
  unsupported, Codex exposes the MCP tools directly. Every other setting is
  the `codex` agent's. Rows carry agent `codex-visible` and a `variant`
  field, the manifest the catalog's SHA-256; the comparison base is the
  `codex` agent's base runs. That the variant shows the tools rests on the
  Codex 0.171.0 source; confirmed by round 2 of 2026-11-24, where the variant
  called Graphvein in 44 of 63 runs.
- **opencode:** `run --format json --pure -m openrouter/<model> --dir
  <workspace> "<prompt>"` with a generated config (`OPENCODE_CONFIG`;
  project config disabled; an empty `XDG_CONFIG_HOME`, so the machine's
  global opencode config and its MCP servers stay out of both modes; its own
  `XDG_DATA_HOME` and `XDG_STATE_HOME` in the work directory, so a login stored by `opencode auth
  login` is never used and only `OPENROUTER_API_KEY` pays): `edit`, `bash` and `webfetch` and `websearch` denied; `read`,
  `grep`, `glob`, `list` and `graphvein_*` allowed; `external_directory`
  denied (a path outside the workspace is refused; opencode still allows its
  own tool-output directory) and `doom_loop` allowed, so no permission is
  left at "ask", which `opencode run` rejects with a failed tool call (the
  cause of the ten invalid opencode attempts of the first full run); 60 steps; OpenRouter
  routing with the preferred endpoints first and no fallback; output per
  call limited to 32,768 tokens; the MCP server with a 60 s timeout in
  `mcp` mode.

## Limits and outcomes

- Wall-clock timeout 10 minutes per run; the driver kills the process tree.
- Turn limit 60 (Claude `--max-turns`, opencode steps); Codex has none
  besides the timeout.
- OpenRouter runs and API-billed Claude runs also stop at their per-run
  ceiling (below).

| Outcome | Meaning | Graded |
|---|---|---|
| `answered` | the run ended and its final message was graded | yes |
| `no_answer` | the final message has no parsable ```json object | scores 0 |
| `timeout` | killed at the wall-clock limit | scores 0 |
| `turn_limit` | the CLI reported its turn limit | scores 0 |
| `cost_cap` | killed at the per-run ceiling | scores 0 |
| `agent_error` | non-zero exit or a CLI-reported error; `error_kind` `rate_limit` stops the agent's remaining runs | scores 0 |
| `invalid` | workspace modified; MCP server not connected in `mcp` mode (where the CLI reports it); an MCP server present in `base` mode; a web tool used; a configured tool denied (the row's `detail` keeps the first denial's error text, paths shortened; an opencode path refused by the `external_directory` rule is expected and does not count) | retried at most twice |
| `not_run_cap` | never started: the total cap could not cover its worst case | not a failure |
| `not_run_rate_limit` | never started: the agent hit a rate limit earlier; `--resume` continues later | not a failure |
| `not_run_preflight` | never started: the CLI or its sign-in failed the preflight | not a failure |

Preflight, before anything runs: each CLI path is given by a flag or an
environment variable and never looked up on `PATH`; its `--version` is
recorded; Claude's `claude auth status` and Codex's `codex login status`
are classified (subscription, API key, not signed in, or "unconfirmed" when
the output cannot be placed, which stops the agent unless the run passes
`--accept-unconfirmed-signin`). A failure stops that agent only.

## Metrics

| Metric | Claude Code | Codex | opencode |
|---|---|---|---|
| Input tokens (uncached) | `usage.input_tokens` | `input_tokens` − `cached_input_tokens` | Σ `tokens.input` over steps |
| Cache read | `usage.cache_read_input_tokens` | `cached_input_tokens` | Σ `tokens.cache.read` |
| Cache write | `usage.cache_creation_input_tokens` | not reported | Σ `tokens.cache.write` |
| Output | `usage.output_tokens` (thinking separately where reported) | `output_tokens` (+ `reasoning_output_tokens` separately) | Σ `tokens.output` (+ `tokens.reasoning` separately) |
| Cost | `total_cost_usd`: the run's API cost on API billing (booked at a worst case when missing, see below); a list-price estimate, not billed, on a subscription | none | billed: the OpenRouter key's usage delta over the run; opencode's own estimate kept as `harness_cost_usd` |
| Tool calls | `tool_use` blocks; MCP = `mcp__graphvein__*` | completed tool items; MCP = `mcp_tool_call` on `graphvein` | `tool_use` events; MCP = `graphvein_*` |
| Turns | `num_turns` | `turn.completed` events | `step_finish` events |
| Wall time | measured by the driver from process start to exit, the same way for every agent | same | same |

**MCP result sizes.** For every MCP tool result the transcript shows, the
row records the bytes the agent received (`mcp_result_bytes`), split into a
text copy and a structured copy, and counts results that carried both
(`mcp_both_copies`). A structured copy is a `structured_content` field, a
non-text content block, or, in a text-only transcript, a JSON-object text
block next to a plain-text block. This shows whether a client feeds a model
both copies of the same result.

A metric a CLI does not report stays 0 and is named in the row's
`unreported` list, so a zero is never read as a measurement.

## Grading

The grader reads the last ```json block of the final message. Atoms are
normalised before matching:

- routes `"METHOD /path"`: method upper-cased, whitespace collapsed, every
  `{name}` path parameter compared as `{}`; the trailing slash is
  significant (FastAPI distinguishes);
- locations `"path:line"`: backslashes become `/`, a leading `./` and a
  leading workspace path are removed;
- columns `"table.column"`: lower-cased; foreign keys `"table.column ->
  table.column ON DELETE ACTION"`: identifiers lower-cased, action
  upper-cased, whitespace collapsed; conditions `"SETTING == value"`:
  quotes removed, whitespace collapsed;
- pairs `"METHOD /path @ path:line"`: route part as routes, location part as
  locations.

Matching is one-to-one. Call-site locations match when the file is equal
and the lines differ by at most 2; a handler matches when its line is within
the row's decorator-to-`def` range. Each key gets precision, recall and F1;
an empty expected set is matched only by an empty answer; a missing key, a
wrong value type or a duplicate atom counts against the answer. A run is
**correct only when every key has F1 = 1**; the mean key F1 is reported as
the score alongside.

## Repetitions and order

Three repetitions per agent by default. An agent run on a subscription
can be given fewer (`--agent-reps codex=1`) to stay within that plan's
usage limits; the manifest records `plan.reps` and `plan.agent_reps`, the
summary shows how many runs of each agent started, and the results page
states the counts. More repetitions of that agent can be added later with
`--resume` and a higher count; runs already recorded are skipped.
Agents run one after another, never
concurrently. Within a repetition, all (question, mode) pairs of the agent
run in a seeded shuffle (the seed is in the manifest), so the two modes of a
question are not always adjacent and rate-limit or time-of-day effects fall
on both modes. The summary reports correctness as k/n per row and the
median (min-max) of every metric per agent and mode.

## OpenRouter cost control

- The key comes only from the `OPENROUTER_API_KEY` environment variable. It
  is never accepted as a flag, never written to a file or log, and sent to
  `curl` as a header on standard input, so it does not appear in a process
  list. Before any row, answer, manifest or summary is written, the helper
  scans it and the run's transcript for the key value and refuses the write
  if it is found.
- Prices come from a committed price file
  ([`scripts/benchmark/prices/deepseek-v4.1-flash-1017.json`](../../../scripts/benchmark/prices/deepseek-v4.1-flash-1017.json))
  read on 2026-11-23 from
  `https://openrouter.ai/api/v1/models/deepseek/deepseek-v4.1-flash-1017/endpoints`
  and `https://openrouter.ai/api/v1/models`. It lists every endpoint's
  price and quantization; the bound used for the cap is the highest price
  over all tool-capable endpoints and time-of-day surcharges, before any
  discount: $0.60 per million input tokens and $2.40 per million output
  tokens (DeepInfra fp8 lists $0.098 / $0.294, Pearl Research fp8
  $0.05 / $0.30, the model's listed price $0.15 / $0.60). At the start of a
  run the endpoint prices are read again; the run refuses to start if any
  live price is above the bound, or if the file is older than 7 days and the
  live read failed.
- Spend is read from `GET https://openrouter.ai/api/v1/key` before the first
  run and before every attempt. Spent = the larger of the key's usage since
  the start and the sum of the costs recorded for all attempts so far.
- **Total cap $8.** An attempt starts only if its worst case fits in what is
  left: `ceiling + 2 × W(C, O)`, where `W(C, O)` is one model call with a
  full context window `C` (1,048,576 tokens) and the per-call output limit
  `O` (32,768 tokens) at the bound prices with no caching ($0.708). The two
  calls cover the call whose result crosses the ceiling and one more that
  may already be in flight when the driver kills the process. So an attempt
  starts only while at least $1.87 is left. Planned runs that cannot start
  are recorded as `not_run_cap` and reported as "not run (cap)", never as
  failures.
- **Per-run ceiling $0.45.** While a run streams, the driver prices every
  finished step (every 0.2 s) and kills the process tree once the total
  reaches the ceiling (outcome `cost_cap`). A step is priced at its reported
  tokens: uncached input and cache writes at the highest input price
  ($0.60 per million), cache reads at the highest cache-read price of any
  tool-capable endpoint, raised by that endpoint's time-of-day surcharge
  where one is listed ($0.03 per million, against $0.003 listed for the
  model), and output plus reasoning at the highest output price ($2.40). An
  endpoint bills a cache hit at its own cache-read price, so this remains an
  upper bound of the real cost. Until 2026-11-24 every step was priced with
  no caching at all; an agent re-sends its whole conversation each step,
  mostly as cache reads, so that bound reached $0.45 on runs that cost
  $0.003-0.006 and stopped five BQ-21 runs of the first full run. The bound
  assumes no full model call completes within one 0.2 s poll.
- The total cap is unchanged and does not rely on caching: an attempt still
  starts only if the ceiling plus two full-context calls with no caching
  ($1.87) fit in what is left, so a run can cost at most the ceiling plus
  those two calls whatever its cache rate. A run whose usage does not settle
  is still booked with no caching.
- After a run the key usage is read until two reads 5 s apart agree (at most
  60 s); the difference is the run's billed cost (`cost_kind` `billed`). If
  it does not settle, the run is booked at its worst case
  (`worst_case_unsettled`), so the cap stays conservative.
- Backstop outside the script: a dedicated OpenRouter key with a credit
  limit equal to the cap. The preflight reports a key without a limit; a
  key's own remaining limit, when lower, replaces the cap's remainder.

### Serving provider

Each OpenRouter row records `serving.provider`, `serving.endpoint`,
`serving.quantization` and the evidence they rest on:

- `provider_reported`: the transcript, or OpenRouter's generation record
  for a generation id found in the transcript, names the provider;
- `price_match`: the billed cost reproduces, within 3%, the cost of the
  run's tokens at one or more endpoints' prices (listed or discounted); the
  matching endpoints are listed;
- `none`: no evidence; every field is "unconfirmed".

`served_elsewhere` is set when a reported provider is not a preferred
endpoint's, or when price evidence matches no preferred endpoint.

## Claude API cost control

Applies when `ANTHROPIC_API_KEY` is set (or the CLI's own sign-in is an API
key allowed with `--allow-api-billing`).

- The key comes only from the `ANTHROPIC_API_KEY` environment variable. It
  is never accepted as a flag, never written to a file or log, and handled
  exactly like the OpenRouter key: error text is redacted, and the helper
  refuses to write any row, answer, manifest or summary whose data, or
  whose run's transcript or standard error, contains the key value (the
  run then stops).
- Prices come from a committed price file
  ([`scripts/benchmark/prices/claude-opus-5-5.json`](../../../scripts/benchmark/prices/claude-opus-5-5.json))
  read on 2026-11-23 from
  `https://platform.claude.com/docs/en/about-claude/pricing`: $4 per million
  input tokens, $20 output, $0.20 cache read. Cache writes cost 1.25x input
  with the 5-minute TTL and 2x with the 1-hour TTL; a transcript does not
  say which TTL was used, so every cache write is booked at the higher 2x
  ($8). Fast mode and US-only inference cost more and are never enabled by
  the driver. A run refuses to start if the price file is not the pinned
  model's.
- **Booked cost of a run.** Claude Code's own `total_cost_usd` from the
  result object (`cost_kind` `api_billed`). When the field is missing, the
  run is booked at a worst case (`api_billed_worst_case`): the larger of the
  result's token totals and the per-call usage of the streamed assistant
  messages, priced as above (thinking tokens are added to output, which can
  only overcount); a run that ended without a result object (killed at the
  timeout or the ceiling) books two more calls at the full bound below, the
  call in flight when it was killed and one more.
- **Total cap $60** (`--claude-cap-usd`, `GV_BENCH_CLAUDE_CAP_USD`). Spent is
  the sum of the booked costs of every API-billed Claude attempt so far,
  superseded invalid attempts included. An attempt starts only if `ceiling
  + 2 × W(C, O)` fits in what is left, the same rule as the OpenRouter cap,
  where `W(C, O)` is one call with a full context `C` (1,000,000 tokens,
  `--claude-step-context`) booked at the cache-write price ($8 per million)
  and the per-call output bound `O` (32,768 tokens, `--claude-step-output`)
  at $20 per million: $8.655. With the default ceiling of $3 an attempt
  starts only while at least $20.31 is left. Planned runs that cannot start
  are recorded as `not_run_cap` and reported as "not run (cap)", never as
  failures.
- **Per-run ceiling $3** (`--claude-run-cap-usd`,
  `GV_BENCH_CLAUDE_RUN_CAP_USD`). Claude Code receives it as
  `--max-budget-usd`; independently, the driver prices every finished
  assistant message of the stream every 0.2 s and kills the process tree
  once the total reaches the ceiling (outcome `cost_cap`).
- The summary's "Spend" table lists the API-billed Claude runs and their
  total cost.
- Backstop outside the script: a dedicated API key in its own Console
  workspace with a spending limit (README).

## Graphvein identity

Results are produced in Graphvein's private development repository, whose
commit ids never become public: the public repository is built from
snapshots of it (`scripts/public-snapshot.sh`). The manifest therefore
records, beside the private commit (`graphvein.commit`, an internal
reference only):

- `graphvein.identity.public_tree`: the Git tree id of the public snapshot
  of the commit the binary was built from (`graphvein.build_commit`: the
  commit its `graphvein version` names, else the checkout's HEAD), computed
  offline by `bash scripts/benchmark/public-tree.sh <commit>` (`gvbench
  public-tree` over `git ls-tree -r -z`, no checkout) with the same rules as
  the snapshot script (the paths of the commit's
  `scripts/public-exclude.txt` removed, `docs/public/AGENTS.md` as
  `AGENTS.md`);
- `graphvein.identity.extractor_version` (the index's extractor version at
  that commit), `graphvein.version_output` (the full `graphvein version`
  output) and `graphvein.binary_sha256`.

A published result names the public commit whose tree id
(`git rev-parse <commit>^{tree}`) equals the recorded `public_tree`, once
the public repository exists. If the binary was built from uncommitted
changes, the tree id names the commit, not those changes. The ids match only if
the public commit keeps the snapshot's files byte for byte, including the
executable bit (a commit made on Windows with `core.fileMode` false records
every file as non-executable).

## Re-running selected runs

`--rerun LIST` runs only the listed run ids (`*` matches any text, e.g.
`codex-visible-mcp-*`); every other planned run is left out, and a pattern
that matches no planned run stops the driver. Runs of an earlier label are
re-run under a new label that holds only them (for the first full run:
`2026-11-24-r2`), so the original rows, grades and summary stay as
recorded and the new runs are reported beside them, never merged into
them. The manifest records the list (`plan.rerun`).

## What is committed

Per run label, under `docs/evidence/benchmark/runs/<label>/`:

- `manifest.json`: CLI and harness versions, sign-in kinds, model ids,
  corpus commits and workspace digests, index build time, the price file
  and the live price check, seed, cap, limits;
- `runs.jsonl`: one row per attempt and per run that never started, with
  every metric above, the grade, the outcome and the SHA-256 and size of the
  raw transcript (a run's last row is its result);
- `answers/`: the final JSON block of each run;
- `summary.md`: per agent and mode tables, generated from `runs.jsonl`.

Raw transcripts stay in the work directory on the machine that ran the
benchmark; their hashes in `runs.jsonl` let anyone holding them check that
they are the ones graded.

## Stand-in mode

`--standin` replaces every agent CLI with the helper's stand-in, which
prints canned transcripts in each CLI's output format, picks a scenario per
question and mode (correct, wrong, malformed, hang, rate limit, a cost
stream that crosses the per-run ceiling, a run billed at a non-preferred
endpoint, a Claude result without `total_cost_usd`, a transcript that
contains the API key), and serves OpenRouter key usage from a local file.
Like Claude Code, the stand-in refuses `--bare` without `ANTHROPIC_API_KEY`
and then reports the key as its credential, so the test sees which auth
path the driver chose. It tests the
driver, the parsers, token capture, grading, the timeout, the per-run
ceiling and the total cap with no CLI, no sign-in, no network and no spend
(`bash scripts/benchmark/test-run.sh`, part of `scripts/test.sh`).

## Provisional details

These come from the CLIs' documentation, help output and released code,
not yet from a signed-in run, and are checked by one real run of BQ-1 in
each mode per CLI before the full run (the canned transcripts are replaced
by sanitised captures of those runs):

- Codex: the event nesting of usage on `turn.completed`, whether
  `input_tokens` includes cached tokens, the shape of `mcp_tool_call`
  results, the signed-in wording of `codex login status`, the
  `web_search="disabled"` override, and whether a stdio MCP server starts
  under the read-only sandbox. The Codex canned transcripts are
  provisional.
- Claude Code: that `--restricted` keeps a subscription sign-in working,
  and the exact result object of a rate-limit stop. On API billing: that
  `--bare` together with `--restricted`, `--tools` and the MCP flags behaves
  as described, that `--add-dir <workspace>` loads the corpus's `CLAUDE.md`
  under `--bare`, that `CLAUDE_CODE_MAX_OUTPUT_TOKENS` bounds output per
  call, the result object of a `--max-budget-usd` stop, that
  `total_cost_usd` matches the Console's usage for the run, and that the
  per-message usage in stream-json assistant events is final (the live
  ceiling watch and a worst-case booking rely on it).
- opencode: the pass-through of the OpenRouter routing options and the
  per-call output limit, whether a transcript carries the serving provider
  or generation ids, and whether `tokens.input` excludes cached tokens.
