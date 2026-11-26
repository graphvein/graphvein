# ADR 0085 - Committed bundler output is not linked

**Status:** Accepted (delegated review, 2026-11-23)

## Context

Some repositories commit their front end's build output: owncast serves
its Next.js export from `static/web/`, so `static/web/_next/static/chunks/`
holds 539 bundler-written JavaScript files, most of them a few minified
lines. Graphvein indexes every visible file, and the client call extractor
reads `fetch` and axios calls in a minified chunk as it reads them in
source.

The link precision measurement (`docs/evidence/link-precision.md`) found
that this output adds no correct link and causes false ones. On owncast at
extractors-57, 35 of 59 `calls_route` links came from 29 chunks: 32
repeated a link of the source the chunk was built from (same method,
route and template up to parameter names) and 3 were false. Of owncast's
12 false links, 11 came from chunks: minified code reuses one-letter names
in many scopes, defeating the rules that keep wrapper invocations and
literal origins precise (T034 rows LF-1 and LF-3). After the T034 rules,
25 links from 21 chunks remain, all 25 duplicating a source link. No other
of the 34 pinned corpora commits bundler output with client calls.

The developer decided (2026-11-23) that committed bundler output is treated
as generated: still indexed, its client calls not linked to routes, a
per-file diagnostic saying why, and a `graphvein.yaml` opt-in that links it
again.

## Options considered

1. **Leave it linked** (the current behaviour). Duplicated links inflate
   every caller list and count, and the false ones are confident wrong
   facts. Rejected by the developer's decision.
2. **Skip the files entirely** (as an ignore rule would). The chunks then
   have no file node, no symbols and no `calls` edges, although they are
   real, served code a reader may want to open. A per-repository ignore
   already gives this to a team that wants it; it is not a safe default.
3. **Index them, record their client calls, link none of them** (chosen).
   The call nodes stay visible (with a status that says why), nothing else
   about the files changes.
4. **Link them at a lower confidence.** A duplicate is still a duplicate,
   and the precision rules that minified code defeats would still decide
   the link. Rejected.

Detection was considered as (a) a minified-content heuristic alone, (b)
bundler output layouts alone, and (c) a closed list of layouts each
confirmed by minified content. (a) also hits committed vendor files and
data modules written on one line (gladys has hand-written source files with
lines over 1,000 bytes); (b) hits a development build or a hand-written
file placed in a build directory (owncast has 87 unminified Turbopack
loader stubs in `_next/static/chunks/`). (c) is chosen.

## Decision

1. A file is **committed bundler output** when its repository path matches
   one row of a closed list of bundler output layouts **and** it is
   **minified** as a whole: its dense long lines (at least 1,000 bytes,
   `clients.MinifiedLine`, with at least one `;` or `}` per 100 bytes)
   hold at least half of its bytes. A long `data:` URI, base64 string or
   one-line data table in a hand-written file is not dense code, and one
   long line in a longer file does not hold half of it (T034 confirmation
   review). owncast's chunks with client calls have 96% or more of their
   bytes in dense long lines:

   | Row | Bundler | Layout (path pattern) |
   |---|---|---|
   | GB-1 | Next.js (`next build`, webpack or Turbopack) | `_next/static/` anywhere in the path, file `.js` or `.mjs` |
   | GB-2 | Vite (Rollup) default `build.assetsDir` and file names | `assets/<name>-<hash>.js` (or `.mjs`), hash 8 characters of `[A-Za-z0-9_-]` with at least one digit or capital letter (a lower-case-only part such as `settings` is a word; Graphvein's own Vite 8 build has 19 hashes, 6 without a digit and all with a capital or digit, so requiring a digit would miss about a third) |
   | GB-3 | webpack as Create React App configures it | `static/js/<name>.<hash>.js` or `.chunk.js`, hash exactly 8 hex digits (CRA's `[contenthash:8]`; a longer configured hash is not detected, a miss) |
   | GB-U | anything else, a layout path whose file is not minified included | every client rule, unchanged |

   The list is closed: a layout is added only with evidence of the
   bundler's own output naming, a fixture case, and a corpus measurement.
2. Such a file is indexed as before (file node, symbols, `calls` edges,
   routes if any). Its client calls are recorded as `client_call` nodes
   with `status: "generated"` and `generated_output: "<row>"`; each path
   entry with a template gets status `generated`. They have no
   `calls_route` edge and no candidates. A wrapper's own request keeps
   status `wrapper`. OpenAPI correspondence (`documents_route`) is not
   affected: no bundle documents a route.
3. Each such file with at least one client call gets one info diagnostic,
   `client_calls_generated_output`, naming the bundler, the row, the
   evidence, the number of calls, and the opt-in.
4. `clients.link_generated_output: true` in `graphvein.yaml` turns the rule
   off: every call is linked by the ordinary rules and no diagnostic is
   written. It is read on every link pass; no reparse.
5. The project link pass treats `generated` as a status the member's own
   index decided (row PL-R1): it never links such a call across members.
6. The local server documents the status (`generated`, reason
   `generated_output` in `docs/cli/serve.md`); the web UI shows it as an
   other status with its raw value.

## Consequences

- owncast loses its 25 duplicate links from `static/web/_next/static/chunks/`
  (each confirmed to duplicate a source link); its source links are
  unchanged. No other corpus changes.
- The status vocabulary of `client_call` gains `generated`, an additive
  value inside contract v1 (open attributes; consumers that do not know a
  status treat it as unknown, which the local server already does).
- The index of a repository with committed output shows those calls as
  not linked, with the reason, instead of as duplicate callers.

## Known gaps

- Bundlers and layouts outside the three rows (Angular CLI, Parcel,
  esbuild with custom names, Vite with a configured `assetFileNames`, a
  Next.js `distDir` renamed and committed) keep every rule.
- A minified file inside a layout path is classified even if a person
  wrote it (unlikely by construction of the layouts).
- Bundler output that is not minified (a development build) keeps every
  rule, as do source maps' original sources (`sourcesContent` is not
  read).
- Detection reads only the file's own path and bytes: a bundle whose
  minification puts every statement on short lines is not recognized.

## Review notes (2026-11-23)

- Review: one independent review of T034 (on the link rules) and a confirmation pass that found two more issues (an 8-letter word taken for a Vite hash, one long `data:` line taken as minified: hand-written assets lost correct links) and C5 (GB-3 hash length). Revisions: minification decided per file (half the bytes in long dense lines), the GB-2 hash needs a digit or a capital letter, GB-3 exactly 8 hex digits (Create React App's default). Both confirmed fixed by fixture cases.
- Results: gate run 45213718136 (Linux and Postgres; 0 FAIL, 239 `/postgres` PASS, only the opt-in oracle and census skips); local full check 0 FAIL, 238 `/postgres` PASS.
- Coverage per corpus: owncast 142 client calls in 62 bundle files marked `generated`, 25 links from 19 chunk files removed, all 25 duplicates of a source link; no other corpus of the 34 changes. Link precision after the change: owncast 22/22, seerr 174/174, langflow 167/167.
- Assumptions: the three layout rows mark committed build output; a person does not hand-write minified files in those layouts.
- Known gaps: as listed above. After v1: more bundlers and layouts as corpora show them; reading source maps.
