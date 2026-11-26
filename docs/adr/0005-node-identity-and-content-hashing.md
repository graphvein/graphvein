# ADR 0005 — Node identity, content hashing and snapshot storage

**Status:** Accepted (developer review, 2026-10-05)
**Date:** 2026-10-05

## Context

T004 establishes the contract used by external indexers and all later analysis.
Identity must survive body edits, hashes must agree across checkouts, and a
failed or truncated import must not destroy the last usable graph.

## Options considered

- Line numbers or commit IDs: unstable under unrelated edits or across branches.
- Unescaped names: ambiguous delimiters and platform-dependent paths.
- Strip whitespace/comments or hash syntax trees: requires language knowledge,
  can hide meaningful indentation, literal or comment changes, and cannot yet
  be reproduced by every external indexer.
- Separate backend implementations: duplicate SQL behavior and drift risk.
- Commit each ingestion batch directly: exposes incomplete graphs on failure.

## Decision

Use `kind:escaped-path#escaped-qualified-name`, scoped by a stable repository
identifier stored separately. Kind is lowercase ASCII `[a-z][a-z0-9_]*`.
Escape UTF-8 bytes using uppercase `%HH`; leave only ASCII letters, digits,
`-._~` unescaped (also `/` in the path). Decode and re-encode to reject aliases.
Paths are case-sensitive repository-relative POSIX paths, without empty, dot,
dot-dot or backslash components. `.` denotes the repository root only for nodes.
Do not normalize Unicode or case. Qualified names come from lexical owners
joined with `.`, including receiver names; anonymous/overloaded symbols require
a stable semantic discriminator chosen by the extractor, never a line number.
Renames intentionally produce new identities. Location nodes are the exception:
their qualified names encode a source range, so they move with that range.

Hash source bytes with SHA-256 after replacing CRLF and lone CR with LF.
Preserve all other bytes, including trailing whitespace, BOMs and final newline
presence. Emit `sha256:` followed by 64 lowercase hex digits. File hashes cover
the whole file; source node hashes cover exactly their half-open byte span.
Synthetic nodes hash canonical UTF-8 JSON of their semantic attributes (sorted
keys, no insignificant spaces), with no location or commit metadata. V1 makes
no language-dependent whitespace promises. A future normalization change is a
breaking contract change, never a silent hash algorithm update.

V1 streams are complete snapshots for one repository. Files precede their nodes;
all nodes precede edges. External references need explicit `external` nodes;
missing endpoints become diagnostics rather than dangling edges. A store run
is one atomic transaction, receiving bounded write batches, and publishes only
after the footer and EOF validate. Replacement removes previous repository
facts while preserving run history; other repositories are isolated. Invalid
body records are diagnosed and skipped. Invalid framing rolls back the run.

Use `database/sql` with modernc SQLite and pgx's SQL adapter, sharing portable
SQL and versioned migrations. Store keys include repository; index node kind,
level and file, and both edge endpoints. JSON payloads retain open attributes;
indexed columns support graph queries. pgx receives its dial callback from
`internal/egress`; only that package opens network connections. JSON Schema
validation uses `github.com/santhosh-tekuri/jsonschema/v6` with local schemas.

## Consequences

Body edits preserve identity but invalidate content-bound annotations. Harmless
formatting can invalidate caches conservatively. Snapshot imports delete absent
files and incident edges atomically. A single transaction can hold locks for a
large run, but memory use is bounded and readers never see partial ingestion.
Schema versions newer than the binary fail on open. Migrations serialize opens.
Postgres graph keys use `C` collation to match SQLite byte ordering rather than
depending on the server's locale. Synthetic attribute hashes use the exact
canonical JSON rules and safe-integer range documented in contract v1.
Multi-table dumps use one read transaction (repeatable read on Postgres) so they
cannot combine facts from snapshots committed between queries.

## Known gaps

Incremental delta streams, language-aware hashing, anonymous-symbol naming,
working-tree overlays and the full egress policy belong to later milestones.
Repository identifiers must be configured consistently by teammates; absolute
checkout paths are unsuitable. V1 does not merge concurrent branch snapshots.

## Review notes (developer, 2026-10-05)

- Incremental indexing: v1 stays a complete snapshot. The built-in indexer
  must reparse only changed files and replay cached per-file facts for the
  rest, and the ingester must diff the snapshot against the store instead of
  rewriting unchanged records. Decide and measure in the scanner/incremental
  task before large-repository use; a delta-stream contract version remains
  an option if diffing is insufficient.
- Formatting churn: strict hashes are correct for caches, but dispositions
  keyed on them would all need re-review after a formatter run. Plan an
  optional `semantic_hash` field (significant tokens only, trivia excluded),
  added compatibly as an optional field, used to decide disposition re-review.
- Column units: spans use UTF-8 byte offsets. IDE links must convert to the
  target's units (characters for VS Code URLs, UTF-16 for LSP) using the
  source line.

## Review notes (developer, 2026-10-05, after T006)

- Superseded consequence: "memory use is bounded" no longer holds for
  ingestion. ADR 0006 keeps old-payload and seen-key maps of O(snapshot size);
  only the 128-record write buffer remains bounded.
- Incremental-indexing review note resolved by ADR 0006 (cached extraction plus
  ingester diffing; no delta contract).

## Review notes (developer, 2026-10-07, after T008b)

- Column-unit note resolved by T008b: IDE links convert contract UTF-8 byte
  columns to the configured unit (default UTF-16 code units, 1-based, matching
  VS Code), falling back to inexact byte columns when the file changed.
