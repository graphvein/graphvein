# OpenAPI spec import

ADRs 0023 and 0024, contract v1, extractors-49. The link-time pass consumes only retained
scanner sources on every index. Existing JSON/YAML file records remain unchanged;
no detection or cross-file cache, extra reads or network fetches are added.

Discovery considers case-insensitive `.json/.yaml/.yml` extensions with an
openapi/swagger basename, a column-zero YAML version key (plain or quoted),
quoted JSON version-key bytes, or `openapi.include`. Exclude wins. Confirmation
requires a single mapping root with version-shaped scalar text: digits separated
by dots. Read strings and numbers by their written scalar text, without numeric
conversion: YAML `swagger: 2.0`, YAML `openapi: 3.1` and JSON `"openapi": 3.1`
all confirm specs. OpenAPI `3.<minor>` accepts further numeric components;
Swagger accepts exactly `2.0`. `spec_version` preserves that text. Null,
mapping, sequence and non-version-shaped values (including booleans) are
non-specs: silent unless explicitly included. Thus an `openapi` configuration
section in `graphvein.yaml` produces no diagnostic.
Duplicate keys and multiple documents are parse errors. Named
non-specs are silent; explicitly included non-specs are diagnosed. Prefilter-only
invalid JSON is silent; malformed YAML with a column-zero version key is not.

Supported minors are 3.0, 3.1 and 3.2. Later 3.x minors use 3.2 rules with
`version_supported: false` and a warning. Other versions emit a warning and no
API nodes. Fixed methods follow the version; QUERY and additionalOperations
require 3.2. Extra method names retain case and cannot duplicate fixed uppercase
verbs. Path keys must begin with `/`; extension keys are ignored.

Each confirmed document emits level-2 `api_spec:<file>#spec`, with format,
spec_version, version_supported, title, api_version, self and servers. Each
operation emits level-4 `api_operation:<file>#<METHOD> <raw path>` with method,
path, operation_id, summary, ordered tags, deprecated, merged parameters sorted
by location/name (null name first), sorted request content types, response codes,
security scheme names, server path alternatives and full path_count. Identity
uses contract escaping. File → spec → operation are fact contains edges using
the existing source-evidence convention. Test-scope specs/operations have
`scope: test` from the current classifier/configuration.

References cover path items, parameters, request bodies and responses. JSON
Pointers support percent decoding and `~0/~1`. Resolve relative references
against the file location or effective 3.2 `$self`. Relative `$self` is first
resolved against the file; a value leaving the repository is ignored with a
diagnostic. Absolute URIs resolve only through a unique scanned confirmed
document's matching effective `$self`; duplicate claims name all claimants.
An unclaimed absolute URI instead states that no scanned document claims it,
without an empty claimant list.
Missing files/pointers, remote URIs and cycles are diagnosed without preventing
other operations/files from being emitted. Fragment files get no API spec node
unless independently confirmed. YAML anchors, aliases and merge keys work.

Effective 3.x servers prefer nonempty operation, path-item, root, then `/`.
Substitute declared defaults; missing defaults or relative URLs without leading
slash yield null templates and unresolved resolution. Split absolute or
protocol-relative origins from base paths and discard server query/fragment.
Server URLs ignore `$self`, following the [3.2 server URL rule](https://spec.openapis.org/oas/v3.2.0.html#relative-references-in-api-urls).
Swagger uses basePath (default `/`) and effective schemes plus host. No origin
is guessed without both. Swagger body/formData operations use effective consumes.

Join base/path, collapse slashes, add a leading slash, remove trailing slashes
except root; preserve braces, literal colons, dot segments and percent escapes.
An original nonroot trailing slash sets `trailing_slash: true`. Deduplicate by
template/flag, merge sorted distinct origins, and prefer literal over
server_default. Null templates sort last. Emit at most ten paths plus the full
count. Declarations retain their identities and source hashes when matching changes.

## Route correspondence

After route composition, each non-null path entry uses the shared client matcher
at its exact tier only. Preserve method case for additionalOperations; fixed
methods are uppercase. A method containing any ASCII lowercase letter excludes
Express and FastAPI routes, including ANY: Node's llhttp parser and uvicorn's
httptools parser reject that spelling before framework dispatch. Axum's
hyper/http stack accepts case-preserving extension methods, so its ANY routes
remain eligible. Remaining candidates use the existing matching rules. See
[ADR 0024](../adr/0024-openapi-route-links.md) for source reasoning. Uppercase
methods outside a server's supported list (for example FOO on Node) remain a
known gap; alternative FastAPI server/parser configurations are not inferred.
ANY and Express/Axum implicit HEAD, specificity, FastAPI
slash twins and equivalent Express first registrations follow
[client matching](clients.md). Test-scope specs ignore route scope; production
specs retain production candidates in the winning tier, or report test_only
without an edge or weaker fallback. A null template is unresolved.

Each path has status (linked/ambiguous/unmatched/test_only/unresolved), match
(exact/null), candidates (at most ten sorted identities), and candidate_count
(full tie size for ambiguous/test_only with at least two; zero otherwise).
The operation is linked if all emitted entries link; otherwise take the worst
of unresolved > unmatched > test_only > ambiguous.

Each linked target receives one inferred documents_route edge, with the
operation's source span as evidence. Confidence is 0.9 when the entry has no
origins and 0.7 otherwise. Alternatives reaching the same target use the strongest
confidence. These edges describe inferred correspondence, not a declaration fact.
Route and client attributes are unchanged, except that a route linked by
exactly one edge shows the operation's parameters, bodies, responses and
security as `mapped` items of its `shape` attribute
([route shape](route-shape.md#committed-specs), claim contract
[route-shape-spec](../contract/claims/route-shape-spec.md)); clients never
consume these edges.

Optional [openapi.base_paths](../config.md#openapi) replaces server alternatives
for a spec with one configured base joined to its path keys, preserving slash
flags and normalization. The longest matching spec-file or directory key wins.
Origins become empty and resolution becomes configured. This repairs unresolved
relative server paths and overrides already resolved server paths. Configuration
edits relink without reparsing language sources.

## Generation

Graphvein also writes an OpenAPI 3.1 document from the extracted routes
([graphvein openapi](../cli/openapi.md), `GET /api/v0/openapi`; claim
contract [openapi-generation](../contract/claims/openapi-generation.md),
ADR 0081). The imported specs described above are never merged into it:
a committed spec can say more (summaries, parameters, bodies) or
something else than the code, and the generated document states only what
the code registers. The generated document lists each imported spec in
its root `x-graphvein.imported_specs` (file, title, version, operation
count and link statuses), and an operation whose route has a
`documents_route` edge names the committed operation and the edge's
confidence in `x-graphvein.documented_by`, so the explorer can show both
side by side. Nothing in this section changes the import, its nodes or
its edges.

## Spans and hashes

Coordinates use normalized LF bytes, 1-based lines and 0-based UTF-8 byte columns,
end exclusive. Spec spans/hash cover the whole file. Operation spans start at
the method key, or additionalOperations entry key. A referenced path item uses
the root path-key region and records its final ref_file/ref_pointer. Its hash
therefore does not change on fragment-only edits (known gap). Alias-occurrence
hashes similarly do not change when only their anchor body changes elsewhere.

JSON regions start at the key opening quote and end at the encoding/json token
offset immediately after the value. Whitespace, commas and following entries
are excluded. Escaped strings retain their original lexical width.

YAML converts library character columns using the source line. Boundaries are
the next key in the same mapping, otherwise the parent entry boundary, otherwise
EOF for root entries. Trim whitespace/line breaks backward; for flow mappings,
then trim one comma and whitespace again. Comments between entries, including
comments introducing the next operation, belong to the preceding entry. The
last flow entry can include enclosing flow closing delimiters. Another entry's
key is never included. Alias values retain the containing operation entry;
operations inherited through a whole-mapping alias use the alias occurrence
region, propagated through its descendants. Merges are
explicit `!!merge` nodes in the YAML library; inherited operations take the merge
entry region. These comment/delimiter gaps deliberately favor safe invalidation.

Diagnostics: warning openapi_parse_error/openapi_unsupported_version; info
openapi_ref_unresolved/openapi_invalid_path/openapi_invalid_operation/
openapi_not_a_spec. Messages include file/pointer and byte-column span when
available. Messages state the actual condition, such as a missing version value
or the number of YAML documents, without Go formatting artifacts.
When the route shape reader meets a schema reference cycle with no schema
in between, it records one info openapi_ref_unresolved ("reference cycle")
per entry schema.
Each emitted spec also receives one info openapi_coverage diagnostic, including
empty specs, reporting total operations and counts for every matching status.
The total reads "1 operation" for one, otherwise "N operations".
There are no per-operation coverage diagnostics. Unsupported versions that emit
no spec retain only their existing unsupported-version warning. The
`api_operation` nodes hold no schemas; schemas are read only into the
mapped route shape items above (diagnostic `openapi_shape_failed`, info,
when a mapped shape cannot be stored). documents_route query traversal
remains deferred to M5.

The fixture is `testdata/fixtures/openapi/`, with a separate API projection
golden. Tests copy `_graphvein.yaml` to `graphvein.yaml`: it includes the
non-spec `included.json` and escaped-key spec `explicit.json`, and excludes
`excluded-openapi.yaml`. No live ignore files or ignored directory names are
used. Tests restore CRLF in `crlf-openapi.yaml` after copying, independently of
Git's LF normalization; `eof-openapi.yaml` deliberately has no final newline.
`fragment.yaml` supplies reference targets without being a root spec. Independent
span assertions cover flow comments, block scalars, inter-operation comments,
escaped JSON, Unicode byte columns, aliases and merge keys.

Revision 2 adds `unquoted-swagger.yaml`, `unquoted-openapi.yaml` and
`numeric-openapi.json`, each with one operation, plus silent non-specs
`settings.yaml` (mapping-valued `openapi`) and `toggle.yaml` (boolean-valued
`openapi`). The activated configuration is also silent. The API golden has
17 specs, 33 operations and 12 diagnostics; only this fixture's golden changes.

T013b adds coverage diagnostics (17 in the activated existing fixture) and
matching attributes to that golden. `testdata/fixtures/openapi-routes/` has a
separate route-link golden with 24 operations and 18 inferred edges. The lowercase
query operation on `/t013b/any` is unmatched under Revision 1. Its
`_graphvein.yaml` is activated only in an isolated fixture copy; the self-index
keeps it inert. All new URLs use `/t013b` to avoid cross-fixture links.
