# Link precision of route <-> caller links

This page measures how often Graphvein's `calls_route` links (a client call
site linked to the HTTP route that serves it) are right, on three messy
open-source repositories. It also measures how many client calls that have
a serving route were left without a link. It gives the method, the corpus
pins, the results and the limitations. Every adjudicated link and every
sampled unlinked call is in the data files under [`data/`](data/), with its
evidence, so each verdict can be checked again.

## Headline

Two measurements of the same three corpora at the same pins. The first
(extractors-57) is the published measurement. The second re-measures
after the class rules LF-1 to LF-3 of [client matching](../extract/clients.md#link-precision-rules-t034)
closed the three classes of false links it found (extractors-58, see
"Re-measurement after the fix" below).

| corpus | extractors-57: links | correct | wrong | precision [Wilson 95%] | false-link rate [Wilson 95%] | extractors-58: links | correct | wrong | precision [Wilson 95%] | false-link rate [Wilson 95%] |
|---|---|---|---|---|---|---|---|---|---|---|
| langflow | 167 | 167 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] | 167 | 167 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |
| owncast | 59 | 47 | 12 | 79.7% [67.7%, 88.0%] | 20.3% [12.0%, 32.3%] | 47 | 47 | 0 | 100.0% [92.4%, 100.0%] | 0.0% [0.0%, 7.6%] |
| seerr | 184 | 174 | 10 | 94.6% [90.3%, 97.0%] | 5.4% [3.0%, 9.7%] | 174 | 174 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |
| all three | 410 | 388 | 22 | 94.6% [92.0%, 96.4%] | 5.4% [3.6%, 8.0%] | 388 | 388 | 0 | 100.0% [99.0%, 100.0%] | 0.0% [0.0%, 1.0%] |

The fix removed exactly the 22 false links and no correct one; it added
no link. The price is recall on seerr: the three calls of class 2 are now
`ambiguous` (their serving routes are among the candidates), so seerr's
recall estimate moves from 95.0% to 93.5%.

The follow-ups (ADR 0085, "After the follow-ups" below) remove owncast's
25 links from committed bundler output, each a duplicate of a source
link: owncast 22 links, 22 correct (100.0% [85.1%, 100.0%]); langflow and
seerr are unchanged. The `-t034` data files hold this state; the owncast
table rows of the re-measurement section give the state before it.

**Current state** (after the follow-ups; measured on `extractors-58`, the
follow-up build), from the `-t034` data files. The website's screenshots
come from a later build, whose per-category counts can differ by a few
calls:

| corpus | links | correct | wrong | precision [Wilson 95%] | false-link rate [Wilson 95%] |
|---|---|---|---|---|---|
| langflow | 167 | 167 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |
| owncast | 22 | 22 | 0 | 100.0% [85.1%, 100.0%] | 0.0% [0.0%, 14.9%] |
| seerr | 174 | 174 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |
| all three, current state | 363 | 363 | 0 | 100.0% [99.0%, 100.0%] | 0.0% [0.0%, 1.0%] |

### Recall in application code

Application code is the source each repository ships as its application,
by top-level directory: langflow `src/` (its backend and frontend);
owncast `web/` (the frontend source; `static/` holds the build output
that `build/web/build.sh` writes from `web/`, plus service-worker
libraries); seerr `src/` (frontend), `server/` (backend) and `public/`
(its service worker `sw.js`). Left out are documentation (langflow
`docs/`, seerr `gen-docs/`), tooling scripts (seerr `bin/`) and committed
build output. Current state, from the `-t034` data files:

| corpus | areas | production calls with a correct link | estimated missed calls | recall estimate [conservative 95%] |
|---|---|---|---|---|
| langflow | `src/` | 160 | 12 | 93.0% (all adjudicated) |
| owncast | `web/` | 22 | 5 | 81.5% (all adjudicated) |
| seerr | `src/`, `server/`, `public/` | 172 | 12 | 93.5% (all adjudicated) |

Recall is measured over the calls Graphvein recognises as client calls
(`fetch`, axios and the other clients of [client matching](../extract/clients.md));
a call through a helper it does not recognise is in no stratum and is not
counted. This matters for owncast: its admin console calls the API through
the helpers `fetchData` and `fetchText` of `web/utils/apis.ts`, 57 call
sites at the pin (56 `fetchData`, 1 `fetchText`; 53 of them under
`web/pages/admin/` and `web/components/admin/`; the helper file and its
tests not counted), and none of them is a client call. owncast's figure is
therefore an upper bound for its application code. Only the helper's own
`fetch` calls are counted, as correctly unlinked (generic helper).

Over all production calls, recall is 27.8% (langflow), 81.5% (owncast)
and 93.5% (seerr) (table "After the follow-ups" below). The gap is
langflow's `docs/` folder: 409 unlinked calls in documentation examples,
nearly all of which call the Langflow API through `fetch` with the
method in a separate options object and the base URL from
`process.env`, a form Graphvein does not resolve yet. The same estimator
prints this table, restricted to the calls under the named directories.
A sampled stratum that mixes them would be refused rather than split;
here every stratum lies wholly inside or outside them, or was
adjudicated in full:

```bash
python3 scripts/linkprecision/link_precision.py stats \
  docs/evidence/data/link-precision-{langflow,owncast,seerr}-t034.jsonl \
  --areas langflow=src --areas owncast=web --areas seerr=src,server,public
```

The 22 false links of the extractors-57 measurement fall into three classes:

1. **A literal external origin linked to a catch-all route (18 links, every
   link at confidence 0.7).** An absolute URL to another host, for example
   `https://api.github.com/repos/owncast/owncast/releases/latest`, matches a
   repository route that accepts any path: owncast's internal transcoder
   listener (`services/transcoder/fileWriterReceiverService.go:50`, a
   `net/http` `/` subtree root on a loopback port) or seerr's Next.js
   catch-all (`server/index.ts:270`, `server.get('*path', ...)`). The request
   goes to GitHub, jsDelivr, owncast.online, raw.githubusercontent.com or
   plex.tv, never to the repository. In both corpora every link of the
   `exact-origin-0.7` tier is false.
2. **A path parameter covering literal routes linked to a catch-all (3 links,
   seerr, 0.9).** `axios.get(`/api/v1/genres/${type}`)` with `type` being
   `'movie' | 'tv'` is served by the literal routes `GET /api/v1/genres/movie`
   and `/genres/tv`. A call parameter never matches a literal route segment,
   so the only remaining candidate was the catch-all, which the API router
   mounted before it shadows at runtime. `/api/v1/${type}/${tmdbId}` is the
   same case.
3. **A wrapper call bound to the wrong function in minified code (1 link,
   owncast, 0.9).** In a committed Next.js build chunk
   (`static/web/_next/static/chunks/362m0p_addvwa.js:9`), the call
   `f(window.innerWidth>=768)` calls a React `useState` setter. It was linked
   as `DELETE /api/admin/federation/servers/{id}`, because another
   function-local binding named `f` in the same file is a DELETE wrapper.

Without class 1, precision on the `exact-0.9` tier is 388 of 392 links
(99.0%).

## Corpora and pins

| corpus | repository | commit | why it is messy |
|---|---|---|---|
| langflow | `langflow-ai/langflow` | `504c02fc47e76087b82b0e7cbe4186e9cdd916d4` | React frontend and FastAPI monorepo: nested `APIRouter` prefixes (`/api` > `/v1` or `/v2` > module), routers mounted conditionally on feature flags, a shared axios instance, URLs built through a `getURL()` key table and helpers, hundreds of documentation examples |
| owncast | `owncast/owncast` | `b2c4e69553f87884c856a1a71147fe8138d21cc3` | Go chi server with oapi-codegen generated routes mounted under `/api/`, separate internal `net/http` listeners, a Next.js frontend, and the minified build output of that frontend committed under `static/web/` |
| seerr | `seerr-team/seerr` | `8331f36c919cf4af79ece4d308434fd8d7002390` | Express server with routers nested three levels deep, a router mounted under two prefixes (`/blocklist`, `/blacklist`), parameter routes beside literal ones, a Next.js catch-all after the API router, and many calls to external services |

Seerr is the third corpus because it has links in quantity (184) and a
different backend framework (Express) from the other two. A Tauri corpus
such as kftray was considered and left out: its links are IPC commands
(`invokes_command`), not HTTP `calls_route` links.

## Method

1. **Index.** Each corpus was indexed at its pin with Graphvein's own
   indexer, extractor version `extractors-57`, built from Graphvein commit
   `868e42a8bbe92bd885f6c105877f36417193b907` (its product code is identical
   to the main branch head at measurement time). For each corpus the command
   was `GRAPHVEIN_SQLITE_PATH=<scratch>/<corpus>.db graphvein index
   --repository <corpus> .`, run from the corpus root with no
   `graphvein.yaml` (so no `clients.base_paths`). Corpus code was read and
   never executed.
2. **Enumerate links.** Every stored `calls_route` edge was exported with
   `scripts/linkprecision/link_precision.py export`, together with its tier,
   its confidence and the path alternative that links. Tier names follow the
   [client matching](../extract/clients.md) confidences: `exact-0.9` (exact,
   no origin), `exact-origin-0.7` (exact, literal or assignment-derived
   origin) and `suffix-0.6` (suffix match, unresolved base). No corpus had
   more than 400 links, so **every link was adjudicated**, with no sampling.
   No corpus produced a `suffix-0.6` link.
3. **Adjudicate each link.** Links were adjudicated by an AI coding agent reading the source at each site; every verdict's evidence is recorded in the data files, so anyone can re-check it.
   The reading covers the call site (the method and the URL the request
   sends at runtime, including base URL, instance configuration, interceptors,
   wrappers and helper functions) and the route (its method and full path,
   with every router prefix and mount on the way, registration order where
   a parameter route could shadow a literal one, and trailing-slash forms).
   For minified build chunks the call site was read at the stored byte span.
   Verdicts:
   - `correct`: the request the call sends is served by the linked route;
   - `wrong` (false link): it is not, because the request goes to another
     host, another route serves it, or the site sends no such request;
   - `unverifiable`: the source does not determine the answer (for example
     a base URL from runtime configuration). No link needed this verdict.

   A link is judged on the request it sends. A route that is mounted only
   under a feature flag, or a test whose browser mock answers the request,
   still counts as `correct`, with a note. A FastAPI trailing-slash
   redirect would also have counted as served, but no link needed it.
4. **Recall side.** Production client calls without a link (status
   `dynamic`, `unmatched` or `ambiguous`; test-scope calls left out) were
   exported with `link_precision.py unlinked` and stratified by their
   top-level directory. Each sample was drawn with `link_precision.py sample
   --seed 32` (at most 100 calls per corpus, small strata in full):
   - langflow: all 19 `src/` calls and 81 of 409 `docs/` calls;
   - owncast: all 9 `web/` calls and 91 of 110 `static/` calls;
   - seerr: all 34 calls.

   Each sampled call is either `correctly_unlinked` (external API, generic
   helper whose URL is a parameter, arbitrary request, or no such route in
   the repository) or `missed` (a route in the repository serves the
   request). Recall is estimated as calls with a correct link divided by
   (those calls + estimated missed calls). The estimated missed calls are
   each stratum's population times its sampled missed share. The interval
   plugs in each sampled stratum's Wilson bounds, so it is conservative.
   A stratum adjudicated in full adds no sampling error.
5. **Report.** `link_precision.py stats
   docs/evidence/data/link-precision-{langflow,owncast,seerr}.jsonl` prints
   every table of this measurement from the data files. Precision is correct / (correct +
   wrong). The false-link rate and the unverifiable share are over all
   links. Intervals are Wilson 95% intervals.
   The current state's per-corpus rows are printed by
   `link_precision.py stats docs/evidence/data/link-precision-{langflow,owncast,seerr}-t034.jsonl`;
   its all-three row pools their links (363 correct of 363) and takes
   both intervals from the same script's Wilson function:
   `python3 -c 'import sys; sys.path.insert(0, "scripts/linkprecision"); import link_precision as lp; print(lp.interval(363, 363), lp.interval(0, 363))'`.

## Results per tier

| corpus | tier | links | correct | wrong | unverifiable | precision | false-link rate | unverifiable share |
|---|---|---|---|---|---|---|---|---|
| langflow | exact-0.9 | 167 | 167 | 0 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] | 0.0% [0.0%, 2.2%] |
| owncast | exact-0.9 | 48 | 47 | 1 | 0 | 97.9% [89.1%, 99.6%] | 2.1% [0.4%, 10.9%] | 0.0% [0.0%, 7.4%] |
| owncast | exact-origin-0.7 | 11 | 0 | 11 | 0 | 0.0% [0.0%, 25.9%] | 100.0% [74.1%, 100.0%] | 0.0% [0.0%, 25.9%] |
| seerr | exact-0.9 | 177 | 174 | 3 | 0 | 98.3% [95.1%, 99.4%] | 1.7% [0.6%, 4.9%] | 0.0% [0.0%, 2.1%] |
| seerr | exact-origin-0.7 | 7 | 0 | 7 | 0 | 0.0% [0.0%, 35.4%] | 100.0% [64.6%, 100.0%] | 0.0% [0.0%, 35.4%] |

Pooled over the three corpora: `exact-0.9` 388 of 392 correct (99.0%
[97.4%, 99.6%]), `exact-origin-0.7` 0 of 18 (0.0% [0.0%, 17.6%]).

## Recall

Recall counts only the calls Graphvein recognises as client calls; see
"Recall in application code" above for owncast's helper calls, which are
not counted.

| corpus | stratum | unlinked calls | sampled | correctly unlinked | missed | missed share |
|---|---|---|---|---|---|---|
| langflow | docs | 409 | 81 | 1 | 80 | 98.8% [93.3%, 99.8%] |
| langflow | src | 19 | 19 | 7 | 12 | 63.2% (all adjudicated) |
| owncast | static | 110 | 91 | 64 | 27 | 29.7% [21.3%, 39.7%] |
| owncast | web | 9 | 9 | 4 | 5 | 55.6% (all adjudicated) |
| seerr | bin | 3 | 3 | 3 | 0 | 0.0% (all adjudicated) |
| seerr | public | 1 | 1 | 1 | 0 | 0.0% (all adjudicated) |
| seerr | server | 18 | 18 | 18 | 0 | 0.0% (all adjudicated) |
| seerr | src | 12 | 12 | 3 | 9 | 75.0% (all adjudicated) |

| corpus | production calls with a correct link | estimated missed calls | recall estimate [conservative 95%] |
|---|---|---|---|
| langflow | 160 | 416 | 27.8% [27.6%, 28.9%] |
| langflow, application code only (`src/`) | 160 | 12 | 93.0% |
| owncast | 47 | 38 | 55.5% [49.1%, 62.3%] |
| seerr | 172 | 9 | 95.0% |

The causes of the missed calls repeat across corpora:

- `fetch(url, options)` with the method in a separate `options` object
  (the method stays dynamic). This covers nearly all of langflow's
  documentation examples, which call the Langflow API with a base from
  `process.env`;
- URLs built with `let` and `+=`, query helpers such as `addQueryParams`,
  or `new URL(path, window.location...)` followed by `searchParams`;
- a call parameter that takes literal values (`/settings/${type}/test`
  with `type` either `radarr` or `sonarr`), which never matches literal
  routes;
- server-side calls to the repository's own server through a dynamic
  authority (`http://${getHostAndPort()}/api/v1/...`);
- in minified build output, multi-declarator `let` bindings and wrapper
  calls through them;
- routes that Graphvein does not extract, such as a FastAPI router created
  inside a factory function (langflow's `POST /api/v2/workflows`).

## Unknowns shown

Stored `client_call` statuses per corpus. Calls that are not `linked` are
shown as unresolved, not hidden.

| corpus | client calls | linked | dynamic | unmatched | ambiguous | wrapper | test_only |
|---|---|---|---|---|---|---|---|
| langflow | 594 | 165 | 423 | 6 | 0 | 0 | 0 |
| owncast | 200 | 59 | 120 | 0 | 1 | 20 | 0 |
| seerr | 220 | 182 | 20 | 15 | 0 | 3 | 0 |

## Data files

One JSON object per line, sorted keys:

- `record: "summary"`: the stored status counts, what was adjudicated and
  the sampling rule;
- `record: "stratum"`: the number of unlinked production calls per
  recall stratum;
- `record: "link"`: one per stored `calls_route` edge: `call_site` and
  `route_site` (file:line), `url_expression` (as stored), `url_built` (how
  the request URL is built, at most 120 characters), `linked_template`,
  `origin`, `wrapper`, `tier`, `confidence`, `route_method`, `route_paths`,
  `verdict` and `note`;
- `record: "unlinked"`: one per sampled unlinked call, with `stratum`,
  `status`, `templates`, `verdict` and `note`.

Files of the extractors-57 measurement: [langflow](data/link-precision-langflow.jsonl),
[owncast](data/link-precision-owncast.jsonl),
[seerr](data/link-precision-seerr.jsonl).

## Re-measurement after the fix

The three classes above became the class rules LF-1 to LF-3 of
[client matching](../extract/clients.md#link-precision-rules-t034): a call
with an origin nothing proves to be the server never takes a route without
a literal segment (LF-1); a final catch-all is only one candidate when a
call parameter stands where other routes have literal segments (LF-2); a
destructured binding shadows a wrapper's name, and a call is an invocation
of a wrapper only through the wrapper's own lexical binding (LF-3).

Method. The same corpora at the same pins were indexed the same way
(no `graphvein.yaml`) with extractor version `extractors-58`, built from
Graphvein commit `6ae2972c6aedfee0c8b9dca2c5e27770a606fcc1`. Every stored `calls_route` edge was exported
again. A link whose call, route and tier are unchanged keeps its verdict
from the first measurement (field `adjudication` says so); a new or
changed link would have been adjudicated afresh, but there was none: all
388 links of the re-measurement are links of the first one. The 22 links
the fix removed are the 22 false links. Calls that lost their link and are
still client calls form a new recall stratum per corpus, `T034 unlinked`,
adjudicated in full (owncast 11, seerr 10). The minified call of class 3
is no client call any more (no wrapper invocation), so it is in no stratum.
The other recall strata and their samples are unchanged: no sampled
unlinked call became linked. `link_precision.py stats
docs/evidence/data/link-precision-*-t034.jsonl` prints these tables.

| corpus | tier | links | correct | wrong | unverifiable | precision | false-link rate |
|---|---|---|---|---|---|---|---|
| langflow | exact-0.9 | 167 | 167 | 0 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |
| owncast | exact-0.9 | 47 | 47 | 0 | 0 | 100.0% [92.4%, 100.0%] | 0.0% [0.0%, 7.6%] |
| seerr | exact-0.9 | 174 | 174 | 0 | 0 | 100.0% [97.8%, 100.0%] | 0.0% [0.0%, 2.2%] |

No corpus has an `exact-origin-0.7` link any more: in all three, every
link of that tier was class 1. Literal-origin links to routes with a
literal segment are kept; none occurs in these corpora.

| corpus | stratum | unlinked calls | correctly unlinked | missed |
|---|---|---|---|---|
| owncast | T034 unlinked | 11 | 11 (GitHub, jsDelivr, owncast.online) | 0 |
| seerr | T034 unlinked | 10 | 7 (GitHub, raw.githubusercontent.com, plex.tv) | 3 (class 2: now `ambiguous`, served by literal routes among the candidates) |

| corpus | production calls with a correct link | estimated missed calls | recall estimate [conservative 95%] |
|---|---|---|---|
| langflow | 160 | 416 | 27.8% [27.6%, 28.9%] |
| owncast | 47 | 38 | 55.5% [49.1%, 62.3%] |
| seerr | 172 | 12 | 93.5% |

| corpus | client calls | linked | dynamic | unmatched | ambiguous | wrapper | test_only |
|---|---|---|---|---|---|---|---|
| langflow | 594 | 165 | 423 | 6 | 0 | 0 | 0 |
| owncast | 199 | 47 | 120 | 11 | 1 | 20 | 0 |
| seerr | 220 | 172 | 20 | 22 | 3 | 3 | 0 |

Committed build output. owncast is the only one of the three corpora (and
of the 34 corpora Graphvein is tested on) with links whose call site is
committed minified build output (`static/web/_next/static/chunks/`). In
the first measurement 35 of its 59 links came from 29 such chunks; 32 of
them repeat a link of the source tree (same method, route and template up
to parameter names), and the 3 that did not were false (class 1 and 3).
After the fix 25 of its 47 links come from 21 chunks, and every one
repeats a source link: the build output adds no correct link, only
duplicates, and was the site of 11 of owncast's 12 false links.

### After the follow-ups (committed bundler output, ADR 0085)

The follow-ups to the fix store every framework's final rest-of-path
wildcard as `{*name}` (FastAPI's `{name:path}` included), apply LF-1 to
OpenAPI operations in the project link pass, stop a parameter's default
value from shadowing, show the assumption of a 0.7 link whose origin is
not proven, and stop linking the client calls of committed bundler output
([ADR 0085](../adr/0085-generated-bundle-output.md)). The three corpora
were indexed again the same way with the follow-up build (still
`extractors-58`). langflow and seerr are unchanged, link for link (same
ids, tier, confidence, template and route); langflow's
`/api/{_path:path}` reserve route is registered on a function parameter
`app`, which the FastAPI extractor does not resolve, so the path convertor
change does not reach it. owncast's 142 client calls in 62 chunks under
`static/web/_next/static/chunks/` now have status `generated`: its 25
links from 21 chunks are gone, and each of them duplicated a link of the
source tree to the same route. No other link changed or was added. The
owncast data file was updated to match: its links are the 22 source
links (verdicts unchanged), the summary records the new status counts and
the excluded output, and the bundle's unlinked calls left the recall
strata (they are not linked by design, so they are no misses; the
`unlinked` export skips status `generated`). The 13 unlinked production
calls that remain were all adjudicated before.

| corpus | links | correct | precision | production calls with a correct link | estimated missed | recall |
|---|---|---|---|---|---|---|
| langflow | 167 | 167 | 100.0% [97.8%, 100.0%] | 160 | 416 | 27.8% |
| owncast | 22 | 22 | 100.0% [85.1%, 100.0%] | 22 | 5 | 81.5% (all adjudicated) |
| seerr | 174 | 174 | 100.0% [97.8%, 100.0%] | 172 | 12 | 93.5% |

owncast's recall rises because the bundle's 27 sampled misses (calls a
chunk repeats from source) are no longer counted; its source calls are
linked as before. Status counts: owncast 199 client calls, 22 linked, 12
dynamic, 2 unmatched, 1 ambiguous, 20 wrapper, 142 generated.

Data files: [langflow](data/link-precision-langflow-t034.jsonl),
[owncast](data/link-precision-owncast-t034.jsonl),
[seerr](data/link-precision-seerr-t034.jsonl). Each holds the summary,
every link with its verdict and `adjudication`, the strata (the new one
included) and every adjudicated unlinked call, in the format above; a
`T034 unlinked` call also names its removed link (`previous_link`).

## Limitations

- Three corpora, about 400 links. The intervals are wide for the 0.7 tier
  and for owncast. No corpus produced a suffix-tier link, so the
  `suffix-0.6` tier is not measured here.
- One adjudicator, reading the source. No program was run, so runtime
  facts that the source does not fix (deployment proxies, environment
  overrides of base URLs) were judged from the code as committed.
- A link counts as correct when the linked route serves the request the
  call sends. Whether the handler then succeeds (authentication, feature
  flags, validation) is not judged.
- Recall counts call sites, not edges. A call with two alternatives is one
  call. The recall estimate depends on which directories hold the
  unlinked calls. langflow's total is dominated by documentation examples,
  so the recall in application code is given first ("Recall in
  application code" above).
- Owncast's committed build output repeats the same frontend modules in
  several chunks. Its links and misses are counted once per chunk, as
  stored.
