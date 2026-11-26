# Agent benchmark

Does an agent answer codebase questions better, and more cheaply, with the
Graphvein MCP server attached? This folder holds the method, the frozen
question set and the committed results.

- [method.md](method.md): agents, models, prompt, grading, cost cap, what
  is committed.
- [questions.jsonl](questions.jsonl): the 21 frozen questions with verified
  answers and evidence.
- `runs/<label>/`: results of one benchmark run (manifest, per-run rows,
  final answers, summary).
- [results-2026-11-24.md](results-2026-11-24.md): results of the first full
  run (baseline, 2026-11-24), with the pilots that preceded it.

## Running it (Windows, Git Bash)

All commands run from the repository root in Git Bash. They also work in
bash on macOS and Linux. The driver needs Go, git and tar; curl only for
the OpenRouter agent. CLI paths are never looked up on `PATH`: give each
one by flag or environment variable. Point them at the native `.exe` where
there is one rather than an npm `.cmd` shim, since a shim passes arguments
through `cmd.exe`.

### 1. Check the plumbing first (no CLI, no sign-in, no spend)

```bash
bash scripts/benchmark/test-run.sh
```

It prints `test-run: OK`.

### 2. Point the driver at the CLIs and corpora

```bash
export GV_BENCH_CLAUDE="$HOME/.local/bin/claude.exe"        # Claude Code
export GV_BENCH_CODEX="/c/path/to/codex.exe"                # Codex CLI
export GV_BENCH_OPENCODE="/c/path/to/opencode.exe"          # opencode
export GV_BENCH_GRAPHVEIN="$PWD/bin/graphvein.exe"          # bash scripts/build.sh builds it
export OPENROUTER_API_KEY=...   # a dedicated key whose credit limit equals the cap
```

Do not export `ANTHROPIC_API_KEY` here: set it only for the benchmark
command (step 3). Without it, Claude Code runs on its own sign-in as before.

The equivalent flags are `--claude-bin`, `--codex-bin`, `--opencode-bin`
and `--graphvein-bin`. The MCP server command defaults to the graphvein
binary with the argument `mcp`; change it with `--mcp-command PATH` and
repeated `--mcp-arg ARG` (or `GV_BENCH_MCP_COMMAND` and a space-separated
`GV_BENCH_MCP_ARGS`). `{workspace}`, `{store}` and `{corpus}` in an argument
expand per corpus.

The corpora are git checkouts that contain the pinned commits
(`1762adac607a1b29cfc4da129557780beea71616` for the FastAPI full-stack
template, `504c02fc47e76087b82b0e7cbe4186e9cdd916d4` for langflow); the
driver exports those commits itself.

### 3. A dedicated Anthropic API key for the Claude runs

Claude Code runs are billed to API credits through `ANTHROPIC_API_KEY`;
the driver then runs Claude Code with `--bare` (only that key is read) and
stops before a run that could take the Claude spend over its cap ($60 by
default, `--claude-cap-usd`). As a second limit outside the script:

1. In the Claude Console (platform.claude.com), create a workspace used only
   for this benchmark.
2. Set that workspace's spending limit to the cap (or a little above it).
3. Create an API key in that workspace. Copy it once; do not save it in a
   file, a script or the repository.
4. Delete the key (or archive the workspace) when the benchmark is done.

Give the key to one command only. In Git Bash, read it without echo and
pass it to the run:

```bash
read -rs ANTHROPIC_API_KEY      # paste the key, press Enter; nothing is shown
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" bash scripts/benchmark/run.sh ...
unset ANTHROPIC_API_KEY
```

`ANTHROPIC_API_KEY=... bash scripts/benchmark/run.sh ...` also works, but
the key then ends up in the shell history. The driver never prints the key,
redacts it from error text and refuses to write any result file that
contains it. Each row records the auth path (`auth_path` `api_key_bare`)
and the summary lists the API spend under "Spend".

### 4. Preflight and plan only

```bash
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" bash scripts/benchmark/run.sh --dry-run \
  --corpus-fastapi /c/src/full-stack-fastapi-template --corpus-langflow /c/src/langflow \
  --label 2026-11-24-p0
```

This checks every CLI's version and sign-in, the OpenRouter key and prices,
the Claude price file, exports the workspaces, builds the indexes and prints
the run order without running an agent. The models default to
`claude-opus-5-5` (`--model-claude`) and `gpt-6.2-sol` (`--model-codex`; if
the Codex account does not offer it, use `--model-codex gpt-6.1-sol`).

### 5. Pilot: one repetition of one question first

```bash
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" bash scripts/benchmark/run.sh \
  --corpus-fastapi /c/src/full-stack-fastapi-template --corpus-langflow /c/src/langflow \
  --questions BQ-1 --reps 1 --label 2026-11-24-p0
```

Two runs per agent (both modes). Check the Claude rows' `cost_usd` against
the workspace's usage in the Console, then read `docs/evidence/benchmark/runs/2026-11-24-p0/summary.md`
and the transcripts in `$HOME/graphvein-bench/2026-11-24-p0/transcripts/`
before going further. One agent alone: add `--agents claude` (or `codex`,
`opencode`).

### 6. The full run

```bash
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" bash scripts/benchmark/run.sh \
  --corpus-fastapi /c/src/full-stack-fastapi-template --corpus-langflow /c/src/langflow \
  --label 2026-11-24-full
```

21 questions × 2 modes × 3 repetitions per agent. Defaults: total cap $8
and per-run ceiling $0.45 for OpenRouter (`--cap-usd`, `--run-cap-usd`);
total cap $60 and per-run ceiling $3 for Claude on API billing
(`--claude-cap-usd`, `--claude-run-cap-usd`, or `GV_BENCH_CLAUDE_CAP_USD`,
`GV_BENCH_CLAUDE_RUN_CAP_USD`); timeout 10 minutes (`--timeout`), 60 turns
(`--max-turns`). Runs the cap cannot cover are reported as "not run (cap)". If an agent
hits a rate limit, its remaining runs are recorded as not run; continue
later with the same command plus `--resume`.

Transcripts, workspaces and index stores go to
`$HOME/graphvein-bench/<label>` (`--work`), never into the repository. The
results to commit are written to `docs/evidence/benchmark/runs/<label>/`
(`--out`).

### 7. Round 2 of 2026-11-24: Codex with visible MCP tools, re-runs

One command runs the second round under its own label, `2026-11-24-r2`,
which holds only these runs (the first full run stays as recorded):
`codex-visible` (Codex with its MCP tools shown instead of deferred behind
tool search, mcp mode only) on all 21 questions, three repetitions; the five
opencode BQ-21 runs stopped by the old per-run ceiling; and the three opencode
base BQ-16 repetitions that had invalid attempts. No Claude run, so no
`ANTHROPIC_API_KEY`; `OPENROUTER_API_KEY`, `GV_BENCH_CODEX`,
`GV_BENCH_OPENCODE` and `GV_BENCH_GRAPHVEIN` as in step 2.

```bash
bash scripts/benchmark/test-run.sh
bash scripts/benchmark/run.sh --dry-run \
  --corpus-fastapi /c/src/full-stack-fastapi-template --corpus-langflow /c/src/langflow \
  --agents codex-visible,opencode \
  --rerun 'codex-visible-mcp-*,opencode-base-bq-21-r1,opencode-base-bq-21-r2,opencode-base-bq-21-r3,opencode-mcp-bq-21-r2,opencode-mcp-bq-21-r3,opencode-base-bq-16-r1,opencode-base-bq-16-r2,opencode-base-bq-16-r3' \
  --label 2026-11-24-r2
```

The dry run prints 71 runs (63 `codex-visible`, 8 `opencode`); run the same
command without `--dry-run`. If it stops (rate limit, cap), add `--resume`.
Before the 63 Codex runs, a check of one run is worth its time: the first
`codex-visible` rows of `docs/evidence/benchmark/runs/2026-11-24-r2/runs.jsonl`
should show `mcp_tool_calls` above 0 on most questions.

### Re-grading after a correction of the question file

```bash
go run ./scripts/benchmark/gvbench regrade --check \
  --runs-dir docs/evidence/benchmark/runs/2026-11-24-full \
  --questions docs/evidence/benchmark/questions.jsonl
go run ./scripts/benchmark/gvbench regrade \
  --runs-dir docs/evidence/benchmark/runs/2026-11-24-full \
  --questions docs/evidence/benchmark/questions.jsonl --note "DATE: what was corrected"
```

The first lists the rows whose grade would change; the second rewrites them
(keeping each earlier grade in `grade_v1`) and regenerates `summary.md`.
`bash scripts/benchmark/public-tree.sh <commit>` prints the
public snapshot tree id of a commit (method.md, "Graphvein identity").

`bash scripts/benchmark/run.sh --help` lists every flag.
