# External indexer invocation

`graphvein index [root] --indexer <path> [--indexer-timeout 30s] [--emit copy.ndjson]`
runs a user-supplied executable without a shell:

```text
<absolute indexer path> --repository <resolved logical ID> <absolute repository root>
```

The working directory is the repository root. Arguments remain separate even
when paths or IDs contain spaces. Relative executable paths resolve against
Graphvein's current directory, never against the repository root or `PATH`.
Missing/non-executable files fail preflight with exit 1 before config or storage
work. Unix files need an execute bit; Windows requires a native `.exe` or `.com`
file. Windows cannot directly execute `.sh` files through this no-shell API.

The parent environment passes through except variables with the `GRAPHVEIN_`
prefix (case-insensitive to respect Windows environment semantics). This prevents
Graphvein storage URLs and credentials reaching the producer. No test exception
exists. Other parent variables remain available; this is not an OS sandbox or a
general-purpose secret filter.

stdout must carry a [contract v1 complete snapshot](v1.md); stderr carries logs.
Graphvein streams stdout directly through the same validator and transactional
diff ingester as its built-in producer. `--emit` tees those bytes to a file,
without staging a whole snapshot. On failure the copy may contain a partial
stream. The repository in the header must equal the resolved ID. A framing,
footer, start, I/O, timeout or non-zero process exit failure exits 1 and rolls
back, even when the process emitted a valid footer before exiting non-zero.
Publication waits for both validated EOF and successful process exit. An early
ingestion failure cancels the process; every started child is waited for.

`--indexer-timeout` is a Go duration, default `0` (no deadline); negative values
are usage errors. Explicit `--workers`, `--timeout`, `--max-file-size` or `--shape-depth`
flags with `--indexer` are usage errors (exit 2), even when set to their defaults.
External runs never read, save or prune the built-in fact cache. Run metadata
preserves the producer header. Graph rows still use the normal snapshot diff,
so repeated identical snapshots write zero graph rows.

Only the last 64 KiB of stderr are retained and logged at `slog` debug level.
Failures include the last 20 retained lines. stderr is drained while stdout is
ingested to avoid blocking a producer with verbose logs.

`status` and query freshness reflect Graphvein's own scan and ignore rules,
not the external indexer's view of the files. Different producer inventories
can therefore report changed files immediately after indexing.

The offline guarantee covers Graphvein's own connections through
`internal/egress`. External indexers are user-supplied code and may themselves
open network connections. Only run producers you trust.

For manual replay, `scripts/replay-indexer.sh` ignores its arguments and copies
`GV_REPLAY_STREAM` to stdout. On Windows build `scripts/replay-indexer.go` as a
native executable. The task summary contains exact round-trip commands.
