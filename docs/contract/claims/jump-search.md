# Claim contract: jump-to search

**Status:** in force (ADR 0089, accepted 2026-11-25). Rows `JS-1`..`JS-29` are reserved
for this contract; rows `JS-1`..`JS-21` and the default row `D` are in
force, the others are kept for a later reported contract change (row
`JS-21` and the change to `JS-16`, better matches before member order,
are the T040 server follow-ups; the unknown-prefix key of `JS-4` is ADR
0089 Revision 2, its "(path not known)" label Revision 4, the
"(shadowed)" and "(path withheld)" labels Revision 5). The code
table is `searchRows` in `internal/server/search.go`, and
`TestSearchContractTable` keeps it 1:1 with the table below: every row is
in both, in the same order with the same topic, and names a test that
exists.

`GET /api/v0/search?q=&kinds=&limit=&editor=` (one repository, or one
project member with `repository=`) and the same path without `repository`
under `graphvein serve --project` (every member) answer the UI's jump-to
palette ([serve](../../cli/serve.md#get-apiv0searchqkindslimit)). The
search reads the stored index through the `store` interface; it never
reads a file's text, never runs code and never states that something does
not exist.

## Vocabulary

- **Key**: a text of a result that the query is matched against (a route
  path, a node's name, a file path). Each kind has a fixed list of keys.
- **Fold**: ASCII case folding only (`A`..`Z` to `a`..`z`); every other
  byte is compared as stored.
- **Word start**: a byte position of a key that begins it, follows a byte
  that is neither an ASCII letter nor a digit, or is an upper-case ASCII
  letter that follows a lower-case one (`getUser`: `U`) or a letter that
  follows a digit or a digit that follows a letter.

## Claim contract

| Row | Topic | Rule |
|---|---|---|
| JS-1 | query | `q` is required; leading and trailing white space is dropped; 1..200 bytes of valid UTF-8 remain, else 400 `bad_request`. The query is one string (no tokens, no operators) matched as a folded substring of a key. |
| JS-2 | kinds | `kinds` is a comma list of `route`, `operation`, `table`, `symbol`, `file`; absent means all five. An unknown or repeated kind is 400. A kind not asked for is absent from the answer, never reported as empty. |
| JS-3 | limit | `limit` is the number of items shown per kind: 1..50, default 10; anything else is 400. |
| JS-4 | routes | Stored `route` nodes (HTTP routes, production and test). Keys: every full path entry's served path, the stored `pattern`, the unknown-prefix path the item is labelled with when it has no production full path (key `prefix_unknown`: the label without its leading `…`, so any contiguous part of the shown label after the `…` finds the route; the `…` itself is matched only by the name), the node name. An item carries `method`, `path` (the production path first, as the endpoint list chooses it), the unknown-prefix path when the route has no full path, `file`, `line`, `test` (node scope `test`). Its `label` is the path, else the unknown-prefix path, else (no path of any kind stored: `path` and `prefix_unknown` both null) the stored pattern, or the node name without one, followed by " (shadowed)" when its `shadowed_paths` are stored, " (path withheld)" when it is a couldn't-read CR-44 registration, else " (path not known)", so it is never shown or kept in recent jumps as a bare pattern (ADR 0089 Revisions 4 and 5). |
| JS-5 | operations | The operations of the generated OpenAPI document of the repository, built by the same function as `GET /api/v0/openapi`. Key: the path key. An item carries `method`, `path`, the route identity, `file`, `line`. When `graphvein.yaml` does not load, the document cannot be built: the kind is answered `available: false` with the reason, the other kinds are answered as usual. |
| JS-6 | tables | Stored `table` nodes (migration and model tables). Keys: name, qualified name. An item carries the stored `source` attribute (`migration`, `model`, or null), `file`, `line`. |
| JS-7 | symbols | Stored level-4 nodes whose kind is one of `annotation`, `class`, `constant`, `constructor`, `enum`, `extension`, `extension_type`, `function`, `interface`, `macro`, `method`, `mixin`, `record`, `struct`, `trait`, `type`, `type_alias`, `union`, `variable`. Keys: name, qualified name. An item carries `node_kind`, `qualified_name`, `file`, `line` and the server-built editor link. |
| JS-8 | files | Stored file records. Keys: the base name, the path. An item carries `language` and the editor link of the file's stored `file` node (none when the store has no such node). |
| JS-9 | tiers | An item's tier is the best over its keys: `prefix` (the folded key starts with the folded query), `word` (it occurs at a word start), `substring` (it occurs elsewhere). A key the query does not occur in gives no tier; an item none of whose keys match is not a result. |
| JS-10 | ranking | Within a kind: tier (prefix, word, substring), then the length of the key that gave the tier (shorter first), then that key in byte order, then the identity (path for files). |
| JS-11 | group order | Kind groups are ordered by their best tier, then by the fixed order route, operation, table, symbol, file. |
| JS-12 | totals | `total` counts every match of the kind; `items` holds the first `limit` of them and `truncated` says whether more exist. |
| JS-13 | searched | Every answer names what was searched per repository: the index run (null when not indexed), the stored file count, and the count of files Graphvein could not read and of scanner gaps under the couldn't-read rules (CR-19, CR-21). |
| JS-14 | no result | No answer states that a thing does not exist: a kind with no match has `total: 0` and the answer's `absence` is always `not_proven` (files not read, scanner gaps, languages without a reader and parts not modelled are not searched). |
| JS-15 | project scope | Under `serve --project` without `repository`, every member is searched; `first=<member>` puts that member first, then manifest order. Each member reports its own totals per kind and its `searched`; the project totals are their sums. An unknown `first` is 400. |
| JS-16 | project order | In project scope each item names its `repository`; within a kind group items are ordered by tier first (JS-9), then the first member's items before the other members' in member order, then the rest of JS-10 (key length, key, identity); `limit` applies to the merged group. A better match of another member comes before a weaker match of the first member. |
| JS-17 | no checkout | A member without a checkout is searched from the store; its items have no editor link. |
| JS-18 | not indexed | A repository or member without a stored run has `index: null`, no items and `searched.indexed: false`. |
| JS-19 | editor | `editor=<preset>` builds the links from that preset, as on every node-presenting endpoint (unknown or empty: 400). |
| JS-20 | everything else | Other node kinds (calls, columns, locations, external nodes, components), attributes, diagnostics, file contents and the files Graphvein could not read are not searched. |
| JS-21 | in-process reader | `/api/v0/search` is a `server.ReadPaths` view: the in-process reader answers the same JSON as HTTP. No MCP tool reads it. |
| D | default | A form no row above covers is not searched and claims nothing. |
