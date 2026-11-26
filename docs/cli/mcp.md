# graphvein mcp

`graphvein mcp` is a read-only [MCP](https://modelcontextprotocol.io) server
over the stored index, for coding agents ([ADR 0082](../adr/0082-mcp-server.md)).
It speaks MCP on standard input and output (newline-delimited JSON-RPC, the
stdio transport) and nothing else: it opens no listener and makes no
network connection. It never indexes: run `graphvein index` (or
`graphvein index --project`) first, and again when the answers report a
stale index.

```
graphvein mcp [path] [--repository ID] [--structured-only]
graphvein mcp --project MANIFEST [--structured-only]
```

- `path` is the repository (default `.`); the repository ID and storage are
  found as for `graphvein query` (`--repository`, `graphvein.yaml`, the
  directory name; `GRAPHVEIN_SQLITE_PATH` or `GRAPHVEIN_POSTGRES_URL`).
- `--project` serves a [project](project.md): every tool but `status`
  and `link_changes` takes a `repository` argument (required when the project has several
  members and the tool reads one member); `callers`, `impact` and
  `change_impact` cross repositories through the project's stored links,
  and `chain`, `endpoint`, `unresolved` and `overview` show the other
  members as the project views do. `link_changes` answers in project
  mode only (a single-repository server refuses it with `project_only`).
- `--structured-only` returns the answer as `structuredContent` with a
  one-line text summary instead of a second, identical JSON copy in the
  text block (for clients that pass both to the model).
- Diagnostics go to standard error; standard output carries only the
  protocol. The session ends when the client closes standard input.

## Tools

| tool | question | arguments |
|---|---|---|
| `status` | Is the index current, what can be trusted? Also whether `graphvein.yaml`'s index settings changed since the last run (`settings`, MC-33) | `limit`, `cursor` (the changed files) |
| `find` | Where is X defined? | `term`; `kind`, `file`, `limit`, `cursor` |
| `endpoints` | Which routes exist? | `transport` (`http`), `method`, `path_prefix` (any served path), `scope` (`production`), `detail` (`compact`, `full`), `limit`, `cursor` |
| `endpoint` | What does this route do, who calls it? | `target` (identity, `METHOD /path` as served, or a name), `scope`, `detail` (`compact`, `full`), `limit`, `cursor` (the callers) |
| `source` | Show me the lines | `target`, `context` (8, at most 50) |
| `callers` | Who calls this? | `target`, `depth` (1; a route 2; at most 10), `scope` (`production`, `all`), `limit`, `cursor`, `unknowns_limit`, `unknowns_cursor` |
| `impact` | What depends on this? | `target`, `depth` (3; at most 10), `scope` (`production`, `all`), `limit`, `cursor`, `unknowns_limit` (the possibly missing call sites, 20), `unknowns_cursor` |
| `chain` | The whole path from UI to table, up or down | `target` (identity, component directory or name), `direction` (`both`, `up`, `down`), `depth` (6, at most 10), `scope`, `limit`, `cursor` (the members) |
| `table` | A table's schema, and which code reads or writes it | `target` (a table), `include` (`both`, `schema`, `users`), `detail` (`compact`, `full`), `scope` (the users), `limit` (20), `cursor` (the users), `unknowns_limit`, `unknowns_cursor` (the sites that may also use it) |
| `changes` | What did my change do to the facts? | `base` (`previous`; `run:ID`, `pin:NAME`, a stored `ref:` snapshot), `group`, `change`, `scope`, `detail`, `limit`, `cursor` |
| `change_impact` | What does this changed item reach? | `id` or `group`, `base`, `depth` (3, at most 10), `limit`, `cursor`, `unknowns_limit`, `unknowns_cursor` |
| `unresolved` | What could Graphvein not link? | `transport` (`http`), `status` (every status but `linked`), `scope`, `limit`, `cursor` |
| `overview` | What is this repository made of? | `scope`, `limit`, `cursor` (the components), `unknowns_limit`, `unknowns_cursor` |
| `link_changes` | Which project links changed? (project mode) | `base` (`previous`, `pass:N`), `limit`, `cursor` |

Every answer is one envelope with the index freshness checked for that
call batch, the request after defaults, the caps that bound, paging ("N
more not shown; call again with cursor"), the results, unknowns,
assumptions, the meaning of each label it uses and every field it left
out with how to get it (`omitted`: `{field, how}`). The refusal, the caps
and the paging come before the results, so a client that cuts a long
answer keeps them. A list an unknown carries (possibly missing call
sites, sites that may also use a table, external stubs) shows its first
`unknowns_limit` items (20) with the exact total and `next_cursor`.

Freshness is checked once per call batch: a check serves the calls that
start within 2 seconds of it, unless a new index run was stored since,
and says `reused: true`. `status` always checks. When a node in an
answer says its file changed (`file_state`) while the freshness says
fresh, the answer is checked again, or the mismatch is listed as an
unknown; an answer never calls an index fresh next to a changed file.

A target is an identity, a name, or a qualified-name suffix that starts
at a name boundary (`save` for `User.save`), as for `graphvein callers`;
`request.match` says which. A suffix inside a name (`Items` for
`listItems`) is never chosen: it is offered as a candidate of the
refusal. "METHOD /path" names the routes served at that path, under any
of the paths a route is mounted at in the tool's scope (a test-only
mount is not a production path), before any other node (an OpenAPI
operation of the same name is a candidate only). `table` looks up tables
only; `chain` also takes a component directory, and `component:DIR` when
a node has the same name. Several matches are always refused as
ambiguous with every candidate.

Answers are compact by default. `endpoints` lists each route's method,
path, declaration file and line, caller count and flags without the
per-route shape summary; `endpoint` writes the request and response shape
as section states and item names with their types ("array<Item>",
"object{id: integer, note?: string}", "unknown (reason)", "not read");
`table` counts each column's claims by verification and names every
claim that is not oracle-verified, and lists 20 users per page (a table
whose source no code-to-table row covers says its users are not
extracted, never zero); `changes` names the changed fields.
`detail: "full"` returns the views' own detail; nothing is left out
silently.

`callers` and `impact` answer in production scope by default, as
`chain` and `table` do, by the same test-scope rule (the chain view's: a
node whose scope attribute is `test`, or in a file the repository's test
rules classify as test, built-in names and the `tests` section of
`graphvein.yaml`). The results and "? not modelled" ends in test code are
left out and counted in one unknown (`test_scope_hidden`) that says to
call again with `scope: "all"`; with `all`, a result in test code says
`test_code: true`. The filter applies to the results, not the walk: a
kept result whose stored shortest path passes through test code says
`via_test_code: true` (a longer production-only path may exist), and the
caps, blind spots, near misses and possible callers of other members
count test code too. Every production-scope answer says so in one
unknown (`test_scope_unfiltered`), and the near misses, possible callers
and possibly missing call sites in test code say `test_code: true`.
When results were left out, `impact`'s count of unresolved call sites
"in the files of these results" counts the shown results' files only.

`link_changes` compares two kept link passes of a project (the link
journal of `graphvein changes --project`, read through
`/api/v0/project/links/changes`): the links added, removed or changed
(same kind and ends, a different stored payload) with their kind and
ends, and in its summary the view's `state` (`compared`, `same_pass` or
`no_comparison`), the passes compared, the counts, whether the
manifest changed and the members and runs each pass linked. With fewer
than two kept passes, or with the newest pass as the base, nothing is
compared, which is an unknown (`no_comparison`, `same_pass`), never "no
change"; an unknown pass is refused (`base_unknown`). It never links or
indexes: run `graphvein index --project` for a new pass.

The change tools read only what is stored: a `ref:` base whose snapshot
is not stored is refused (`would_index`) with the command that stores it,
and a base that does not exist lists the stored bases as candidates. An
empty result with a bound cap means unknown beyond the cap, not none; no
result is ever proof of absence. What each tool and field may claim is the
[MCP claim contract](../contract/claims/mcp-server.md).

## Configuring a client

Replace `/path/to/repo` with the repository's absolute path (on Windows,
for example `C:/src/app`; the command is `graphvein.exe` if it is not on
the `PATH` as `graphvein`). The snippets follow each client's documented
format; they were not run against the clients for this release, so check
the client's own documentation if one is refused.

**Claude Code** (unverified here): add it from the command line,

```
claude mcp add graphvein -- graphvein mcp /path/to/repo
```

or commit a project-scoped `.mcp.json` at the repository root:

```json
{
  "mcpServers": {
    "graphvein": {
      "command": "graphvein",
      "args": ["mcp", "/path/to/repo"]
    }
  }
}
```

**Codex** (unverified here): in `~/.codex/config.toml`,

```toml
[mcp_servers.graphvein]
command = "graphvein"
args = ["mcp", "/path/to/repo"]
```

**opencode** (unverified here): in `opencode.json`,

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "graphvein": {
      "type": "local",
      "command": ["graphvein", "mcp", "/path/to/repo"],
      "enabled": true
    }
  }
}
```

For a project, pass `--project /path/to/project` instead of the repository
path. Postgres storage is configured through `GRAPHVEIN_POSTGRES_URL` in
the environment the client starts the server with (most clients take an
`env` map next to `command`).

The server's `initialize` result carries short instructions for the
agent: which questions the tools answer (routes and their handlers,
callers, what a change reaches, tables and the code that uses them,
unresolved calls), that every answer carries freshness, labels, unknowns
and caps, and that for such questions the agent should look for and use
the Graphvein tools before reading files. Whether a client shows them to
its model is up to the client.

### Codex and tool search

Codex 0.171.0 defers MCP tools behind its tool search when the model
supports that search: the model sees a search tool, not Graphvein's tools,
and may never look for them. In the [agent benchmark of
2026-11-24](../evidence/benchmark/results-2026-11-24.md), Codex with the
configuration above called a Graphvein tool in 2 of 63 runs; with the
tools visible, in 44 of 63; with the tools still deferred but the
server instructions above, in 26 of 63 (round 3). The deferral (feature
`tool_search_always_defer_mcp_tools`) is fixed on in that version, so
`--disable` does not turn it off.

An advanced setting makes the tools visible: a model catalog in which no
model supports the search tool. This is what the benchmark's
`codex-visible` agent does (`scripts/benchmark/agents.sh`). It relies on
Codex's model catalog format and was checked only with Codex 0.171.0; a
later version may change or remove it.

1. Write Codex's bundled catalog to a file (no network):
   `codex debug models --bundled > codex-models.json`.
2. Set `"supports_search_tool": false` on every entry of its `models`
   list and keep every other field (the benchmark does this with
   `gvbench codex-catalog --in codex-models.json --out codex-visible.json`,
   built from `scripts/benchmark/gvbench`).
3. Start Codex with the edited catalog:
   `codex -c model_catalog_json="/path/to/codex-visible.json"` (the same
   option works with `codex exec`).

To check whether Codex calls Graphvein, run it with `codex exec --json`
and look for `item.completed` events whose `item.type` is
`mcp_tool_call` and whose `item.server` is `graphvein` (the name in
`[mcp_servers.graphvein]`); calls to the server `codex` are Codex's own
built-in tools. An interactive session's transcript shows each MCP tool
call with its server and tool name.

## Security

The tools only read: the server answers from the same read-only views as
`graphvein serve` (through an in-process reader with no socket) and from
the query commands' code. Re-index, pins, ref snapshots, manifest edits and
"try it" are not reachable. Tests enforce the boundary: a static scan
allows only the SDK's stdio server API in Graphvein's own code, the
dependency review lists the SDK's network-capable packages with the reason
they are not reached, and a runtime test runs a whole session (initialize,
tool list, every tool) with tripwires on egress, the default resolver and
the default HTTP transport.
