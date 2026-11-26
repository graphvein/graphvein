# Express CommonJS fixture: unscanned loader candidates

Hand-written Express code in CommonJS; nothing is installed or executed by
the tests. `web/app.js` exports the app so an oracle can serve it.

Inert names (the test copies the fixture to a temporary directory and
restores the real names before indexing; an oracle does the same):

| stored as | restored as | why |
|---|---|---|
| `_graphveinignore` | `.graphveinignore` | a live project ignore file would apply to the repository |
| `web/_gitignore` | `web/.gitignore` | a live Git ignore file would hide fixture files from Git |
| `web/_vendor` | `web/vendor` | the scanner always skips the name `vendor` |

Row CJ-U1 of `docs/extract/routes.md` ("CommonJS modules"): Node's loader
tries `X`, `X.js`, `X.json`, `X.node`, then the directory `X`. When an
earlier candidate exists on disk but was not scanned, Node loads code
Graphvein never read, so the `require` gives no value and the mount gets
`route_mount_unresolved` naming the unscanned entry:

- U1 `require("./routes/u1")`: `routes/u1.js` is ignored by `.gitignore`;
  `routes/u1/index.js` (scanned) composes no path.
- U2 `require("./routes/u2")`: `routes/u2.js` is excluded by
  `.graphveinignore`; `routes/u2/index.js` composes no path.
- U3 `require("./vendor")`: the extensionless file `vendor` (a name the
  scanner always skips) comes before `vendor.js`, which composes no path.
- K1 (control) `require("./routes/k1")`: `routes/k1.js` is scanned and
  comes first, so `/k1/from-file` composes; the ignored directory
  `routes/k1/` comes later and changes nothing.
