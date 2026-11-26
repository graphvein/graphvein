# Client suffix precision fixture

T017a. Every call has an environment-shaped base (`process.env.API`), so the
base is `unresolved` and suffix matching is allowed. A suffix remainder must
match at least one literal call segment with a literal route segment.

| Call | Route remainder | Expected |
|---|---|---|
| `DELETE /nothing` | `ANY /files/*` leaves `{*}` only | unmatched |
| `GET /health` | `GET /users/:id` leaves `{id}` only | unmatched |
| `GET /orders/42` | `GET /api/orders/:id` leaves `orders/{id}` | suffix, 0.6 |
| `GET /assets/logo.png` | `ANY /cdn/assets/*` leaves `assets/{*}` | suffix, 0.6 |

No route matches any call exactly, so no exact match hides a suffix case.
All names are literal; no inert-to-live ignore or directory mapping is needed.
