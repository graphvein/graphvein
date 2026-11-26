# ADR 0010 — External indexer invocation and project configuration

**Status:** Accepted (developer review, 2026-10-07)
**Date:** 2026-10-06

## Context

T008a makes the indexer replaceable without changing contract v1, while giving
different checkout directories a shared logical repository ID. A valid stream
alone does not prove that a producer finished successfully. Parent storage
credentials must not be exposed accidentally to user-supplied code.

## Options considered

- Shell commands or PATH lookup: ambiguous quoting and executable selection.
- Commit at footer/EOF without checking exit: publishes a failed producer's data.
- Buffer all stdout and check exit first: adds unnecessary snapshot memory/disk.
- Inherit all environment variables: leaks Graphvein configuration and secrets.
- JSON config, archived `gopkg.in/yaml.v3`, permissive YAML, or maintained
  `go.yaml.in/yaml/v3` with explicit node/tag validation.
- Duplicate repository fallback logic in each command, or one shared resolver.

## Decision

Use `exec.CommandContext` with `<executable> --repository <id> <absolute root>`,
root as cwd, no shell, and relative executable paths resolved against the caller's
cwd. Preflight checks regular files and execute bits on Unix; on Windows native
`.exe`/`.com` files are required. Strip every `GRAPHVEIN_` environment variable,
case-insensitively for Windows; pass other variables unchanged. This prevents
Graphvein credentials and storage URLs from reaching the child, without claiming
to sanitize arbitrary environment secrets or sandbox the executable.

Stream into the existing ingester with optional expected-repository validation
and a pre-commit callback. Validate the complete framing/EOF before waiting for
successful child exit and committing. A non-zero exit rejects even a valid footer.
Timeout or early ingestion failure cancels the child, and every started child is
waited for. Retain a 64 KiB stderr tail; debug-log it and show the last 20 lines on
failure. `--emit` tees the stream. The store's variadic `RunOptions` suppresses
fact-cache pruning for external runs; built-in ingestion retains its current
default. No schema migration or contract-version change is needed.

Use already-pinned `go.yaml.in/yaml/v3`, the maintained successor to archived
`gopkg.in/yaml.v3`. Inspect YAML node kinds/tags explicitly to prevent scalar
coercion; reject duplicates, unknown keys, aliases as values, extra documents,
invalid repository strings and any version other than integer 1. Read only the
repository argument's root. Resolve ID once: explicit flag, config, basename.
Validate config even with an override. Config failures are usage errors (exit 2).
`ingest` continues to use the stream header's identity.

The VISION offline promise covers Graphvein's own connections only. An external
indexer is user-supplied code outside `internal/egress` and can use the network;
Graphvein does not enforce its network behavior.

## Consequences

Existing snapshot validation, rollback and diff behavior apply to both producers.
Repeated external snapshots write zero graph rows. Teammates can configure a
shared ID independent of checkout names. Strict config catches misspellings and
future unsupported settings immediately. Child stderr is bounded; ingestion's
existing comparison maps remain proportional to snapshot size (ADR 0006).
Test binaries re-execute helper tests with a `GV_TEST_` marker, never nested builds.

## Known gaps

This does not provide an OS sandbox or terminate arbitrary descendant process
trees. Cancellation kills and waits for the direct child; inherited stdout reads
are closed on cancellation and inherited stderr draining is bounded. Windows
shell scripts require a native wrapper, supplied for replay verification. An
external producer can retain a different file inventory; freshness uses the
Graphvein scanner. IDE templates and additional config settings remain T008b.

## Review notes (developer, 2026-10-07)

- Postgres parity verified in CI: TestExternalSnapshots/postgres passed with
  GRAPHVEIN_REQUIRE_POSTGRES=1; no unexpected skips.
- go mod tidy recorded kr/text and rogpeppe/go-internal as test-only
  dependencies of yaml; neither is linked into the binary.
- Only the direct child is killed. Descendant process trees (Windows job
  objects, Unix process groups) need handling before release (M7).
- Windows accepts only native .exe/.com indexers; script-based producers need
  a wrapper. Revisit only if real external indexers ship as scripts.
- External runs never prune the built-in fact cache, so entries for deleted
  files persist until the next built-in run. Include in the ~5k-file profile.
- Freshness uses Graphvein's scanner, so a producer with a different file
  inventory reports changed files immediately. Revisit with MCP freshness (M5).
