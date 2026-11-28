# Client call extraction

ADRs 0018, 0020, 0022, 0025, 0027 and 0029, extractors-49, contract v1. Syntax is cached per file;
recognition and linking run after current route composition, using the shared
TypeScript resolver. Receiver names alone never prove axios.

Orval 8 generated clients use the bounded configuration and invocation model
in ADR 0027. Scanned orval.config.ts/js/mjs/mts files must call external orval
defineConfig with a literal project map. Each modeled project requires a
major-8 package declaration, supported scanned api_spec input and approved
tags-split/react-query/fetch settings. Generated source remains authoritative.
Paths resolve from the config directory and output workspace; tags and
operation exports follow the inventoried Orval ASCII case rules.

Named/aliased imports and namespace calls must resolve first to the predicted
file without choosing scanned source. Direct requests and generated hooks count;
options calls count only as the immediate first argument of proven TanStack
useQuery/useSuspenseQuery or QueryClient fetchQuery/ensureQueryData/prefetchQuery.
Const new QueryClient bindings and function-local const useQueryClient bindings
use the shared resolver, including package shadowing. Keys, URL functions,
assigned/spread options, barrels and require supply no invocation facts.
Literal query.queryFn overrides emit generated_client_override and no node.
Wrong-file exports emit an unmatched node with wrapper null and
generated_client_mismatch; they make no transport claim.

Orval nodes carry library, invocation, operation, generated_file and
generated_export in addition to existing client attributes. Each proven
operation receives one inferred calls_operation edge at confidence 0.9.
Paths retain resolution generated. Without transport proof they remain dynamic;
with proof they contain the evaluated generated URL and use ordinary matching.
Path parameters retain the sanitized camel-case identifier names emitted by
Orval, for example /articles/{slug}.
Mutators require plain, unchanged URL/options parameters, exactly one global
fetch reached through const declarations and URL authority assignments, and
options forwarded directly or spread first without an own method. Route edges
carry the application call and mutator fetch spans. A used inner fetch is wrapper.
Unproven transport keeps calls_operation and emits
generated_client_transport_unproven.
Unproven projects emit info generated_client_model_unproven. An unsupported
operation within a modeled project emits info generated_client_operation_unmodeled,
naming the operation and reason, without rejecting the other operations.
Absent generated output emits generated_client_absent. Missing directories omit
the ignore remedy; excluded existing directories name the ignore source and
retain its limits.

Fetch recognizes bare unbound fetch and window/globalThis/self.fetch. The first
argument is the URL. Fetch init object entries apply in source order: an own
method replaces the previous value; a spread of a proven object contributes its
own method, while proven absence or null/undefined contributes nothing. Unknown
spreads make the method dynamic until a later own method overrides them. Missing
init/method or an undefined method means GET. Proven spread sources include
bounded module/local const objects and literal objects recursively; ordinary
direct nonliteral init arguments remain dynamic. Axios recognizes its external default export
(default import, namespace default, require), scanned workspace shadowing,
instances and one factory-return level. Verbs get/delete/head/options and
post/put/patch plus Form variants, request(config), obj(config) and obj(url,config)
use the specified arguments. Axios interceptors remain outside the supported subset.

Angular recognizes external @angular/common/http HttpClient through constructor
parameter properties, @Inject(HttpClient) constructor parameters, inject(HttpClient)
fields and function-local const declarations. inject and Inject must resolve
to @angular/core. Aliases and re-exports use the shared resolver; scanned packages
shadow external Angular exports. Receiver names, plain constructor assignments
and inherited fields do not supply proof. Verbs use the first URL argument, jsonp
is GET, request(method, url) uses the existing method rules, and request(HttpRequest)
is dynamic. Type arguments are transparent and options are ignored. Sites are
recorded even when their Observable is never subscribed.

Production provideHttpClient(withInterceptors([...])) registrations and class
HTTP_INTERCEPTORS multi providers in an injector with HttpClientModule or
provideHttpClient(withInterceptorsFromDi()) supply interceptor evidence.
Test registrations and DI providers without that enabling feature are ignored.
One unconditional URL-touching interceptor can supply a base. Recognize
req.clone({url: prefix + req.url}) and equivalent templates, forwarded to next
or next.handle directly or through one local const. Evaluate prefix with the
unchanged bounded evaluator, including imported environment const members.
Other URL clones, HttpRequest construction and opaque transformed requests
make every affected alternative dynamic with null template/origin and unresolved
base; they cannot produce a calls_route edge. Header/error-only interceptors do
not contribute a base.

The deepest angular.json project sourceRoot (root fallback), relative to its
configuration, owns registrations and calls. Missing/malformed metadata leaves
prefix-shaped bases unresolved; malformed JSON emits angular_config_parse_error.
Multiple or conditional registrations, or an unevaluable prefix expression,
also leave bases unresolved and suffix-eligible for clients.base_paths, but only
when every applicable URL-touching registration has the proven prefix shape.
An unowned call considers every production URL-touching registration in the
repository; any opaque one makes its alternatives dynamic. Configuration cannot
repair opaque rewrites. With no touching interceptor, URLs stand as written.
Prefixes concatenate, so prefixed absolute URLs are dynamic. Static environment
imports are followed; fileReplacements remain inventory evidence.

Wrappers have emitted callable identities and forward a URL parameter last
after literal/const prefixes. Fetch also permits one leading environment-shaped
base under the direct-call rule below, through transparent assertions. A
wrapper's leading base spelling is retained from its fetch site. New environment
wrapper chains forward whole URL parameters; resource helpers composing an ID
into a request argument retain their ordinary request edge.
Methods use literal/direct-parameter/options.method with supported
fallback/default, or fetch init spreads of the options parameter and proven
objects. Each invocation binds literal options objects (including nested spreads);
missing/unshadowed undefined arguments bind undefined when no default is
declared. A default proven to be an object, null or undefined binds that value;
any unproven declared default stays unknown. Spreading an unknown default makes
the method dynamic until a later own method overrides it. This distinction also
applies through nested wrappers and options.method reads. Defaults are evaluated
in the scope enclosing the function, with parameter references unknown and body
or inner-block declarations invisible, using existing cached URL scopes. Other
supplied options arguments stay unknown even when they are
const identifiers. Existing options.method forwarding also reads literal options
spreads. Follow existing resolved callable sites for
three levels; repeated groups and cycles supply no speculative calls. Inner
forwarding sites have status wrapper. Wrapper-derived nodes use their actual
call-site URL expression and identify the outermost used wrapper.

Evaluation follows module const initializers through the shared resolver with
cycle guards. A local const must have one identifier declarator and precede its
lexically visible use unless the use is inside a nested function. Call-site
let/var stay dynamic; undefined is recognized unless shadowed. Parentheses,
as/satisfies/as const, non-null and angle-bracket assertions are transparent.
Values include strings, integers, booleans, null, undefined, plain objects,
dynamic pieces and unknowns. Templates and string + use ECMAScript ToString;
|| uses ToBoolean and ?? falls through only for null/undefined. Unknown left
operands stay dynamic, except existing environment-base shapes below. Known
ternaries fold; unknown conditions retain alternatives.

Module const tables have identifier/string/numeric keys and own-property dot or
evaluated bracket access. Spreads, computed keys, accessors, methods, member
assignment/update/delete or Object.assign/defineProperty/defineProperties/
setPrototypeOf mutation disqualify a table. Object-literal arguments may contain
dynamic values; nonliteral object arguments used in loops remain dynamic.
Property order uses canonical array indices below 2^32-1, then inserted strings.

Helpers are uniquely emitted function declarations or const-bound arrows/function
expressions, excluding async/generator/method/repeated groups. Plain identifier
parameters use defaults when missing or undefined. Supported statements are
const/let declarations, local let =/+=, folded if/else, return, for-of over
Object.keys/values and for-in over known objects. Other statements reject a
helper. Exactly one return must be reached. Evaluation has a three-helper depth
limit and 2,000 steps per site; recursion is dynamic. Memoization lasts one link
pass. No repository code is executed.

URL objects admit a single-identifier const initialized with unshadowed
new URL(input[, base]), at module or function scope. Only absolute http(s)
inputs or absolute paths are modeled. Absolute inputs ignore base. Absolute
paths require a literal base origin or a subsequent host assignment; hostname
or port alone cannot establish an unknown host. Unconditional assignments to
protocol, host, hostname or port preserve the path and yield origin null with
origin_resolution unresolved. Other prior references, escapes, property writes,
searchParams and conditional/loop assignments make the use dynamic. Serialize
with .toString() or .href directly or through one local const. Relative/network
paths, literal dot/encoded-dot segments, backslashes, whitespace, non-ASCII and
invalid percent escapes remain dynamic. Literal origins retain existing behavior.
Assignment-derived origins use exact confidence 0.7 and never suffix matching.

For each alternative, URL pieces include strings, templates and concatenation.
Absolute and protocol-relative URLs with a dynamic authority piece have a null
template, null origin and unresolved base, for all three libraries.
Split a leading literal origin; truncate
query/fragment text. Dynamic pieces form parameter segments named after an
identifier/final member property, or empty braces. Shared literal/dynamic
segments become parameters. Only leading import.meta.env/process.env members,
those members with literal ??/|| fallback, or one-level module const bindings
to those shapes, or a build-time constant (row BC-1 below), may be
unresolved prefixes before a slash. Other leading
dynamic expressions are dynamic with null templates. Collapse slashes, ensure
a leading slash and remove trailing slash except root; preserve punctuation,
case, percent escapes and dot segments. Client colons are literal.

Axios bases prefer per-call baseURL, create baseURL, unconditional module-level
defaults.baseURL, then inherited default-object base. Distinct/default dynamic
assignments are unresolved. Absolute URL origins ignore baseURL. Join bases
using axios's slash trimming. Relative page URLs and unresolved axios bases
remain unresolved; clients.base_paths supplies the longest containing directory
only in that case. Configuration cannot repair opaque resource prefixes.

hey-api client-axios requires a scanned openapi-ts.config default call to external
@hey-api/openapi-ts defineConfig, an evaluated output string/output.path and a
literal client-axios plugin. Its scanned client.gen.ts must export const client
from createClient imported from a scanned module under that output. Comments and
receiver names never prove transport; client-fetch remains unproven. Relevant
config fields use the bounded evaluator; callback settings are not executed.

Proven-client connect/delete/get/head/options/patch/post/put/trace requests inside
functions, methods and arrows produce library hey-api. An immediate parameter's
client ?? provenClient fallback (including optional access) is supported. An
object argument needs exactly one own url evaluating to one string. Earlier
spreads cannot replace it; later immediate-parameter spreads use the inventoried
Options URL exclusion. Other later spreads stay dynamic. Only whole {name}
segments are parameters; other brace forms, unsupported requests and own client
overrides stay dynamic. The options object's `security` array is the
client-call attribute `security` as written ([auth extraction](auth.md),
row DA-H1, T026 AG-B4a).

An own per-call baseURL wins. Otherwise setConfig merges with the initializer:
one unconditional production module-level plain-object baseURL call replaces
the initial base; objects without baseURL preserve it. Multiple base-setting
calls, conditional/test/function calls, opaque/spread arguments and other config
mutations leave it unresolved. Existing base configuration applies then. getUrl
prepends a slash to relative URL text and concatenates base plus path; absolute
URL spellings are paths too, rather than bypassing the base as in axios.

App invocations are not client_call nodes. Info generated_client_override names
literal options containing client/baseURL/url and the SDK method. Nonliteral
options and module-evaluation requests before setConfig are gaps. Info
generated_client_absent distinguishes missing directories from excluded existing
directories. Only excluded output receives its ignore-source name and
.graphveinignore negation remedy (visible parents required; built-in ignores
cannot be negated). Info generated_client_unproven identifies generated-header files outside
proven output directories. See ADR 0025.

Attributes: library; method (uppercase/null); method_expression when dynamic;
url_expression; paths (sorted alternatives);
base (none/literal/constant/configured/unresolved); base_expression when unresolved
(the compact leading source spelling at the request site, or wrapper fetch site,
or a URL object's origin base, see "Build-time constant bases";
all-null paths omit it and configured bases clear it; whole-URL const aliases
may retain an empty string when the request site has no separate base expression);
status (linked/ambiguous/unmatched/dynamic/test_only/wrapper); wrapper (outermost callable
identity/null); helpers (sorted evaluated identities, capped at ten, when used).
Each path has template (normalized/null), origin (literal/null), conditional,
resolution (literal/constant/evaluated/generated), status, match (exact/suffix/null),
candidates (at most ten sorted identities) and candidate_count (full ambiguous
ties, otherwise zero). On a call with more than one path or a conditional one,
each path with a template also has base (the base kind the linker used for
that alternative, which can differ from the call-level base) and, when that
base is unresolved, base_expression (the alternative's own expression; omitted
when values merged into the alternative disagree on it). extractors-47,
additive; the project link pass (ADR 0075) matches each alternative from them. Linked candidates name the target. Include trailing_slash
only when the query-truncated raw path ends in / and normalizes to a non-root
template. Deduplicate by template and slash flag, then sort template (null last)
and flag. More than four entries is dynamic. Node status is linked when every
entry links, otherwise dynamic > unmatched > test_only > ambiguous; inner sites stay wrapper.
All-null templates keep base unresolved even with configuration. Identities
exclude derived data.

Methods match equal, ANY, or client HEAD via a GET route of Express, Axum or
Go net/http (T023 decision 8; FastAPI, chi and gin require
HEAD/ANY). Route parameters match literal/parameter calls; call parameters
match only route parameters. Final route wildcard consumes one or more
segments; optional final parameters also permit omission. Empty route paths
never match. Exact matches cover the whole path. Suffix requires unresolved
base, at least one literal call segment, and a route remainder with at least
one literal segment matching a literal call segment. Remainders of parameters
or a catch-all alone never match. A remainder's first segment must be a
literal, or a parameter matched by a call parameter. With an unresolved base
(whatever the origin), an exact match also needs at least one literal route
segment, so literal-free routes (`/{id}`, `/{*}`, `/`) are never candidates,
and the same anchor: a route whose first segment is a parameter is a
candidate only when the call's first segment is a parameter (`/{org}/settings`
does not match `${API}/account/settings`). Such a rejected exact match may
still match as a suffix. So for an unresolved base, every accepted candidate
matches at least one literal call segment with a literal route segment, and
no leading call literal fills a route parameter. Specificity prefers more literal
segment matches, then exact verb over ANY/implicit HEAD. Distinct tied routes
are ambiguous, except equal-template ties of one framework whose router serves
the two slash forms as two routes (FastAPI, ADR 0019; Go `net/http`, `chi` and
`gin`, ADR 0076: the other form gets a redirect or a 404, never the handler)
with mixed slash flags: keep those matching the call flag. Never apply this to
a sole candidate, Express, Axum, Next.js or mixed frameworks. Before ranking,
a `net/http`, `chi` or `gin` path entry is a candidate only for a call of its
own slash form (TOP-2 of 2026-11-18): those routers never hand the other form to
the route (chi answers 404; gin's default `RedirectTrailingSlash` and a
`net/http` subtree root named without its slash answer a redirect the client
has to follow; a redirect is not a link), so such a call is unmatched unless
the route also claims that form (a chi `/` route's bare-prefix entry, row
GR-C1). A final catch-all (`{*}`, `{*name}`) and the root have no other form.
A FastAPI sole candidate of the other form still links (ADR 0019). One winner per alternative emits calls_route with call,
selected table-property and used helper-return evidence and inferred
confidence 0.9 exact/no origin, 0.7 exact/origin, 0.6 suffix.
Alternatives reaching one target emit one edge at the strongest tier.

OpenAPI operation correspondence reuses these selection rules through an
exact-only RouteMatcher adapter; see [OpenAPI route correspondence](openapi.md#route-correspondence).
The client matcher and its output remain unchanged.

After existing scope filtering, equivalent Express registrations select the first
source registration at the existing confidence. All tied candidates must share a
proven app/router object in one file, method, raw pattern, resolved non-inline
handler, middleware and matching path template/scope. ANY/verb mixtures,
different handlers/middleware/routing objects and other frameworks retain ambiguity.
See route_duplicate_registration in [routes](routes.md).

Client nodes carry scope: test when the actual call site is in a test-scope file,
including wrapper invocation sites. Production calls apply scope after the
existing top-tier and slash filtering: mixed tiers keep only production paths;
entirely test-only tiers give test_only with capped candidates, full count for
two or more (zero for one), and no edge or weaker-tier fallback, unless a
production route below an unknown prefix may serve the call (T057 precision
correction; see [test scope](test-scope.md)): then the alternative is
unmatched. Test calls
ignore scope entirely, preserving their existing ambiguity and links. See
[test scope](test-scope.md). Scope and config edits relink unchanged cached syntax.

Known gaps: fetch uppercases all verbs rather than its standard six; slash-bounded
dynamics might contain slashes; Express matching is case-sensitive here; axios
creation order and interceptors are unmodeled. URL objects outside the subset
above, Request objects,
XMLHttpRequest, WebSocket, EventSource or other generated clients. Helper evaluation
is restricted to the statement subset and resource bounds above.

## Link precision rules (T034)

extractors-58. Three classes of false `calls_route` links measured in the
[link precision evaluation](../evidence/link-precision.md) are closed by
class rules. LF-1 and LF-2 are link-time rules of the shared matcher, so
they hold in the project link pass too (`NewStoredRouteMatcher` replays
them; `NewProjectRouteMatcher` keeps them, except as LF-1 says). LF-3
changes the cached TypeScript call syntax (the `Shadowed` flag of a call)
and adds a link-time check of wrapper invocations.

| Row | Form | Claim |
|---|---|---|
| LF-1 | a call alternative with an origin nothing proves to be the route's server (a literal origin, or an assignment-derived one, `origin_resolution` unresolved), or an OpenAPI operation path entry with literal origins (its spec's `servers`) that nothing proves to be the route's server, and a route path with no literal segment (`/{id}`, a catch-all `/{*}` or `/{*name}`, the root `/`) | not a candidate: the request may go to any host, and no literal segment says it is meant for this route. Proof of a call's origin is a `net/http` host-pattern route that names the call's host, or, in the project link pass, a `consumes` entry of another member whose `origins` names the literal origin (PL-R5, PL-L4); an assignment-derived origin is never proven. An operation's origins are proven only, in the project link pass, by a `consumes` entry whose `origins` names one of them; a spec in the repository that stores the routes is no proof (it may document another service, as a committed spec of a public API does), so the rule holds in the in-repository correspondence too |
| LF-2 | the sole winning route of an alternative ends in a final catch-all, and a call parameter stands where another eligible route (same method, ANY, or GET for HEAD where that rule applies; same filters of scope, host, slash form and Next.js rules) has a literal segment, the rest of that route matching | `ambiguous`; the candidates are the catch-all and every such route (test-scope routes only for a test call): the parameter may hold the literal, and routers try or register the more specific route first |
| LF-3 | a TypeScript or JavaScript call through a plain identifier, where the identifier is bound by a destructuring pattern (array or object pattern, shorthand property, `key: name` pair value, default left side, rest element) of a declaration or parameter, or where its lexical binding at the call site is not the binding that declares the resolved wrapper (another binding of the same name, or none because the wrapper is declared inside another function) | the call is not an invocation of the wrapper: no wrapper-derived client call; a destructured or shorthand-parameter binding also shadows the name for `calls` edges |
| LF-U | anything else: a call or operation without an origin, a route with a literal segment, a winner that is no catch-all, a parameter where no literal route overlaps, a member call, a call from another file, a call whose lexical binding is the wrapper's own | the rules above this section, unchanged |

LF-1 extends the unresolved-base rule (T017b Rule E) to calls with an
origin. A literal-origin call still links a route that has a literal
segment, at 0.7; such a link, and every other 0.7 link whose origin no
proof above covers (an assignment-derived origin included), carries a
shown assumption: the path entry stores `origin_assumption` and the call
`origin_assumptions` ("origin https://api.example.com assumed to be this
repository's server", or "origin (not resolved statically) ..." for an
assignment-derived origin). The local server shows it as the reason
`origin_assumed` of a linked caller; a project link appends the same text
with the provider's name ("... assumed to be backend's server") to its
stored assumption, for a call's unresolved origin and for an operation
whose origins no `consumes` entry names. A same-member link scope that a
`consumes` entry names is matched with the member's own matcher, where
LF-1 holds (no waiver): a possible miss, never a false link. LF-2 never changes a winner that is not a catch-all: a
parameter route beside literal siblings (`/items/{id}` beside
`/items/featured`) keeps its link. Fixture:
`testdata/fixtures/link-precision-fl`, test `TestLinkPrecisionRules`.
For `calls` edges a destructured name shadows by the call binding model
of `docs/lang/typescript-resolution.md` (row TSX-4): in its own scope,
blocks, loops and anonymous functions included (since T046; before, a
pattern in a nested block or an anonymous function shadowed nothing).
The wrapper check of LF-3 reads the full lexical scopes of
the routes syntax, blocks and anonymous functions included. A parameter
binds its pattern only, by the rule of declarations: identifiers of
patterns, the left side of a default, the value of a `key: value` pair and
rest elements; a default value (`function f(cb = del)`,
`({ del: d = del })`) and a computed key are read in the enclosing scope
and shadow nothing, for `calls` edges and for the routes syntax scopes
alike (T034 review). Every branch reports its row (a tag-to-row test
keeps code and table 1:1); fixtures `testdata/fixtures/link-precision-fl`
and `testdata/fixtures/link-precision-catchall` (an api.github.com spec beside `GET *path` in the first; Next.js `[...rest]` and
`[[...slug]]`, chi `*`, Axum `{*rest}`, FastAPI `{name:path}`). Known gaps: `catch` clause
parameters and `for ... of` / `for ... in` heads are not bindings of the
`calls` shadow model; LF-2 does not look at
registration order, so a catch-all registered before the literal routes is
ambiguous too; whether a literal origin is the repository's own deployment
is not known inside one repository, so no in-repository literal origin is
proof.

## Unknown-prefix tier (T033, ADR 0084)

A route whose prefix the code does not fix (a configured chi `Route` or
`Mount` prefix, a `StripPrefix` of a configured value, a router behind an
unproven hop) has no `paths` entry; its `unresolved_prefix_paths` entries
([routes](routes.md), "Unknown-prefix paths") hold the path proven below
the unknown part. The client matcher reads them in a fallback tier, under
the Draft claim contract
[unknown-prefix-paths](../contract/claims/unknown-prefix-paths.md) (rows
UP-13..UP-22):

- Only an alternative the tiers above leave without any candidate is
  matched against the entries, so every existing edge, ambiguity and
  test-only outcome stays as it was (UP-13), with one exception: when every
  candidate of the tiers above is a final catch-all (an SPA or root
  fallback) and an entry matches it with a literal segment (UP-17's
  competitor), the alternative is
  `ambiguous` with the catch-alls and those routes as candidates, as LF-2
  makes a catch-all ambiguous beside a more specific route (UP-22); it is never linked to either.
  The test-scope correction of T057 reads the entries the same way for an
  alternative whose existing tiers give only test-scope candidates: a
  production entry matching it with a literal segment makes it
  `unmatched` instead of `test_only` (no entry becomes a candidate, no
  edge is added; [test scope](test-scope.md)).
- A call whose path is known from its start (base `none`, `literal`,
  `constant` or `configured`) matches an entry when its segments are
  `above` + V + `template`, V a run of zero or more literal call segments
  standing for the unknown prefix (UP-14); `/v1/api/orders/42` matches the
  entry `/api/orders/{id}` with V = `/v1`. A call with an unresolved base
  matches only an entry without `above` whose template it matches
  entirely: its unknown base is taken to be the route's unknown prefix
  (UP-15, assumption UP-V2).
- A candidate needs at least two literal route segments matched by literal
  call segments and an anchored template (its first segment a literal, or
  a parameter taken by a call parameter) (UP-16); `/api/orders/{id}` has
  one literal segment only and never becomes a candidate.
- Candidates are never ranked: when a second route matches the call with
  at least one literal segment (whether or not it meets UP-16), the call
  is `ambiguous` (UP-17). Each candidate implies a different unknown
  prefix, so the literal count is no evidence between them.
- Method, HEAD to GET, strict-slash and scope rules are the tiers' own
  (UP-18). A call with a literal origin or an assignment-derived origin is
  never matched, nor is a Next.js route (UP-20), with one exception in the
  project link pass: a literal origin that a covering `consumes` entry
  names for a provider of another member (UP-21).
- A link is one `calls_route` edge, inferred, confidence 0.6; the path
  entry records `match: "suffix"` and `route_prefix: "unresolved"` and,
  like the call, `assumes: ["UP-V1"]` (`["UP-V1", "UP-V2"]` for an
  unresolved base) (UP-19). The local server shows the assumption text as
  the reason `prefix_assumed` of a linked caller ([serve](../cli/serve.md)).
- The project link pass links on these entries only through a
  declaration: a call of another repository whose only candidates are
  unknown-prefix entries is `unproven` (row UP-25, PL-R13 of
  [project links](project-links.md)) unless a `consumes` entry of its
  consumer that covers it names the provider (and, for a literal origin,
  the origin); then the provider's result counts as any declared
  provider's (row UP-26, PL-L7: one route links at 0.6 with `assumes`
  UP-V1, UP-V3 and, for an unresolved base, UP-V2; a tie or a second
  possible provider is `ambiguous`).

OpenAPI operation correspondence uses the exact tier only and never this
tier. On the pinned corpora the tier links dozzle's frontend only: its
calls through the runtime base helper `withBase` (rows UP-39 and UP-41,
"Unknown-prefix call side" below) give 24 links, all correct by the
source, one ambiguous call (`.../actions/update` beside
`.../actions/{action}`, UP-17) and one refusal (`/healthcheck`, UP-16);
no other corpus link changes. Fixture:
`testdata/fixtures/unknown-prefix`, test `TestUnknownPrefixLinks`.

The tier's precision gate on the corpora is the opt-in test
`TestUnknownPrefixStripOracle` (`internal/extract/clients`): set
`GRAPHVEIN_UP_ORACLE_SNAPSHOTS` to a directory holding one
`<corpus>.ndjson` per corpus, each written by `graphvein index <corpus
checkout> -repository <corpus> -emit <dir>/<corpus>.ndjson`. It strips the
first one, then two segments of every known route path into an unknown
prefix and matches again every alternative the corpus links today (its
existing target is the truth: any other link fails the test, and no
alternative unmatched today may link), then matches every corpus's
unmatched alternatives against the entries of the other corpora (any link
fails: wrong by construction). `TestUnknownPrefixPartialStripOracle`
(same variable) strips the routes of one file at a time and
keeps every other route known, so the existing tiers compete: K = 1 and 2
(the first segments) and an `above` variant (the segment after the first
one, which stays known as the entry's `above`); today's links into the
stripped file are the truth, every link out of it must stay (or become
`ambiguous` through UP-22), no alternative unmatched today may link, and
the counts are logged per target framework. Known gaps: an Express, FastAPI or Axum route reaches
this tier only once their route side stores entries (rows UP-5..UP-7); a
final catch-all entry is not checked for call parameters that may hold
another entry's literal (row LF-2 holds for `paths` only).

A Next.js route whose `basePath` is not read (row NX-R4) is not a
candidate of this tier: its entries stay in `paths` and only an
unresolved-base call reaches it, in the existing tiers. By decision U6 of
2026-11-23 such a link is aligned with row UP-19 (row NX-L1 of
docs/extract/routes.md): `match: suffix`, `route_prefix: unresolved`,
confidence 0.6, `assumes` NX-V1, UP-V1, UP-V2 on the path entry and the
call. Which route the call links to is unchanged.

### Unknown-prefix tier claim contract (client matcher)

Copied from the public claim contract's client matcher rows and its
default row; `TestUnknownPrefixMatcherContract` keeps this table, the
public one and the code table (`UnknownPrefixMatcherRows`) 1:1.

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-13 | a call alternative the existing tiers (exact, origin, suffix; ADR 0018) leave without a candidate | the `unresolved_prefix_paths` entries become its candidates; an alternative with any existing candidate (linked, ambiguous or test-only) is never matched against them, so no existing edge changes, except as row UP-22 states | an alternative a Next.js literal rewrite withholds (row NX-L3): no candidate |
| UP-14 | an alternative whose path is known from its start: base `none`, `literal`, `constant` or `configured`, no literal origin | candidate when the call's segments are `above` + V + `template`: the call starts with `above` (route parameters take any call segment), V is a run of zero or more literal call segments (the unknown prefix), and the rest matches `template` by the existing segment rules (route parameters, final catch-all, optional final parameter) | a call parameter inside V |
| UP-15 | an alternative with base `unresolved` and an origin that is not `unresolved` | candidate when the entry has no `above` and the call's template matches `template` entirely (aligned: the call's unknown base stands for the route's unknown prefix, assumption UP-V2) | an entry with `above` (its unknown part does not start the path, so the call's base cannot stand for it); any other overlap of two unknown parts |
| UP-16 | anchoring and minimum evidence | a candidate matches at least two literal route segments (of `above` and `template`) with literal call segments, and `template`'s first segment is a literal or a parameter matched by a call parameter | fewer than two: no candidate (the alternative stays `unmatched`) |
| UP-17 | uniqueness | the alternative links only when exactly one route has an entry that matches it with at least one literal segment, before UP-16's minimum and whatever prefix V each match implies; routes are never ranked against each other by literal count in this tier | two or more such routes: `ambiguous`, candidates and `candidate_count` as today |
| UP-18 | method, host, slash form and scope | the existing rules: equal method, ANY, client HEAD to a GET route of Express, Axum, net/http; a net/http host route only for its host; a strict-slash router's entry only for a call of its own slash form (TOP-2 of 2026-11-18); production calls prefer production entries, test-only outcomes as today | - |
| UP-19 | the link | one `calls_route` edge, provenance `inferred`, confidence 0.6; the path entry records `match: "suffix"` and `route_prefix: "unresolved"`; the call carries `assumes` `UP-V1` (and `UP-V2` for UP-15) | - |
| UP-20 | refused alternatives | none: a literal origin (the route's origin is not known), unless row UP-21 admits it in the project link pass; `origin_resolution: unresolved` (rows BC-2, BC-3: exact only; never proven, a declaration included), a Next.js route (never a candidate here) | - |
| UP-21 | project link pass: an alternative with a literal origin matched against the routes of another member, where a `consumes` entry of the call's consumer that covers it names that origin in `origins` (rows PL-R5, PL-L4 of the project link contract) and so names the provider | candidate as UP-14 reads a call known from its start (the declared origin stands for the provider's server; the prefix stays unknown); UP-16, UP-17 and UP-18 apply; a link is row UP-26 (PL-L7) | an origin no covering entry names (UP-20, PL-R5); the routes of the call's own member (as LF-1: no waiver) |
| UP-22 | an alternative whose every candidate in the existing tiers is tied only through paths ending in a final catch-all (an SPA or root fallback: `r.Get("/*", spa)`, `mux.Handle("/", ...)`, `app.get("*", ...)`), and an `unresolved_prefix_paths` entry matches it as rows UP-14, UP-15 and UP-18 read a call, with at least one literal segment (UP-17's competitor; UP-16's minimum is not needed for an ambiguity), on a production route (for a test call, any route) | `ambiguous`, as row LF-2 makes a catch-all ambiguous beside a more specific route: the candidates are the catch-alls and those routes, `candidate_count` as today; never linked to either (a router tries the route below the unknown prefix before the fallback) | only test-scope entries match a production call, or no entry matches with a literal segment: the catch-all link stands |
| D | anything else | nothing: no entry, no candidate, no call-side reading; the route keeps its existing state (empty `paths`, the existing diagnostics and `unresolved_mounts`), and a call of a helper of any other shape keeps the ordinary evaluation |

## Unknown-prefix call side (T033 UP-C)

A frontend often builds every request URL through one helper that
prepends a base read from runtime configuration, as dozzle does:

```ts
const config = { maxLogs: 400, ...JSON.parse(text) };
export const withBase = (path: string) => `${config.base}${path}`;
fetch(withBase("/api/version"));
```

The bounded evaluator cannot read `config.base`, so such a call used to be
`dynamic` (a leading piece that is not an environment base). Row UP-39 of
the [unknown-prefix contract](../contract/claims/unknown-prefix-paths.md)
reads it as an unresolved base followed by the argument's path, only for
a closed list of helper shapes that prepend exactly one base value to
their argument: a helper bound at its module's top level with one plain
parameter whose body is exactly `return X + p` or ``return `${X}${p}` ``,
where `X` is a module-level `const` of the helper's own module (or a
member read of one) that is never written to and whose value the
evaluator does not read. The call then has base `unresolved`,
`base_expression` the spelling of `X` (`config.base`), resolution
`evaluated` and the helper in `helpers`, and it is matched as any call
with an unresolved base: the exact and suffix tiers first, then the
unknown-prefix tier's aligned row UP-15. `clients.base_paths` applies to it
as to any unresolved base. Every other helper, and every call row UP-40
refuses (an argument that is not a string starting with a literal `/`, a
base that is a parameter, a `let`, a call, an imported value or a value
written elsewhere, a helper inside a function), keeps the ordinary
evaluation. dozzle's modules call `withBase` without importing it:
unplugin-auto-import inserts the import at build time and commits the
declaration file `assets/auto-imports.d.ts` (`const withBase: typeof
import('./stores/config').withBase` inside `declare global`). Row UP-41
binds such an unbound name to that module's export, from a declaration
file carrying the plugin's generated header, for UP-39 only (the
declaration form is cached with the declaration file's syntax). Fixture:
`testdata/fixtures/unknown-prefix-helpers`, test
`TestUnknownPrefixHelpers`.

### Unknown-prefix call side claim contract

Copied from the public claim contract's call-side rows and its default
row; `TestUnknownPrefixCallContract` keeps this table, the public one and
the code table (`UnknownPrefixCallRows`) 1:1.

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-39 | a call `H(a)` of a URL helper `H` bound at its module's top level (a `const` arrow or function expression, or a function declaration) with exactly one plain parameter `p` without a default, whose body is exactly `return X + p` or ``return `${X}${p}` `` (an arrow's expression body included): `p` used once, as the last piece, nothing before `X` or after `p`; `X` a member read `O.k` of a `const`, or a `const` itself, declared once at the top level of the helper's own module (not imported), never written to, whose value the bounded evaluator does not read (dozzle's `withBase`, `X` = `config.base`, `config` an object spreading the parsed page configuration); `a` evaluating to strings that each start with a literal `/` | the call's alternatives as for `a`, after a base `unresolved` whose `base_expression` is the spelling of `X` (resolution `evaluated`, `H` in `helpers`); the matcher reads them as any unresolved-base call: the existing tiers, then UP-15 | - |
| UP-40 | refused calls of a helper of UP-39's body shape | none (the ordinary evaluation stands; `dynamic` when it cannot read `X`): a helper not bound at its module's top level; `X` a parameter, a `let` or `var`, a call, an imported value, a value written elsewhere, or a `const` whose value the evaluator reads (a known object without the property is `undefined`, not a configured base); no argument, more than one, or a spread; an argument that is not a string starting with a literal `/` (a variable, an absolute URL, a relative path, a template starting with a substitution) | - |
| UP-41 | the helper of a UP-39 call reached through a declaration file generated by unplugin-auto-import (its leading comments hold `// Generated by unplugin-auto-import`): the callee is a name no binding of the calling file declares, and exactly one such file whose directory holds the calling file declares it inside `declare global { }` as `const NAME: typeof import('./module').NAME` (a relative module, the export of the same name; dozzle's `assets/auto-imports.d.ts`), the module resolving to a scanned file | the callee is that module's export, read by UP-39 and UP-40 only | any other reading of the helper (the call keeps the ordinary evaluation of an unbound name: `dynamic`); a declaration file without the header, another declaration form (`['NAME']`, a package module, another export name), a name two such files declare, a calling file outside the declaration file's directory |
| D | anything else | nothing: no entry, no candidate, no call-side reading; the route keeps its existing state (empty `paths`, the existing diagnostics and `unresolved_mounts`), and a call of a helper of any other shape keeps the ordinary evaluation |

## Path assumptions on links

A route path entry can rest on a path assumption of the route side
([routes](routes.md)): GR-V5 (a Go path served through a wrapper not
proven to keep it), RO-V1 (a FastAPI mount at an unknown prefix added
before the route may answer first) or RO-V2 (an Express app or router
mounted earlier below the path's prefix may answer first). A link through
such an entry rests on the same assumption, so it carries the ids exactly
as a link of the unknown-prefix tier carries UP-V1 and UP-V2:

- The matcher reads the `assumptions` of the route path entries through
  which the linked route won (its tier; production entries for a
  production call). When one of them rests on none, the link needs none
  (a path served without an assumption reaches the route); otherwise the
  link carries the union of their ids. In the unknown-prefix tier the
  same holds for the `unresolved_prefix_paths` entries that match under
  row UP-16.
- The ids go to the path entry's `assumes` and the call's `assumes`, after
  the Next.js (NX-V1, NX-V3) or unknown-prefix (UP-V1, UP-V2) ones. The
  confidence and the target are unchanged.
- They are shown wherever link assumptions are shown, with the route
  side's text: the caller reason `path_assumed` ([serve](../cli/serve.md)),
  the `assumptions` of a flow, architecture chain or change impact step,
  and in MCP answers as `route_path_assumptions` (ids, the texts in the
  envelope assumptions). A project link stores them in `assumes` and ends
  its assumption with `route path: assumes <ids>`
  ([project links](project-links.md)).

Fixture: `testdata/fixtures/link-path-assumptions` (RO-V1, RO-V2, GR-V5
in the exact tier, GR-V5 in the unknown-prefix tier, one route with a
wrapped and a plain entry), test `TestLinkPathAssumptions`.

## Committed bundler output (ADR 0085)

A file that a bundler wrote and the repository commits is still indexed,
but its client calls are not linked to routes: a bundle repeats the
requests of its sources, and minified code defeats the precision rules
above. A file is bundler output when its path matches a row below **and**
it is minified as a whole: lines of at least 1,000 bytes with at least one
`;` or `}` per 100 bytes hold at least half of its bytes (a long `data:`
URI or base64 string in a hand-written file is not dense code).

| Row | Bundler | Path layout |
|---|---|---|
| GB-1 | Next.js | `_next/static/` anywhere in the path, `.js` or `.mjs` |
| GB-2 | Vite (Rollup) | `assets/<name>-<hash>.js` (or `.mjs`), an 8-character `[A-Za-z0-9_-]` hash with a digit or capital letter (a lower-case word such as `user-settings.js` is no hash) |
| GB-3 | webpack (Create React App) | `static/js/<name>.<hash>.js` or `.chunk.js`, exactly 8 hex digits (CRA's default) |
| GB-U | anything else, a layout path whose file is not minified included | every rule above, unchanged |

Each call of such a file is a `client_call` with `status: "generated"` and
`generated_output: "GB-n"`; its path entries with a template have status
`generated`, no candidates and no `calls_route` edge. A wrapper's own
request keeps status `wrapper`. One info diagnostic per file with calls,
`client_calls_generated_output`, names the bundler, the row, the evidence
and the count. `clients.link_generated_output: true` in
[graphvein.yaml](../config.md) links them by the ordinary rules (no
diagnostic); it is read on every link pass. The project link pass keeps
the member's own result for a `generated` call (PL-R1). Fixture:
`testdata/fixtures/generated-bundle-output`, test
`TestGeneratedBundleOutput`.

## Next.js routes and SWR (T024a, ADR 0077)

Next.js route handlers and Pages Router API routes (`framework` `nextjs`,
`nextjs_pages`) join the candidates under the rules above, with the
additions of `docs/extract/routes.md` ("Matching (rows NX-L1..NX-L3)"): the
same-origin rule (a literal-origin alternative never matches them), `HEAD`
to a route `GET`, unresolved-base routes only for unresolved-base calls,
literal rewrites (`nextjs_rewrite` on the path entry: a rule that may match
withholds the project's candidates unless Next.js's rewrite order proves
the destination, which is then matched instead, T024a2), and
`assumes: ["NX-V1"]` (re-routed by an afterFiles or fallback rule also
`NX-V3`) on linked calls. `useSWR(k, f)` / `useSWRImmutable(k, f)` from
`swr` become `client_call` nodes with `library: swr` (row NX-L2) when `k`
is a literal or bounded const, or a key the bounded evaluator reads as
strings (a `null` alternative drops out), and `f` a resolved fetch wrapper
forwarding its first parameter unchanged as the URL of a fetch without
options, or an inline arrow whose expression body starts with a fetch or
axios GET of its one parameter (that request's own node becomes
`status: wrapper`); other SWR calls give the info diagnostic
`swr_call_unread`.

## Build-time constant bases

extractors-47 (T022 MR-B6c), additive. A bundler define such as webpack
`DefinePlugin({__API_URL__: ...})` replaces a bare identifier at build time;
the source only declares its type, `declare const __API_URL__: string` in a
declaration file. Such a constant used as a URL base is recorded as an
unresolved base whose `base_expression` is its name, never a value. Bases
from URL objects supply only the origin: an absolute-path input discards
the base's path (WHATWG), so the object's path stays whole.

A build-time constant is a name declared by `declare const NAME: string`
(type exactly `string`, no initializer) at the top level of a script
declaration file (`.d.ts`, `.d.mts`, `.d.cts` without top-level
import/export) or inside `declare global { const NAME: string }` of any
declaration file, that is unbound at the use site and has no value
definition in the repository: no top-level value declaration of NAME in any
other JS/TS file (module or script) and no assignment to
`globalThis`/`window`/`self`/`global` `.NAME` (or a string subscript).
"Base-shaped" below means an environment member, a module const bound to
one (the existing rules), or a build-time constant.

| Row | Form | Claim |
|---|---|---|
| BC-1 | a build-time constant as the leading piece before a slash (template, concatenation; also the one leading base of fetch wrappers) | base `unresolved`, `base_expression` its name; matched as any unresolved base (exact 0.9, suffix 0.6, the unresolved-base guards) |
| BC-2 | a URL object `new URL(path, E)` with an absolute-path input and E base-shaped, every authority assignment (if any) also from E | `origin_resolution` unresolved, base `unresolved`, `base_expression` E's spelling; exact matching only, confidence 0.7 |
| BC-3 | a URL object whose `protocol` and `host` are both assigned as `u.protocol = A.protocol; u.host = A.host` (further `hostname`/`port` the same way) from `const A = new URL(E)` (one argument, E base-shaped, same block, declared before, A referenced only by such reads) | as BC-2; the object's own origin (literal or second argument) is not a source |
| BC-4 | an Orval call whose proven mutator composes the generated URL by BC-1, BC-2 or BC-3 | the call's base and `base_expression` are the mutator's |
| BC-U | anything else: another declared type, `let`/`var`, a module-scoped `declare const`, a value definition, a local binding of the name, an authority part from another source (a literal origin with only some properties assigned, two different constants, a rewritten or escaping A, an unknown second argument) | the existing rules: an unbound identifier stays dynamic; a URL object keeps base `none` with its unresolved origin, or stays dynamic |

The base supplies only the origin, so BC-2/BC-3 alternatives never use the
suffix tier (their origin is unresolved) and link exact at 0.7. The
unresolved-base guards (the leading-parameter anchor and the literal-free
route exclusion) still apply to them, as to merged alternatives (ADR 0018):
stored attributes cannot tell an origin-only base from one that supplies
path segments, so a route starting with a parameter, or without a literal
segment, is not a candidate (conservative recall loss). `clients.base_paths` never
applies to them. Each row has calls in `testdata/fixtures/build-constants`
(`TestBuildConstantBases` checks the calls against these rows and the rows
against this table). Known gaps: the assumption is that the declaration is
in the compilation that includes the call (tsconfig `include` and `files`
are not read); bundler define entries themselves are read only by
`graphvein project suggest` (rows SG-D1, SG-D2); with protocol and host
assigned, a port of the original origin survives when E's host has no port
(the host setter keeps it), which the unresolved origin does not record;
`Object.defineProperty(globalThis, ...)` and other runtime definitions are
not value definitions here.

## What a call sends (T029)

Rows SD-1..SD-5 of the [shape differences claims](../contract/claims/shape-differences.md),
additive, link time over the cached client syntax (no new cached syntax).
After client linking, a client_call whose `wrapper` is null and whose
status is not `wrapper` gets the attribute `sends` (version 1) when the
keys it sends are proven:

- a fetch with a literal JSON body: an init object literal (no spread,
  computed key or method) with `headers` holding exactly one literal
  `Content-Type: application/json` and `body: JSON.stringify({...})`
  (row SD-2); nested object literals are read one level at a time;
- a fetch whose URL template ends in `?${new URLSearchParams({...})}`
  (row SD-3);
- a proven hey-api request in a generated operation whose
  `Options<TData>` type is read: the required properties of `TData`'s
  `body` (row SD-4) and `query` (row SD-5) types.

A literal object is `complete` when it has no spread, computed key,
method or accessor; a typed part never is. The server compares `sends`
with the linked route's stored shape when it presents the link
([serve](../cli/serve.md), `shape_differences`). Every other call form
records nothing.
