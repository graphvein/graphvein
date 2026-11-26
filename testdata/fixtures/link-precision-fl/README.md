# Link precision fixture (T034)

Rows LF-1 to LF-3 of the client matcher (`docs/extract/clients.md`, "Link
precision rules"). `TestLinkPrecisionRules` (`internal/index`) checks every
client call of the fixture against its row, the golden of the client calls,
`calls_route` edges and the `calls` edges of `shadow.tsx` and
`invisible.js`, and that the rows
in the document are the rows the fixture covers.

| Site | Call | Row | Expected (extractors-57 before) |
|---|---|---|---|
| `calls.ts:4` | `fetch(RELEASES)`, a module const absolute URL to GitHub | LF-1 | unmatched (exact 0.7 to `GET *path`) |
| `calls.ts:5` | ``axios.get(`https://plex.tv/api/v2/pins/${id}`)`` | LF-1 | unmatched (exact 0.7 to `GET *path`) |
| `calls.ts:6` | `POST 'https://cdn.example.com/t034-feed.json'` | LF-1 | unmatched (exact 0.7 to `ANY *path`) |
| `calls.ts:9` | URL object whose host is assigned (origin unresolved) | LF-1 | unmatched (exact 0.7 to `GET *path`) |
| `calls.ts:10` | literal origin, path with literal segments | LF-U | exact 0.7 to `GET /t034-items/:id` (unchanged) |
| `calls.ts:11` | `fetch('/t034-pages/about')`, no origin | LF-U | exact 0.9 to `GET *path` (unchanged) |
| `calls.ts:14` | ``/t034-api/t034-genres/${type}`` | LF-2 | ambiguous, 3 candidates (exact 0.9 to `GET *path`) |
| `calls.ts:15` | ``/t034-api/${type}/${id}`` | LF-2 | ambiguous, 5 candidates (exact 0.9 to `GET *path`) |
| `calls.ts:16` | `'/t034-api/t034-genres/movie'` | LF-U | exact 0.9 to the literal route (unchanged) |
| `calls.ts:17` | ``/t034-api/t034-items/${id}`` beside `/t034-items/featured` | LF-U | exact 0.9 to `/t034-items/:id` (unchanged: the winner is no catch-all) |
| `calls.ts:18` | ``/t034-pages/${slug}``, no literal route overlaps | LF-U | exact 0.9 to `GET *path` (unchanged) |
| `shadow.tsx:4` | the DELETE wrapper `remove`, declared inside an anonymous function | LF-U | wrapper (unchanged) |
| `shadow.tsx:6` | `remove('7')` where the wrapper's binding is visible | LF-U | exact 0.9 to `DELETE /t034-items/:id` (unchanged) |
| `shadow.tsx:13` | `remove(...)` of `const [open, remove] = useState(false)` | LF-3 | no client call, no `calls` edge (DELETE link) |
| `shadow.tsx:18` | `remove('3')` of the shorthand parameter `{ remove }` | LF-3 | no client call, no `calls` edge (DELETE link) |
| `shadow.tsx:24` | `remove('5')` of `const { remove } = rest` | LF-3 | no client call, no `calls` edge (DELETE link) |
| `shadow.tsx:30` | `read()` in method `Color.parse`, while another anonymous scope has a function `Color` destructuring `read` | LF-U | `calls` edge to `Color.parse.read` kept (a pattern shadows only in its own function) |
| `shadow.tsx:49` | `drop(state)` of `const [state, drop] = useDrop({ drop(item) {...} })` | LF-3 | no `calls` edge to the object method `Row.drop` (a method is a property, not a nearer binding) |
| `invisible.js:3` | the DELETE wrapper `purge`, declared inside an anonymous function | LF-U | wrapper (unchanged) |
| `invisible.js:4` | `purge('6')` in `Row`, declared beside it | LF-U | exact 0.9 to `DELETE /t034-items/:id` (unchanged) |
| `spec/openapi.yaml` | operations `/repos/{owner}/{repo}` and `/user` of a spec whose `servers` is `https://api.github.com` | LF-1 | no `documents_route` (0.7 to `GET *path` before) |
| `spec/openapi.yaml` | operation `/t034-api/t034-genres/movie`, same spec | LF-U | `documents_route` 0.7 to the literal route (unchanged) |
| `invisible.js:8` | `purge('8')` in `Footer`, where no `purge` is in scope | LF-3 | no client call (DELETE link); the `calls` edge `Footer` -> `purge` stays, a known gap of the `calls` model, which does not know that a function declared inside an anonymous function is invisible outside it |

Inert name: the catch-all routes (`GET *path`, `ANY *path`, Express 5) are
stored as `fallback.ts.inert`, and the test restores it to `fallback.ts`. As a
live file in Graphvein's self-index they would become candidates of the
calls and OpenAPI operations of every other fixture, as in
`client-exact-literal-precision`. No ignore-file or directory mapping is
needed.
