# Claim contract: what Graphvein couldn't read

**Status:** in force (ADR 0089, accepted 2026-11-25). Rows `CR-1`..`CR-59` are reserved
for this contract; rows `CR-1`..`CR-44` and the default row `D` are in
force, the others are kept for a later reported contract change (rows
`CR-34`..`CR-39` and the changes to `CR-10` and `CR-19` are the T040
server follow-ups, ADR 0089 Revision 1; rows `CR-40`..`CR-43` and the
change to `CR-33` are ADR 0089 Revision 2; the `prefix_unknown` field of
`CR-6` is Revision 3; row `CR-44`, the split of `CR-42` and the labels of
`CR-16` are Revision 4; the label words of `CR-16` and `CR-44` are
Revision 5). The code
table is `unreadRows` in `internal/server/unread.go`, and
`TestUnreadContractTable` keeps it 1:1 with the table below: every row is
in both, in the same order with the same topic, and names a test that
exists.

`GET /api/v0/unread?scope=&limit=&editor=` (one repository, or one project
member with `repository=`) and the same path without `repository` under
`graphvein serve --project` (every member plus the project totals) serve
the UI's "What Graphvein couldn't read" page
([serve](../../cli/serve.md#get-apiv0unreadscopelimit)). Every part is
read from the stored index through the `store` interface; a part another
endpoint already serves is computed by that endpoint's code, so one rule
never has two implementations.

## Vocabulary

- **Built-in run**: the repository's latest stored run was written by
  Graphvein's own indexer (header indexer name `graphvein`). Parts that
  depend on what the built-in readers do are not available for an
  external indexer's run.
- **Reader**: a language the built-in indexer parses (`index.Reads`):
  TypeScript, TSX, JavaScript, Go, Python, Rust, HTML (Angular templates),
  Java, C# and Dart.
- **Fact from a file**: a stored node other than the file's own `file`
  node whose `file` is that path, or a stored edge whose evidence names
  that path.

## Claim contract

| Row | Topic | Rule |
|---|---|---|
| CR-1 | scope | `scope` is `production` (default), `test` or `all`. Routes and client calls are in scope by their stored `scope` attribute, as on the endpoint list; files by the test rules of `graphvein.yaml` (`tests`). |
| CR-2 | built-in run | The files-without-facts part (CR-22) and the unresolved-call totals (CR-25) are `available: false` with the reason `external_indexer` when the latest run is not a built-in run. |
| CR-3 | not indexed | A repository without a stored run answers `index: null` and every part `available: false` with the reason `not_indexed`; nothing is counted as zero. |
| CR-4 | lists | Every list holds at most `limit` items (1..2000, default 200) and carries `total` and `truncated`. Counts are never capped. |
| CR-5 | items | Each item names a node (identity and the server-built editor link) or a file and line; `line` is null when the store holds none. |
| CR-6 | routes without a path | The production routes the generated OpenAPI document could not place (`x-graphvein.unresolved`, the explorer's `unresolved` list), with the reason and the keys a path value would fill, and `prefix_unknown`: the route's production unknown-prefix path as the endpoint list writes it (`…` for the unknown part), or null when it has none. The stored `pattern` is never given as the route's path. |
| CR-7 | unknown mount hops | The unresolved mount hops of the generated document (the explorer's `mounts` list), each with the number of production routes below it. |
| CR-8 | document not built | When `graphvein.yaml` does not load, CR-6 and CR-7 are `available: false` with the load error; the other parts are answered. |
| CR-9 | patterns built at run time | Stored `route_dynamic_pattern` and `route_dynamic_methods` diagnostics. The expression and line come from the documented message form `<expression> at L:C-L:C`; a message not in that form keeps the message and has `line: null`. |
| CR-10 | router forms not read | Stored `go_routes_unread` diagnostics, one item per file with the message verbatim and the structured counts of CR-38. |
| CR-11 | dynamic client calls | HTTP `client_call` nodes in scope whose stored `status` is `dynamic` (the `/api/v0/api/calls` status rule). |
| CR-12 | wrapper client calls | HTTP `client_call` nodes in scope whose stored `status` is `wrapper`. |
| CR-13 | call items | A call item carries its owner (as `/api/v0/api/calls` reports it), `file`, `line` and the editor link. |
| CR-14 | routes without a shape | HTTP `route` nodes in scope with no stored `shape` attribute: every section is not read. A stored shape that does not decode is counted apart (`shape_invalid`). |
| CR-15 | sections not read | For each section (`path`, `query`, `header`, `cookie`, `body`, `responses`, `security`), the routes in scope whose stored shape has that section `not_read`. |
| CR-16 | shape items | A route item lists its not-read section names, in that section order. It is labelled by its production full path, else its unknown-prefix path (`…` for the part not known), else its pattern (or name) followed by what is stored about it: "(shadowed)" when its `shadowed_paths` are stored, "(path withheld)" when it is a CR-44 registration (the diagnostic names the route's span), else "(path not known)"; a route with no path stored is never labelled by its bare pattern, and never "not mounted" (ADR 0089 Revision 5). |
| CR-17 | depth caps hit | Routes in scope whose stored shape stopped at least one chain at the depth cap (the compact shape's `depth_cap`), with that count and the depth limit used. |
| CR-18 | file test split | Every file item has `test` (true, false, or null when `graphvein.yaml` does not load and the rules are unknown); file lists and counts follow `scope`, and with unknown rules every file is kept and the scope is reported as not applied. |
| CR-19 | files not read | Files with a `file_too_large`, `parse_failed` or `extract_failed` diagnostic (stored, content not read or not extracted) or a `file_read`, `file_binary`, `file_not_utf8` or `file_not_regular` diagnostic (found by the scanner and not read, so no record is stored: `stored: false`; CR-34). The same code set is the schema views' "not read" rule. |
| CR-20 | read with syntax errors | Stored files with a `syntax_error` warning: tree-sitter recovered, and the readers claim nothing beyond the file there (the change journal's knowledge-gap code). |
| CR-21 | scanner gaps | Stored `scan_read`, `symlink`, `invalid_path` and `ignore_read` diagnostics: a path the scanner did not index (a directory not listed, a symlink not followed, a path skipped, ignore rules unknown). They are listed, never counted as files. |
| CR-22 | files without facts | In a built-in run: stored files whose language has no reader and from which no fact is stored, grouped by extension. They are described as "no fact stored from this file", never as skipped, unread or failed: other passes read some such files (Dockerfiles, `go.mod`, compose files hold facts and are not listed), and project suggestions read `.env` files without storing a fact. |
| CR-23 | files with a reader | A file whose language has a reader is never listed by CR-22, whether or not a fact is stored from it. |
| CR-24 | counted languages | Unresolved in-code calls are counted by the built-in Go, Python, TypeScript (TypeScript, TSX, JavaScript) and Rust readers only (`index.CountsUnresolvedCalls`). |
| CR-25 | unresolved call totals | The totals are the sums of the stored `file` node attributes `unresolved_calls_external` and `unresolved_calls_unknown` over the counted files, never read from a diagnostic message. |
| CR-26 | counted files | A file is counted when its language is counted (CR-24), it has no CR-19 or CR-20 diagnostic, and its `file` node holds both attributes as non-negative integers. Every other file of a counted language is in `files_not_counted`, so the totals are "in the files counted". |
| CR-27 | call items | Counted files with at least one unresolved call, by total (descending), then path. |
| CR-28 | member | Under `serve --project`, `repository=<member>` answers that member's page. |
| CR-29 | project scope | Without `repository`, the answer holds every member's page and the project totals: each count is the sum over the members; when a part is not available for a member, its project total is null and the members are named. |
| CR-30 | no checkout | A member without a checkout is answered from the store: no editor links, and its test rules are the built-in defaults (it has no `graphvein.yaml` here), reported as `config: "defaults"`. |
| CR-31 | sidebar total | No answer adds counts of different units (routes, calls, files, hops) into one total. |
| CR-32 | wording | The answer carries no wording that something was "unread" except for CR-19's files; every part says what it is from its rule. |
| CR-33 | everything else | Other diagnostics (every code no row above names), IPC and server-action calls, model and schema gaps (served by the Schema endpoints) and facts an external indexer stores are not on this page. |
| CR-34 | scanner skips | The built-in scanner records every entry it saw, did not read and has no other record for as a diagnostic on its path: `file_binary` (the file holds a NUL byte; info; a `.rs` file that is valid UTF-8 and holds at most 8 NUL bytes, or one per KiB, is read as source instead: Rust allows a raw NUL inside literals and comments), `file_not_utf8` (the file is not valid UTF-8; warning), `file_not_regular` (not a regular file, directory or symlink: a FIFO, socket or device; info). They are files not read (CR-19); the over-size rule already has its record (`file_too_large`). |
| CR-35 | deliberate exclusions | Paths left out by rule get no record and are never listed: matched by `.gitignore` or `.graphveinignore`, a default name (`.git`, `node_modules`, `dist`, `build`, `target`, `vendor`, agent folders and the others of the scanner's list), or a nested repository (a directory holding `.git`, another checkout). The page tells them apart by the record alone: only a CR-19 or CR-21 diagnostic is listed, and the scanner writes none for an excluded path. |
| CR-36 | file categories | Every item of files without facts (CR-22) and of files not read (CR-19) has `category`, from its lower-cased final extension: `code_with_reader` when the scanner's extension rule gives a language a built-in reader parses (`.py`, `.ts`, `.go`, ...; never a file without facts, CR-23), else from the extension table below `code_no_reader` (source of a programming, style sheet, query or component language without a reader) or `not_code` (documentation, images, fonts, media, archives, data and configuration), else `other` (an extension in no row, or none). The category describes the file name only and claims nothing about the content. |
| CR-37 | category counts | Files without facts and files not read each carry `by_category`: the count of every category in the order `code_with_reader`, `code_no_reader`, `not_code`, `other` (all four present, zero included), summing to the list's `total`; `[]` where the part is not available. |
| CR-38 | router form counts | The built-in indexer stores, on the `file` node of each file with a `go_routes_unread` diagnostic, the attribute `go_route_forms_outside_contract` (`{form: n}`, the counts of that diagnostic, from the same linker result). Each CR-10 item has `forms` (the sum) and `by_form`, both null when the file node holds no valid attribute (n a positive integer); `route_paths.forms` is the sum over the items with counts and `route_paths.form_files_not_counted` the number of items without; the message is never parsed. |
| CR-39 | in-process reader | `/api/v0/unread` is a `server.ReadPaths` view: the in-process reader answers the same JSON as HTTP. No MCP tool reads it. |
| CR-40 | records dropped | Stored `extract_invalid_record` diagnostics in scope (CR-1, CR-18; one without a file is in every scope), one item per diagnostic with the message verbatim (`files.records_dropped`): a record extracted from the file did not validate and was dropped, and when it was a fact record no fact from that file is stored. The message names the record; it is never parsed. |
| CR-41 | route recognition failed | Stored `go_routes_failed` diagnostics in scope, one item per diagnostic with the message verbatim and `line: null` (`route_paths.failed`; one without a file is a run-wide failure): Go route recognition, linking or composition failed there, so those routes are not known. |
| CR-42 | mounts not resolved | Stored `route_mount_unresolved` diagnostics in scope that are not CR-44 items (`route_paths.mounts_unresolved`): a mount whose router or prefix argument is not resolved; no path is claimed through it. The expression and line come from the CR-9 message form; any other message is kept with `line: null`. |
| CR-43 | structure not read | Stored `ts_structure_unread` diagnostics in scope, one item per file with the message verbatim (`files.structure_outside_contract`): TypeScript or JavaScript declarations or imports outside the structural claim contract, which claim nothing. No count is read from the message. |
| CR-44 | route paths withheld | Stored `route_mount_unresolved` diagnostics in scope of a `.go` file whose expression is a route registration, `METHOD "pattern" (reason)` (`route_paths.routes_withheld`): the Go linker found the route but withholds its path, because another pattern of the ServeMux, its redirect, a forwarding closure or a route of the router it is mounted on may answer it; no path is claimed. Counted in routes (`routes_withheld`), apart from the mounts of CR-42; the expression and line come from the CR-9 message form. The page words them "routes whose path is withheld"; the same classification (`routes.IsWithheldRegistration`) marks the route `path_withheld` in the endpoint list and detail and in the change views (ADR 0089 Revision 5). |
| D | default | A form no row above covers is not shown and claims nothing. |

## File categories (row CR-36)

The extension table is `fileCategories` in
`internal/server/filecategory.go`; `TestUnreadCategories` keeps it equal
to this table. An extension in no row is `other` unless a reader parses
it (`code_with_reader`).

| Category | Extensions |
|---|---|
| code_no_reader | .astro, .bash, .bat, .c, .cc, .clj, .cljs, .cmd, .coffee, .cpp, .css, .cxx, .elm, .erl, .ex, .exs, .fish, .fs, .gql, .gradle, .graphql, .groovy, .h, .hh, .hpp, .hs, .jl, .kt, .kts, .less, .lua, .m, .ml, .mm, .nim, .php, .pl, .pm, .proto, .ps1, .r, .rb, .sass, .scala, .scss, .sh, .sol, .sql, .svelte, .swift, .tf, .vb, .vue, .zig, .zsh |
| not_code | .adoc, .avif, .bmp, .cfg, .conf, .csv, .env, .eot, .gif, .gz, .ico, .ini, .jpeg, .jpg, .json, .json5, .jsonc, .lock, .log, .markdown, .md, .mdx, .mov, .mp3, .mp4, .ogg, .otf, .pdf, .png, .properties, .psd, .rst, .svg, .tar, .tgz, .tif, .tiff, .toml, .tsv, .ttf, .txt, .wav, .webm, .webp, .woff, .woff2, .xml, .yaml, .yml, .zip |
