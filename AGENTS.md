# AGENTS.md: working on Graphvein

You are helping someone contribute to Graphvein, a tool that maps codebases
from architecture overview down to the exact line. Its purpose is to fix
what happens when code is built fast without anyone holding the design. Do
not recreate that problem here.

## Read first

1. `docs/VISION.md`: what Graphvein is and its principles (above all,
   principle 6: honest about precision).
2. `docs/ARCHITECTURE.md`: how it is built. Follow it.
3. `CONTRIBUTING.md`: the rules every contribution follows.
4. The ADRs in `docs/adr/` for the area you touch, and its claim contract
   in `docs/contract/claims/` if there is one.

If the request conflicts with these documents, stop and say so instead of
improvising.

## Rules

- Do only what was asked; list anything else you notice as a follow-up.
  Keep changes small enough to review in one sitting.
- `bash scripts/test.sh` must pass. New behaviour needs a test; extractors
  and linkers need a fixture with a golden output. Never update a golden
  file just to make a test pass; explain every golden change.
- Claim contracts: keep the code and the table 1:1, send every form no row
  covers to the single unknown default branch, and never add a claim the
  table does not list. A new claim is a contract change, reported as such.
- Storage code is tested against SQLite and Postgres; CI runs the Postgres
  job. Do not start Docker or install a database service for it.
- Only `internal/egress` opens network connections. No cgo. All persistence
  goes through the store interface. Contract changes bump
  `contract_version` and update docs and goldens together.
- A failure in one file never stops the others; record a diagnostic.
- Decisions about architecture, the indexer contract, storage schema,
  dependencies or public interfaces get an ADR with `**Status:** Proposed`
  (use the next free number); never set another status.
- Every file you create must be tracked without `git add -f`; fixtures use
  inert names for ignore files and ignored paths (see CONTRIBUTING.md).
- Shell scripts run in Git Bash on Windows and under bash 3.2.
- New dependencies need an allowed licence; regenerate
  `THIRD_PARTY_NOTICES` with `go run ./scripts/notices`.
- Never commit secrets; configuration secrets come from environment
  variables. The server binds to 127.0.0.1 by default.
- Go: plain and idiomatic, wrapped errors, `log/slog`, doc comments that
  explain why. TypeScript: strict mode, no unexplained `any`.

## Finishing

Summarise what changed (by file or package), the choices you made that the
request did not specify, the tests you added and the suite's result, and
follow-ups you noticed but did not address.
