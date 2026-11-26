# ADR 0023 — Link-time OpenAPI declarations

**Status:** Accepted (developer review, 2026-10-18)
**Date:** 2026-10-18

## Context

T013a imports declared API contracts without claiming server implementation.
Revision 1 replaces per-file detection caching and exact YAML token ends with
link-time processing and a deterministic entry-region rule. Scanner-retained
JSON/YAML sources already have file records and file nodes.

## Options considered

- Cache parsed documents: unnecessary for the measured census and would require
  cache/version changes; cross-file results still must be recomputed.
- Add a parser or lexer dependency for exact YAML token ends: outside scope.
- Use the existing YAML node positions and JSON decoder offsets, with separate
  documented span rules: chosen by Revision 1.

## Decision

Add `internal/extract/openapi` after existing client/route linking. On every
index, discover, parse and resolve current scanner bytes, then emit contract-v1
`api_spec` and `api_operation` source nodes and ordinary fact contains edges.
Keep existing file records, caches, extractor version `extractors-14`, storage
schema, dependencies and contract unchanged. `index.Summary.OpenAPI` reports
the pass duration. No network requests or extra filesystem reads occur.

Strict `openapi.include/exclude` reuse the tests pattern decoder. A small
`testscope.Rules.Match` method exposes explicit matches without built-in test
names; `Classify` still labels test scope independently. Exclude wins.

Spec identity is `api_spec:<file>#spec`; operation identity is
`api_operation:<file>#<METHOD> <raw path>`, canonically escaped. Fixed verbs
are uppercase; 3.2 additional operation keys retain case. Root specs contain
operations even when their path item comes from a fragment. Server alternatives
are data only; no route/client links are introduced.

Resolve JSON Pointers, aliases, merge keys and scanner-visible local fragments
within each pass. Absolute references use only unique scanned confirmed `$self`
claims; never fetch a URI. Unknown 3.x minors use 3.2 rules with a warning and
`version_supported: false`; other unsupported versions emit no API nodes.

Revision 2 confirms versions using scalar source text, preserving numeric
spelling rather than converting numbers: unquoted YAML `2.0` and `3.1`, and
JSON numeric `3.1`, work like quoted strings. Version-shaped text is
`digits(.digits)*`; OpenAPI requires `3.<minor>` with optional further numeric
components and Swagger requires exactly `2.0`. Null/collection values and
non-version-shaped scalars are non-specs, silent unless explicitly included.
This also makes the project's mapping-valued `openapi` configuration silent.
Diagnostics describe missing values, document counts and unclaimed absolute
references directly; only ambiguous `$self` references list claimants.

### Source regions

All coordinates are LF-normalized, 1-based lines, 0-based UTF-8 byte columns,
end exclusive. Spec spans/hashes cover the whole file. Inline operations start
at the method/extra-operation key. Path-item references use the root path-key
entry and carry the final target `ref_file` and `ref_pointer`.

JSON entries start at the key's opening quote and end at the encoding/json
decoder offset immediately after the value's last lexical byte. Following
whitespace, comma and sibling keys are excluded, including with escaped strings.

YAML library columns count characters; convert to source byte offsets. An entry
ends at the next key in its mapping, or its parent's boundary, or EOF at root.
Move backward over whitespace/newlines; in flow mappings also remove one comma
and preceding whitespace. Comments before the boundary belong to the preceding
entry. The last flow entry may include enclosing closing delimiters. No span
includes another entry's key. Alias-valued operation entries keep their method
key; whole-mapping aliases propagate the alias occurrence region to inherited
operations rather than using anchor positions.
The library exposes merges as `!!merge` keys and alias/sequence values rather
than flattening them; inherited operations use the merge entry's region.

## Consequences

Configuration and fragment edits immediately update nodes without invalidating
parsed-language fact caches. Derived metadata may change while the root operation
hash stays unchanged. Existing goldens must not change; a separate fixture
captures API nodes, contains edges and diagnostics. Diagnostics isolate bad
files/references and name the file, pointer and available source region.

## Known gaps

Fragment edits do not change root reference-operation hashes. Anchor-only edits
outside an alias occurrence likewise can change attributes without changing
that operation's source hash. YAML comments and
enclosing flow delimiters can affect hashes under the specified safe-invalidation
rule. Server URLs without leading slash have unresolved deployment paths;
Swagger host without schemes supplies no guessed origins. Schemas, tag hierarchy,
route correspondence, generation/export/UI and runtime deployment inference
remain outside this task.

## Review notes (developer, 2026-10-18)

- Accepted. Corpus: 5 specs and 178 operations (React 19, RealWorld 19,
  Express 19 in apps/app/controller/getSwaggerJson/swagger.json, Langflow
  121 across docs/openapi/openapi.json and
  docs/openapi/langflow-workflows-openapi.json), zero OpenAPI diagnostics.
  All 253,204 existing records across nine checkouts unchanged; no existing
  golden changed; extractors-14 and all language caches unchanged.
- Langflow declares /api/v2/files and /api/v2/files/ as distinct operations;
  they keep distinct identities and trailing_slash flags for T013b's FastAPI
  slash-twin matching.
- The investigation stopped on S3 (yaml.v3 exposes no end positions). Revision 1 chose
  exact JSON spans, YAML entry-region spans and a cache-free link-time pass
  over retained scanner sources.
- Revision 2 confirms versions from scalar source text (unquoted swagger 2.0,
  openapi 3.1, JSON numbers) and treats non-version openapi/swagger values as
  non-specs, removing a warning on graphvein.yaml files with an openapi
  section. It stopped once on the task's allowlist; the storage parity test
  now derives counts from the golden.
- Timing against main: fresh -0.61%, warm +1.13%. The pass costs about
  595 ms per Langflow run, of which about 546 ms is the YAML column-0 regexp
  applied to JSON files; fix scheduled with T013b.
- Langflow's docs/openapi specs are production scope by path classification;
  tests.include or openapi.exclude can change that per project.
- Gaps: fragment-only and anchor-only edits do not change root operation
  hashes; YAML comments and enclosing flow delimiters fall inside spans;
  server URLs without a leading slash stay unresolved; Swagger host without
  schemes gives no origin; schemas are not extracted.
