# Link precision fixture: catch-alls of every framework (T034)

Row LF-2 of the client matcher (`docs/extract/clients.md`, "Link precision
rules") on the final catch-all of each framework other than Express, and
FastAPI's path convertor, which the routes extractor stores as the final
catch-all `{*name}` like the others (`docs/extract/routes.md`).
`TestLinkPrecisionCatchAlls` (`internal/index`) checks every client call of
`web/lib/calls.ts` against the table below, the stored FastAPI template,
the golden of the client calls, `calls_route` edges and route paths, and
that every row tag a branch reports is a row.

| Site | Call | Routes | Expected |
|---|---|---|---|
| `web/lib/calls.ts:2` | ``/t035-next/items/${id}`` | Next.js `items/featured`, `items/[...rest]` | LF-2: ambiguous, 2 candidates |
| `web/lib/calls.ts:3` | ``/t035-next/docs/${slug}`` | Next.js `docs/intro`, `docs/[[...slug]]` | LF-2: ambiguous, 2 candidates |
| `web/lib/calls.ts:4` | ``/t035-go/files/${name}`` | chi `/t035-go/files/readme`, `/t035-go/files/*` | LF-2: ambiguous, 2 candidates |
| `web/lib/calls.ts:5` | ``/t035-axum/assets/${file}`` | Axum `/t035-axum/assets/logo`, `/t035-axum/assets/{*rest}` | LF-2: ambiguous, 2 candidates |
| `web/lib/calls.ts:6` | ``/t035-py/api/${part}`` | FastAPI `health`, `version`, `{_path:path}` | LF-2: ambiguous, 3 candidates (extractors-57 and the first T034 fix: exact 0.9 to `{_path:path}`, stored as the one-segment `/t035-py/api/{_path}`) |
| `web/lib/calls.ts:7` | `'/t035-py/api/v1/flows'` | FastAPI `{_path:path}` | LF-U: exact 0.9 to the catch-all (before: unmatched, the one-segment form) |
| `web/lib/calls.ts:8` | ``https://cdn.example.org/t035-py/api/${part}/meta`` | FastAPI `{_path:path}` | LF-U: exact 0.7 with `origin_assumptions` "origin https://cdn.example.org assumed to be this repository's server" |
| `web/lib/calls.ts:9` | `'/t035-next/items/featured'` | Next.js `items/featured` | LF-U: exact 0.9 to the literal route |

Each framework uses its own path prefix (`/t035-next`, `/t035-go`,
`/t035-axum`, `/t035-py`), so the routes of one never tie with another's.
No ignore file or ignored directory name is needed.
