# Express path claim contract (Express 4 and 5)

Task T028. This table states which path template Graphvein claims for an
Express route path string (a registration pattern or a `use` mount prefix)
under each Express major, and where it claims nothing. The Express route
linker (`internal/extract/routes/express_paths.go`) and this table are kept
1:1: every form the readers model maps to exactly one row
(`ExpressPathContract`), every other form goes to the single default row
EX5-W14, and `TestExpressPathContract` checks that the code rows equal this
table and that the fixture `testdata/fixtures/express5-paths` reaches every
row. The oracle `scripts/oracle/express_paths.sh` serves that fixture with
Express 4.21.1 and 5.1.0 and compares every claim with what Express matched
(`TestExpressPathOracle`).

Why the major matters: Express 4 parses paths with path-to-regexp 0.1.x
(text becomes a regular expression; `*` is `.*`, `:name?` is optional, other
regular-expression characters pass through) and Express 5 with
path-to-regexp 8 (`:name` and `*name` captures, `{...}` optional groups;
`( ) [ ] + ? !` and unnamed `*` are refused). The same text can mean
different paths: `/*splat` is a named catch-all in Express 5 and `.*`
followed by the literal `splat` in Express 4.

Shared rules, unchanged: the composed template joins mount prefixes and the
pattern with `/`, collapses repeated slashes, has a leading `/` and no
trailing `/` except for the root (both majors ignore a trailing slash by
default). Case is kept as written (Express matches without case by default;
the matcher compares literals as written). A route path entry claims the
requests the matcher's reading of the template matches (the oracle reads
it keeping slashes: an empty segment matches only inside a catch-all, one
trailing slash is ignored as both majors ignore it): a literal segment
equal, `{x}` any one segment, a final `{*x}` or `{*}` one or more
segments, an `optional: true` entry also without its final parameter.

A piece no row reads composes no path: a registration pattern gives the
route no path entry, a mount prefix gives the routes below it no path
through that mount (they may still be `route_unmounted`). The route keeps
its node, handler and middleware.

| id | major | form | claim | refusal / unknown |
|---|---|---|---|---|
| EX5-W1 | 4 | literal text: characters other than `\ : * ? + ( ) [ ] { } \| ^ $` and the never-matched bytes of EX5-W14 (`.` is literal) | the text as written | - |
| EX5-W2 | 4 | `:name` (name `\w+`) filling its segment, not followed by `(` or `*` | `{name}` | a parameter inside a segment (`/:file.:ext`, `/v:id`): EX5-W14 |
| EX5-W3 | 4 | `:name?` filling its segment | the last token of a registration pattern (a trailing `/` aside): `{name}` and `optional: true` on the entry; anywhere else, a mount prefix included: `{name}` without `optional` (the present form only; T028 review) | Express 4 also matches without the segment of an optional parameter that is not the last one (`/a/:id?/edit` answers `/a/edit`): not claimed, a documented recall gap |
| EX5-W4 | 4 | `*` as the last character of a registration pattern, at its start or after `/` | `{*}` as the final segment: one or more segments | Express 4 also matches zero segments (`app.get('*')` answers `/`, a mounted router's `/*` answers the bare mount path): not claimed, a documented recall gap; `*` elsewhere (inside a segment such as `/*splat`, before more text, in a mount prefix): EX5-W14 |
| EX5-W5 | 5 | literal text: characters other than `{ } ( ) [ ] + ? ! : * \` and the never-matched bytes of EX5-W14 (`. \| $` are literal); trailing slashes are dropped before reading, as router 2.x `loosen` does | the text as written | - |
| EX5-W6 | 5 | `:name` (ASCII name `[A-Za-z_$][A-Za-z0-9_$]*`) filling its segment | `{name}` | a parameter inside a segment (`/:from-:to`), a quoted or non-ASCII name: EX5-W14 |
| EX5-W7 | 5 | `*name` as the last token of a registration pattern, after `/` (`/*splat`, `/files/*path`) | `{*name}` as the final segment: one or more segments (`app.use('/r', router)` + `router.get('/*splat')` is `/r/{*splat}`) | `*name` before more text or in a mount prefix: EX5-W14 |
| EX5-W8 | 5 | `*name` as the whole registration pattern (`*splat`) | `{*name}` after the mount path: one or more segments | Express 5 also matches zero segments (`/`, or the bare mount path): not claimed, a documented recall gap |
| EX5-W9 | 5 | `{...}` optional groups in a registration pattern, nested or not | one path entry per combination (each group present or absent, path-to-regexp 8's expansion), each read by EX5-W5..EX5-W8; at most 16 combinations | more combinations; a combination with an empty segment or a trailing slash the written path does not have (`/g/{*rest}` gives `/g/`, `/h/{:id}` `/h/`, `/a/{/b}` `/a//b`: Express 5 does not collapse them; T028 review); a group in a mount prefix (a mount takes the first alternative that reaches a segment boundary, so its alternatives are not a union): EX5-W14 for the whole piece |
| EX5-W10 | 5 | a form path-to-regexp 8 refuses: `:` or `*` without a name, `( ) [ ] + ? !`, an unmatched `}`, an unterminated group, a capture right after another capture with no text between (`/:a{:b}`) | no path: Express 5 throws at the registration; warning `route_pattern_invalid` | - |
| EX5-W11 | unknown | a piece EX5-W1..EX5-W4 and EX5-W5..EX5-W9 read to the same entries (literals, whole-segment parameters) | those entries | readings that differ (`/*splat`, `*`, `:id?`, groups, `\| ^ $ !`): EX5-W14 |
| EX5-W12 | major | the nearest `package.json` at or above the file of the call (registration or `use`) that declares `express` in `dependencies`, `devDependencies`, `peerDependencies` or `optionalDependencies` (a UTF-8 byte order mark ignored, as Node ignores it), every declared range one of `N`, `N.x`, `N.M.P` with an optional `^`, `~`, `=` or `v`, and no prerelease, with N 4 or 5; a pnpm `catalog:` or `catalog:<name>` range reads the `express` entry of that catalog in the nearest `pnpm-workspace.yaml` at or above the manifest | Express major N for the paths written in that file | known gap: Express parses a path with the copy that created the app or router, which differs from the copy of the call's file only across a `package.json` boundary |
| EX5-W13 | major | no such `package.json`; a `package.json` on the way that exists but does not parse (the walk stops: it may declare the copy Node finds); any other range (`>=4`, `^4 \|\| ^5`, `*`, `latest`, `workspace:`, a URL, a prerelease such as `5.0.0-beta.3`, whose path-to-regexp differs, majors other than 4 and 5, ranges naming two majors, e.g. a peer `^4 \|\| ^5` next to a development `^5`: the consumer's copy may be either); a catalog without the entry | major unknown: EX5-W11 | - |
| EX5-W14 | default | every other form: a non-empty piece that does not start with `/` (no request path matches it), a literal byte no request path carries as written (controls, space, non-ASCII, `" # < > ^` and backtick), an empty segment (`//dbl`; Express 4 as written, so a prefix `/api//` too, since it makes only one trailing slash optional; Express 5 after dropping trailing slashes), escapes (`\`), parameters inside a segment, wildcards outside EX5-W4, EX5-W7 and EX5-W8, Express 4 regular-expression syntax (`? + ( ) [ ] { } \| ^ $` outside EX5-W3, so `/:id(\\d+)` too: a recall gap), Express 5 groups outside EX5-W9, readings that differ under an unknown major | no path through the piece, never a guessed literal; info `route_pattern_unmodelled` naming the piece; a route mounted only below such a prefix gets a `route_pattern_unmodelled` naming the prefix instead of `route_unmounted` | - |

Known gaps: EX5-W4 and EX5-W8 do not claim the zero-segment match, EX5-W3
not the path without a non-final optional parameter, EX5-W14 no Express 4
inline regular expression (`/:id(\\d+)`), EX5-W12 the creating copy's
major. Regular
expression paths and arrays stay `route_dynamic_pattern` (unchanged).
Express options that change matching (`strict`, `caseSensitive`,
`mergeParams`) are not read: the defaults are assumed, as before.
