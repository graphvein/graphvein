# graphvein project

```
graphvein project init --name NAME [--dir DIR] [REPOSITORY=]PATH...
graphvein project add [--manifest DIR] [REPOSITORY=]PATH
graphvein index --project MANIFEST [--workers N] [--timeout D] [--max-file-size N]
graphvein status --project MANIFEST
graphvein project suggest [--manifest DIR] [--json] [--accept N]
graphvein project forget [--manifest DIR] REPOSITORY
graphvein callers|impact <target> --project MANIFEST [--repository MEMBER] [--depth N] [--json]
graphvein changes [impact ID] --project MANIFEST [--repository MEMBER] [--base B] [--json]
```

A project groups repositories that work together, typically a frontend and
a backend kept in separate repositories, so Graphvein can link calls in one
to routes in another while every repository keeps its own identities.
Design and reasons: [ADR 0075](../adr/0075-multi-repository-projects.md)
; plan: the T022 task record (internal).

Overview, in the order you meet it:

1. **Write the manifest**: `project init` and `project add` ("Commands";
   the file is described in "The manifest" and "The local file", checked
   by "Validation rules").
2. **Index and link**: `index --project` indexes every member and runs
   the link pass; `status --project` shows member freshness and stale
   links ("Indexing a project").
3. **Resolve ambiguity**: declare what each member `consumes`, with
   selectors and link scopes where one repository holds several servers
   ("The manifest"); `project suggest` proposes declarations from env,
   compose and dev-server files ("Suggestions").
4. **Query**: `callers`, `impact` and `changes` with `--project`
   ("Querying a project").
5. **Serve**: `serve --project` gives the web UI with the Project view,
   where declarations can also be edited ("Serving a project").

A cross-repository link is `inferred`, at most as confident as its path
match, shows the assumption it rests on and is never labelled Precise
([precision](../extract/precision.md#cross-repository-links)).

## Commands

- `project init` writes `graphvein-project.yaml` in `--dir` (default: the
  current directory; created if missing) with one member per `PATH`. It
  refuses to overwrite an existing manifest.
- `project add` appends one member to the manifest in `--manifest`
  (default: the current directory). The rest of the file, comments
  included, is kept; the file is re-indented with two spaces.
- `PATH` must be an existing directory (the member's checkout). It is
  written relative to the manifest's directory, with forward slashes.
- The repository name: when the member's own `graphvein.yaml` sets
  `repository:`, that name wins and is written to the manifest; a
  different `REPOSITORY=` on the command line is refused. Otherwise
  `REPOSITORY=` is required: the directory name is never used for a
  project member, because it differs between machines. The first `=` in
  an argument separates the name from the path.
- Both commands build the new manifest in memory, validate it completely
  (rules below, including the local file if present) and only then
  replace the file atomically. A refused command writes nothing.
- `project init` ends by reminding you to keep
  `graphvein-project.local.yaml` out of version control (for example in
  `.gitignore`); see "The local file".
- Exit codes: 0 written; 2 usage error or refused manifest; 1 file error.
- `project forget REPOSITORY` removes an old member name from the
  project store: its project rows (its member record, the links and
  outcomes naming it) and its stored index (runs, files, nodes, edges,
  diagnostics, fact cache, change journal, pins and ref snapshots), in
  one transaction under the project lock (on Postgres also under that
  repository's index lock). Use it after renaming a member in the
  manifest on disk; a rename from the web UI does this itself. The link
  journal keeps its past passes. `--manifest` is the manifest or its
  directory (default: the current directory); the store is the
  project's (`GRAPHVEIN_POSTGRES_URL`, `GRAPHVEIN_SQLITE_PATH` or
  `.graphvein/index.db` in the manifest's directory). It refuses, removing
  nothing: a name still in the manifest; a name no stored link pass of
  the project (the link journal included) had as a member, so another
  repository's index in a shared store is never removed by a typo; a
  repository that is a member of another stored project; a ref snapshot
  name (`@ref:`). A name with nothing stored left is not an error
  ("Nothing stored under ..."). Run `graphvein index --project` afterwards
  to link the project under the new name.
  Exit codes: 0 removed or nothing stored; 2 usage error or refused; 1
  store or lock error.

```
graphvein project forget --manifest . backend
Removed backend from project shop: its stored index and its project rows. Links of the other members to it are gone; run graphvein index --project to link the project again.
```

## Indexing a project

`graphvein index --project MANIFEST` (the manifest file or the directory
holding it) indexes every member and then links them:

1. Each member with a checkout on this machine is indexed into the project
   store, as `graphvein index` would index it, under the repository name
   the manifest gives, with its own index lock and its own fact cache.
   Each member's lock is released before the next member and before the
   link pass. Members are indexed in manifest order.
2. The link pass then runs under the project lock: it reads every member's
   stored nodes and stores the cross-repository links and every source's
   outcome (linked, ambiguous, unmatched, incomplete, ...) in one
   transaction. The rules are the claim contract in
   [docs/extract/project-links.md](../extract/project-links.md).

The project store is `<manifest directory>/.graphvein/index.db`, or
`GRAPHVEIN_SQLITE_PATH`, or the Postgres database in
`GRAPHVEIN_POSTGRES_URL`. With SQLite the project lock is the store's index
lock, so nothing else writes the store during the pass; with Postgres it is
an advisory lock of its own, and a pass that finds a member published
while it read is retried (up to three times). Single-repository flags
(`--repository`, `--emit`, `--indexer`, a path) are refused: every member's
name and path come from the manifest.

- A member without a checkout here (no `path`, or a path that does not
  exist) is not indexed. When the store already holds its index (for
  example a shared Postgres), it is linked from the store; otherwise it is
  reported, and calls that could reach it are `incomplete` (no link) until
  it is indexed.
- A member that fails to index never stops the others. The failure is
  printed and stored with the pass; the pass links the member from its
  previous stored index, if any, and `status --project` shows those links
  as stale. The command exits 1.
- A pass that changes nothing writes no link change: a member indexed
  again with the same content gets a new run, and its links are rewritten
  with the new run but counted as unchanged.

Output: one line per member, then the pass with its link counts (added,
removed, changed, unchanged) and the outcome counts. Exit codes: 0 every
member indexed and linked; 1 a member failed to index, a lock was held or
the pass failed; 2 usage error or invalid manifest.

`graphvein status --project MANIFEST` reads the project store and writes
nothing:

- each member: `indexed; up to date`, `indexed; N files changed since its
  last index` (when its checkout is here), `indexed; no checkout here,
  linked from the store`, or `not indexed`; and, against the last link
  pass, `indexed again since the last link pass`, `failed to index before
  the last link pass` or `not in the last link pass`; for an indexed
  member, whether its `graphvein.yaml` index settings changed since its
  last index: `settings unchanged since its last index`, `settings changed
  since its last index: KEYS (re-index to apply)`, or `settings since its
  last index unknown: REASON` (also for a member without a checkout here;
  row ST-47 of the [settings claim contract](../contract/claims/settings.md));
- the link pass: `none yet`, `fresh`, or `stale` with the reasons (the
  manifest's declarations changed, a member indexed again, failed, joined
  or left);
- the number of links, fresh and stale, and the outcome counts;
- every stale link (the first 20, then a count) with its reason;
- every stored repository a kept link pass named as a member that the
  manifest no longer names (a member renamed or removed on disk; T022 review), with `remove it with graphvein project forget NAME`. Once
  the journal no longer keeps a pass naming it, it is not listed.

A link is fresh only while both members' latest runs are the runs it
records, neither member failed to index before the pass, and the
manifest's declarations are those the pass used. The manifest hash covers
the project name, the members in order and their `consumes` and `scopes`;
member paths and comments are left out, so moving a checkout or editing a
comment does not mark links stale. Run `graphvein index --project` again
to refresh them.

## Querying a project

Once `graphvein index --project` has stored a link pass, `callers` and
`impact` with `--project MANIFEST` traverse every member and cross
repositories through the stored links, and `graphvein changes --project`
reports the link journal diff; `graphvein changes impact ID --project
MANIFEST --repository MEMBER` continues a change's impact into other
members ([queries](queries.md#project-scope---project)). Every result
names its repository; a step through a project link shows the link's
assumption, is inferred and at most as confident as the link, and a stale
link (the same freshness rule as `status --project`) is followed and
marked with its reasons. Examples on a frontend + backend project:

```
graphvein callers 'route:src/server.ts#GET%20%2Fusers' --project . --repository backend
graphvein impact listUsers --project . --repository backend --depth 4
graphvein changes impact 'table:src/db/schema.ts#users' --project . --repository backend --depth 6
graphvein changes --project .
```

## Serving a project

`graphvein serve --project .` serves the project's web UI and HTTP API:
every endpoint answers per member through a `repository` parameter, and
`/api/v0/project` shows the members, their freshness and the link pass
([serve](serve.md#project-mode---project)). The Project view's "Edit
declarations" writes the manifest through typed edits with a diff
preview ([serve](serve.md#project-manifest-endpoints-serve---project)); renaming a member
there also removes its stored index and links under the old name from
the project store (the preview names it, "delete stored index of NAME (N
nodes)", and the write needs that explicit confirm; T022 review), and offers a project re-index (after a rename made
on disk, `graphvein project forget OLD-NAME` does the same). A manifest edited on
disk while the server runs (an editor, `git checkout`, `project add`) is
served from the next project request; its links are stale until the next
link pass, and a file that does not validate is ignored (logged) until
it does.

## The manifest

`graphvein-project.yaml` lives either in a separate project folder (often
the parent of the checkouts) or inside one member, pointing at the others
by relative path. Commit it.

```yaml
version: 1
name: realworld
members:
  - repository: frontend
    path: ../realworld-react
    consumes:                 # optional: narrows the providers
      - repository: express
        select:               # optional: which calls this entry covers
          bases: [__API_URL__]
          origins: ["http://localhost:3000"]
          path_prefixes: [/api]
  - repository: express
    path: ../express
    scopes:                   # optional: link scopes inside the member
      - name: users
        path: apps/user
      - name: articles
        path: apps/article
```

- `version` (integer 1), `name` (the project's name, same rules as a
  repository ID) and `members` (a nonempty list) are required.
- A member has `repository` (required), and optional `path`, `consumes`
  and `scopes`. A member without a path, or whose path does not exist on
  this machine, is allowed: its facts can come from a shared store.
- `consumes` lists providers: a member's repository as a string, or a
  mapping with `repository`, optional `scope` (one declared scope) or
  `default_scope: true` (only the member's default scope: its routes
  outside every declared scope; the member must declare scopes), and
  optional `select`.
  Without `consumes`, every other member is a candidate provider (ADR 0075
  "Precision"). `select` lists `bases` (base expressions or environment
  member names as the clients extractor records them), `origins`
  (`http(s)://host[:port]`, lower case) and `path_prefixes` (starting with
  `/`); they narrow client calls only, and an OpenAPI operation follows
  the entry's provider unless an entry's `origins` names its server
  origin. How an entry covers a call or an OpenAPI operation, and which link
  each case gets, is the claim contract in
  [docs/extract/project-links.md](../extract/project-links.md).
- `scopes` declare link scopes: named folders inside the member, typically
  several servers in one repository. The rest of the member is its default
  scope. A scope may have its own `consumes`; the member's `consumes`
  applies to its default scope (a scope without `consumes` does not
  inherit it). Each scope is a link endpoint of its own: a `consumes`
  entry with `scope` links only into that scope, one with
  `default_scope: true` only into the default scope, an entry with
  neither into any scope of the repository, and a call matching routes of two
  scopes is ambiguous. A scope whose `consumes` names a scope of its own
  repository resolves calls the repository's own index left ambiguous
  between its servers, as a project link (an overlay; the repository's
  own index still records them ambiguous). Rules: "Link scopes" in
  [docs/extract/project-links.md](../extract/project-links.md#link-scopes).
- When the checkout is present and its `graphvein.yaml` sets
  `repository:`, it must equal the manifest's name.
- Optional `tryit` holds project-wide "try it" environments (T029): one
  name and label with a base URL per member that runs in it. A member's
  own `graphvein.yaml` environments apply too. An environment takes the
  same optional `auth`, `tls`, `use_system_proxy` and `limits` as in
  `graphvein.yaml`, for every member; a relative `tls.ca_file` is read
  from the project directory. Format and rules:
  [configuration: Try it](../config.md#try-it).

  ```yaml
  tryit:
    environments:
      - name: staging
        label: staging
        base_urls:
          express: https://api.staging.example.com
          frontend: https://staging.example.com
  ```

## The local file

`graphvein-project.local.yaml`, next to the manifest, holds machine-specific
member paths. Do not commit it. It may only set `path` for members the
manifest lists; the path may be absolute or relative to the manifest's
directory, in the operating system's own form.

```yaml
version: 1
members:
  express:
    path: D:/src/express
```

## Validation rules

Every rule refuses the manifest with the file, the line when known and the
reason. Each rule has at least one fixture in
`internal/config/testdata/project/refuse-<rule>*` with a golden error; a
test keeps this table and the fixtures 1:1.

| Rule | Refused |
|---|---|
| `yaml` | Malformed YAML, an empty file, several YAML documents, or a root that is not a mapping. |
| `alias` | YAML anchors or aliases anywhere. |
| `keys` | Non-string, duplicate or unknown keys in any mapping. |
| `version` | `version` missing or not integer 1. |
| `name` | `name` missing, not a string, empty, with NUL or with `@ref:`. |
| `members` | `members` missing, not a list, or empty. |
| `member-repository` | A member without `repository` (the directory name is never used), or one that is not a valid repository ID. |
| `member-repository-unique` | Two members with the same repository. |
| `member-path` | A manifest path that is empty, absolute (including `C:`), or uses backslashes. |
| `member-path-unique` | Two members resolving to the same directory. |
| `member-path-directory` | A member path that exists but is not a directory. |
| `member-name-conflict` | A member whose own `graphvein.yaml` names a different repository. |
| `member-config` | A member whose own `graphvein.yaml` is invalid. |
| `consumes` | `consumes` that is not a nonempty list of strings or `repository`/`scope`/`default_scope`/`select` mappings. |
| `consumes-target` | A `consumes` entry naming a repository that is not a member, or a scope its member does not declare. |
| `consumes-default-scope` | `default_scope` that is not the boolean `true`, given together with `scope`, or naming a member that declares no scopes. |
| `consumes-self` | A member, or a scope, consuming itself (a member's default scope naming its own repository's `default_scope` included). |
| `consumes-duplicate` | The same provider (repository and scope, or repository and default scope) twice in one `consumes` list. |
| `select` | A `select` with no list, or a list that is empty or not a list of strings. |
| `select-base` | A base that is empty or contains whitespace. |
| `select-origin` | An origin that is not lower-case `http(s)://host[:port]` without path, query, fragment or user. |
| `select-path-prefix` | A path prefix not starting with `/`, or containing a query, fragment or whitespace. |
| `scope` | A scope without `name` or `path`, or a name using characters other than letters, digits, `.`, `_` and `-`. |
| `scope-path` | A scope path that is `.`, absolute, uses backslashes or `:`, or has `.`, `..` or empty elements. |
| `scope-unique` | Two scopes of one member with the same name. |
| `scope-overlap` | Two scopes of one member where one folder contains the other. |
| `local-version` | A local file without `version: 1`. |
| `local-member` | A local file naming a repository the manifest does not list. |
| `local-path` | A local member entry with a key other than `path`, or an empty path. |

## Suggestions

```
graphvein project suggest [--manifest DIR] [--json] [--accept N]
graphvein callers|impact <target> --project MANIFEST [--repository MEMBER] [--depth N] [--json]
graphvein changes [impact ID] --project MANIFEST [--repository MEMBER] [--base B] [--json]
```

`project suggest` (T022) reads the project's files and proposes
`consumes` entries with the evidence for each. It only reads: it never
runs a file, a package script or a dev server, and listing never writes.
Nothing changes how calls link until you accept a proposal with
`--accept N` and link the project again (`graphvein index --project`).

- Files read: in every member checkout, `.env` and `.env.*` files,
  Compose files (`compose.yaml`, `docker-compose.yml` and variants such as
  `docker-compose.override.yml` or `dev.docker-compose.yml`), `vite.config.*`,
  `webpack.*` configs (`.js`, `.mjs`, `.cjs`, `.ts`, `.mts`, `.cts`) and
  `package.json`; and Compose files directly in the project folder. Hidden
  folders, `node_modules`, `vendor`, `dist`, `build`, `target`, `out`,
  `coverage` and nested member checkouts (read as their own member) are
  skipped; files over 1 MiB are not read.
- An **address** is a URL a member calls: an env or Compose `environment`
  value, or a dev-server proxy target. A **port** is one a member declares:
  an env port key, or a Compose `ports`/`expose` entry of a service built
  from that member (its literal `build` context lies in the member's
  checkout; a service that only runs an image belongs to no member).
- A proposal is made only when an address reaches exactly one other
  member: a loopback address (`localhost`, `127.0.0.1`, `[::1]`) whose
  port that member declares, a Compose `environment` address naming a
  service of the same Compose file built from that member, with that
  container port, or an address outside Compose (an env file, a define, a
  proxy target) naming a service of the Compose files in its own member
  checkout, every service of that name built from that member. Otherwise the address is listed as **unmatched
  evidence** with the reason (no member, only the member itself, or
  several members) and nothing is proposed.
- A proposal with a base is checked against the consumer's stored calls
  (the project's store, as `graphvein index --project` writes it): its
  `bases` keep only the key and the define constants (`SG-D1`) that a
  stored call records as its base expression, its `origins` only an origin
  a stored call uses literally. When nothing is left, the address is
  unmatched evidence "no calls use this base" (`SG-M5`): a backend's CORS
  origin, for example, proposes nothing. Without a stored index every such
  address says to run `graphvein index --project` first. Proxy proposals
  (`path_prefixes`, or none) are not checked.
- The proposed `select`: for an env or Compose address `bases: [KEY]` (the
  key holding the URL, and the define constants reading it; a base covers
  `KEY`, `import.meta.env.KEY` and `process.env.KEY`) and
  `origins: [the URL's origin]`, each kept only when stored calls use it
  (above); for a Vite or
  webpack proxy `path_prefixes: [the proxied path]`; for a Create React App
  `proxy` no `select` (it forwards every path). Addresses of one consumer
  and provider are merged into one proposal per kind. A `bases` proposal
  and a `path_prefixes` proposal for the same provider stay separate,
  because one entry's `path_prefixes` would also restrict its `bases`; the
  manifest allows one entry per provider, so after accepting one the other
  is listed as already declared.
- A proposal whose evidence lies in a declared scope folder names that
  scope (the consumer's `scopes[].consumes`, or the provider's `scope`).
- Evidence shows the member, file, line, key and value read. Values that
  could be secrets are never read or shown: URLs with credentials, a query
  or a fragment are listed as not read, without their value.

`--accept N` takes proposal N of the current listing, appends it to the
consumer's `consumes` list through the same writer as `project add`
(comments kept, the whole manifest validated, atomic replace; a refused
proposal writes nothing). A proposal for a provider the consumer already
declares is refused: edit that entry instead. When it is the consumer's
first entry, the command says so: from then on only calls an entry covers
link across repositories ([project links](../extract/project-links.md)).
`--json` prints the listing as structured data (`proposals`, `unmatched`,
`unread`), the form the web UI reads. Exit codes: 0 listed or written; 2
usage error, invalid manifest or refused proposal; 1 file error.

### Read forms

The readers follow a claim contract: each row says what it reads and when
it refuses; every form no row covers goes to the single default `SG-U`
(listed under "Not read" with the reason). Configuration written in code is
read only in the literal forms below. Code: `internal/projectsuggest`
(`Rows`); a test keeps this table, the code and the fixture goldens
(`testdata/fixtures/projectsuggest*`) 1:1.

| Row | Reads | Yields | Refused (not read) |
|---|---|---|---|
| `SG-E1` | `.env*` line `KEY=VALUE` (optional `export`, quotes, ` #` comment) whose value is an http(s) URL | address; `bases: [KEY]`, `origins: [origin]` | `$` interpolation or escapes, credentials, query or fragment, non-numeric port; non-http(s) values (database URLs) are not addresses |
| `SG-E2` | `.env*` line `PORT`, `HTTP_PORT`, `SERVER_PORT`, `APP_PORT` or `API_PORT` | the member listens on that local port | a value that is not a port 1-65535; other `*_PORT` keys (they usually name a port the member connects to) are not read |
| `SG-C1` | Compose `environment` (list or mapping) of a service built from a member, value an http(s) URL | address of that member, matched by service name only inside the same file | as `SG-E1`; a service without a literal local `build` context, with `extends` or a `<<` merge key |
| `SG-C2` | Compose `ports` of a service built from a member: `"C"`, `"P:C"`, `"IP:P:C"` (optional `/proto`), long syntax `target`/`published` | published `P`: a local port; container `C`: a port of the service name | interpolation, ranges, more than three parts, non-literal values |
| `SG-C3` | Compose `expose` of a service built from a member | a container port of the service name | anything but one port |
| `SG-V1` | `vite.config.*`: `export default {…}` or `export default defineConfig({…})` with literal `server.proxy` entries `'/path': 'URL'` or `'/path': { target: 'URL', … }` | address; `path_prefixes: [/path]` | config computed in code (a function, variable, spread or call), a key that is a pattern (`^…`, globs) or not starting with `/`, a non-literal target, `rewrite`, `pathRewrite`, `router` or `bypass` |
| `SG-W1` | `webpack.*`: `module.exports = {…}` or `export default {…}` with literal `devServer.proxy` object entries (as `SG-V1`) | as `SG-V1` | as `SG-V1` |
| `SG-W2` | `webpack.*` literal `devServer.proxy` array items `{ context: '/path' or ['/path', …], target: 'URL' }` | address per context path; `path_prefixes` | a computed or empty context, and as `SG-V1` |
| `SG-R1` | `package.json` `"proxy": "URL"` when the package depends on `react-scripts` | address; no `select` | no `react-scripts` dependency (nothing else reads the field), a non-string `proxy` |
| `SG-D1` | a define entry `NAME: JSON.stringify(process.env.X)`: webpack `new DefinePlugin({…})` or `new webpack.DefinePlugin({…})` with one literal object argument anywhere in a `webpack.*` file, or Vite `define: {…}` in the literal exported config (`SG-V1` forms); `NAME` an identifier, `process.env.N` or `import.meta.env.N` (base `N`); the config loads env files with dotenv (`require('dotenv')`, `dotenv/config`) | the constant is X at build time: an `SG-E1` address of X in an env file in the config's folder also offers base `NAME` | a computed argument, key or value (a fallback `X \|\| '…'`, a template, a variable, raw code such as `'process.env.X'`); a config without dotenv (listed: no env file is tied to it) |
| `SG-D2` | a define entry (forms as `SG-D1`) whose value is `JSON.stringify('URL')` or the JSON text `'"URL"'` | address with base `NAME` | as `SG-D1`, and as `SG-E1` for the URL |
| `SG-M1` | a loopback address whose port exactly one other member declares (`SG-E2` or a published `SG-C2` port) | proposal | - |
| `SG-M2` | an `SG-C1` address naming a service of the same Compose file built from exactly one other member, with that container port (`SG-C2`, `SG-C3`) | proposal | - |
| `SG-M3` | an address no other member declares, only the member itself, or several members; any other host (a public name is never guessed) | unmatched evidence with the reason; no proposal | - |
| `SG-M4` | an address read outside a Compose file (`SG-E1`, `SG-D2`, a proxy target) whose host is a service name of the Compose files in the address's own member checkout, every service of that name built from one other member (or one scope of it), one of them declaring that container port (`SG-C2`, `SG-C3`) | proposal | a service of that name built from no member (an image, an unread service: unmatched, the name is not proven), from several members, or only from the member itself; no service of that name declaring the port |
| `SG-M5` | a matched address (`SG-M1`, `SG-M2`, `SG-M4`) with a base: of its base, the `SG-D1` constants reading it and its origin, none is recorded by a stored call of the consumer (in the evidence's scope) that the pass could link, or the consumer has no stored index | unmatched evidence: "no calls use this base" (or: index the project first); no proposal | - |
| `SG-U` | everything else, and every refused form above | listed as not read with the reason; nothing proposed | - |

Not read at all: Kubernetes manifests, reverse-proxy configs (nginx,
Traefik labels), `setupProxy.js` and `env_file` references. A define
constant written as a bare identifier (`__API_URL__`) is read (`SG-D1`);
since extractors-47 the clients extractor records a call through it as an
unresolved base with that `base_expression` when the constant is declared
only in a declaration file (docs/extract/clients.md "Build-time constant
bases"), so a matched address proposes `bases: [__API_URL__]`; without
such a declaration the call stays dynamic and the address is `SG-M5`.
