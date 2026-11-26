# change-impact fixture

Own code (no third-party text) for T019a2: three revisions of one small
repository and two NDJSON streams, compared by
`internal/changes/golden_test.go` on SQLite and Postgres. The repository has
an Express server (`server/`) with a fetch client (`web/api.ts`), a sqlx
migration set (`db/`, with `Cargo.toml`), a SQLModel model file
(`models/app/models.py`), a Tauri command with its `invoke` call
(`src-tauri/`, `web/ipc.ts`), an Angular component pair (`ui/`), a Next.js app with server actions (`site/`, T024b R1) and a React app with one react-router
route (`react/`, identical in rev1 and rev2). No file
needs an inert name: there is no ignore file and no ignored directory.

Code-to-table sites (T019 Revision 4, row I8; identical in every revision
except `count_rows`): `db/src/lib.rs` runs sqlx queries on the migration
table `items` (`item_name`: `query_scalar!`, fact, executes; `rename_item`:
`sqlx::query(...)`, CT-S2, inferred `TA-V1`, writes; `all_items`: a
`query!` it builds but does not run, `role: builds`), and
`models/app/queries.py` queries the SQLModel table `Hero` (`heroes`:
`select(Hero)`, fact; `hero_by_id`: `session.get(Hero, ...)` with an
unannotated `session`, inferred `TA-V3c`, `mapped`; `largest_team`:
`select(Team, func.max(Hero.id))` claims `Team` and records a `site_partial`
unknown naming `Hero`, the "may also be used by" entry). `Team` exists in
every revision only for that site.

The MCP schema session (`TestSessionGoldens`, internal/mcp, golden
`internal/mcp/testdata/session-schema.golden.json`) indexes rev1, pins it
as `rev1`, then commits rev2 to a git repository built at test time (tag
`rev2`, no ref snapshot stored) and indexes it.

Goldens (`../change-impact.<case>.golden.json`, each the
`graphvein changes --json --tests` document plus the impact of named seeds,
with run ids, times and commits numbered):

| case | comparison |
|---|---|
| `previous-rev1-rev2` | rev2 against the previous run (rev1) |
| `previous-rev2-rev3` | rev3 against the previous run (rev2) |
| `pin-rev1-rev3` | rev3 against a pin made at rev1, two runs later |
| `ref-rev1-rev3` | `ref:rev1..rev3`: a git repository built at test time from rev1 and rev3, both indexed as ref snapshots |
| `extractor-upgrade` | `streams/v2.ndjson` (indexer version 2) against `streams/v1.ndjson` (version 1) |

## rev1 to rev2

| change | rows |
|---|---|
| `server/items.ts`: `formatRow` body changed; callers `formatAll`, `Formatter.first`, `loadItems`, `listItems`, the file `server/boot.ts` (a top-level call), imported by `server/main.ts`; `server/boot.ts` also has an unresolved `helpers.formatRow(...)` call | C2 `body_changed`; I1, I10; a blind spot |
| `server/blind.ts` (unchanged in every revision): an unresolved `o.formatRow(2)` in a file the traversal from `formatRow` never reaches | a second blind spot (Revision 3: blind spots scan every file) |
| `server/slug.ts` renamed to `server/text.ts` (same `slugify`) | C2 removed + added, C14 hint (`content_hash`), C1 removed + added |
| `app.use('/api', router)` becomes `/api/v2`: `GET /items/:id` keeps its identity, its path changes; `fetchItem` (`/api/items/7`) unchanged | C3 `changed` (paths, cause `relink`), C4 `changed` (status unmatched), C5 `link_lost` `target_changed` |
| `DELETE /api/legacy` registration removed; `removeLegacy` unchanged | C3 removed, C5 `link_lost` `target_removed` |
| `fetchReport` now fetches `window.location.href` | C4 removed + added (dynamic), C5 `link_lost` `call_absent` (worded `call_removed`: the call's removal proves the link's absence, Revision 1) |
| `listItems` body changed | C2 `body_changed`; I4 to its route, I3 to `fetchItems`' call, I2 to `fetchItems` |
| new migration `0003_item_price.sql` adds `items.price` | C10 added, C9 added; I7, and I8 from the column through its table `items` (four users, `column_use: not_extracted`) |
| `count_rows` reads `items` instead of `tags` | C2 `changed` with label `table_access_changed` (field `reads_table`: `tags` lost, `items` gained; Revision 4) |
| applied `0002_tags.sql` edited (`label` NOT NULL) | C10 `changed` with label `edited`, C9 `changed` (`nullable`) |
| `Hero.age` added to the SQLModel model | C11 added; I8 from the column to `heroes` (fact) and `hero_by_id` (inferred, mapped), `largest_team` "may also be used by" |
| two comment lines inserted above `app.get('/api/stable', ...)` | G2: route and edges counted as moves |
| `web/broken.ts` given a syntax error (`syntax_error` warning, a knowledge gap) | G3: `helperTwo` and its edge `unknown_now`; C15 `became_partial` |
| Tauri command `farewell` removed with its registration; `sayBye` still invokes it | C7 removed (command, `exposes_command`), C7 `link_lost` `target_removed`; I5 and I2 from the removed command |
| `site/` (a Next.js app, T024b R1): server action `dropNote` and its direct call removed, `pinNote` and a call of it added | C18 removed and added (`server_action`, `action_call`, `exposes_action`), C17 `link_lost` / `link_gained` |
| file nodes; `exposes_route` edges of the removed route and the moved routes; the `alters` edge of the new migration | Revision 1: file nodes are C1 context (counted, no item); the edges are moves or context of their route (C3) or migration (C10) item |

## rev2 to rev3

| change | rows |
|---|---|
| syntax error fixed | G3 `known_now` (`helperTwo`), C15 `recovered` |
| `0004_tags_cleanup.sql`: `ALTER TABLE tags DROP COLUMN label` then a `DO` block (opaque) | C8 `changed` (completeness `complete_in_source`, cause `relink`), C9 `unknown_now` (`table_not_complete`), C10 added; I7 (`alters`) and I8 from `items`: "reads this table" (`item_name`, `count_rows`), "may write this table" (`rename_item`), "builds a query on this table" (`all_items`) |
| `Hero` gets `__tablename__ = "heroes"` | C11 `changed` (`name`); I8 from the table to its users as above (the edges name the table node, whose identity follows the class) |
| `ChildComponent` changed; `ParentComponent` now also renders a new `BadgeComponent` | C2 `body_changed`, C12 added (`renders`, `scope_imports`); I9 from `ChildComponent` to `ParentComponent` |
| `react/`: `ArticleContent` and `articleLoader` bodies changed; `ArticlePage` also renders a new `ArticleMeta`; new `PreviewPage` rendering `ArticleContent` with a new route `/preview` | C2 `body_changed` and added; C17 added (`ui_route` `/preview`, `renders`, `routes_to`); C16 added (`passes_function` from the router file); I14 from `ArticleContent` to `ArticlePage`, `PreviewPage` and both routes; I15 (inferred) from `articleLoader` to the `/article` route and the router file |
| `web/extra.ts` deleted | C1 removed (`deleted`: the working tree lacks it, Revision 3 G8), C2 removed (`no_longer_in_index`) |
| `GET /api/stable` now handled by the new `stableV2` | C3 `changed` (`handler`, `handler_edges`) labelled `handler_changed` (Revision 1: the `exposes_route` edges are reported on the route), C2 added |

The ref case shows rev1 to rev3 in one comparison, with `items.price`
`known_now` (the head table is `complete_in_source`), the removed route's
impact read from the base namespace, and file removals worded `deleted`
(G8: the head commit's tree lacks them). Revision 3: a `contains` (or
`imports`) edge gained or lost with a node that has a C3-C12 item is
`context` of that item, not a C16 item of its own (`tags.label`'s
`contains` edge next to its C9 `unknown_now`).

## streams

`streams/v1.ndjson` and `streams/v2.ndjson` are one repository (`streams`)
from a fictitious indexer, versions 1 and 2. `src/a.ts` changes (alpha: C2
`body_changed`, cause `code`); in the unchanged `src/b.ts` the route's paths
change (C3 `changed`, cause `extractor_or_relink`); the OpenAPI operation's
status changes and its `documents_route` edge is removed (C6; I6 from the
operation through `calls_operation`); `src/c.ts` gets an error diagnostic
and loses `gamma` (`unknown_now`, C15 `became_partial`); a route `GET
/owners` with no handler gains an `exposes_route` edge from `beta` (C3
`changed`, `handler_changed`, the route itself unchanged); the migration
`db/m.sql` gains an `alters` edge to its table (C10 `changed`,
`alters_changed`); a `widget` node is D (the only kind no row names); a
level-5 `location` node moves (counted as a location). `src/d.ts` holds 40
unchanged filler functions so the journal budget keeps the first run.