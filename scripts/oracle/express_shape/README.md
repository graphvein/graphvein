# Express and Next.js Pages Router API shape oracle (T029 SH-E)

Checks the route shapes Graphvein stores for the fixture
`testdata/fixtures/express-shape` (rows SH-E1..SH-E36 of
[the Express shape claim contract](../../../docs/contract/claims/route-shape-express.md))
against the real frameworks and libraries. Opt-in, never run by CI.

```bash
bash scripts/oracle/express_shape/express_shape.sh --scratch "$SCRATCH"
# or against a snapshot of the fixture:
graphvein index <copy of the fixture> --emit snap.ndjson
bash scripts/oracle/express_shape/express_shape.sh --scratch "$SCRATCH" --snapshot snap.ndjson
```

It writes only under `--scratch` (`result.json`: counts, disagreements,
notes) and exits 1 when anything disagrees. It needs `node` and `npm` on
`PATH` and network access to the npm registry for the first install.

## What it compares

`oracle.cjs`, with the requests and schemas listed in `cases.json`:

1. **Served apps.** The fixture compiled with the pinned TypeScript and
   served on 127.0.0.1: `src/` by Express 4 (`src/validated.ts` with the
   OpenAPI validator replaced by a pass-through, since it only rejects),
   `v5/src/` by Express 5, and the `web/pages/api` files through
   Next.js's `apiResolver`. Every claimed status (kind `read_in_handler`)
   must be observed on some request of its route (`cases.json`
   `unobservable` lists the ones no request can reach, with the reason);
   every observed response at a claimed status must have the claimed
   content type and, where claimed, an empty body. For each request marked
   `valid` (it must be accepted) and each `fact` schema of its route, the
   oracle derives requests that must be **refused** (a required field
   missing; a typed field of another kind, including the strings Joi or
   zod would convert, such as `"5"` for a number and `"true"` for a
   boolean; an enum value in another case; `null` for a field that is not
   nullable; an extra key where extra keys are forbidden; no body where a
   top-level field is required; for query items, `name[x]=1` for a string
   under Express 4 and `"5"` for a number) and requests that must be
   **accepted** (an optional field missing, `null` for a nullable field,
   an extra key where extra keys are allowed). Refused means a status of
   400 or more, or no answer within 1.5 s (an async handler that threw
   under Express 4 never answers). `expectBody` checks one answer exactly
   (the `req.headers['X-Token']` probe).
2. **Schema libraries.** zod's `z.toJSONSchema(S, { io: "input" })` and
   Joi's `describe()` of the listed fixture schemas against the stored
   types, nullability, enum values, extra keys and key presence. A Joi
   `number()` or `boolean()` without convert off (`strict()` on the key or
   above) and a `valid()` with `insensitive()` must not be claimed typed.
3. **TypeScript.** The checker (`--strict`) for every `intended` and
   `returned` model on a route of `src/`: property names, whether a key may
   be missing (`?` or a type including `undefined`), types and nullability,
   and that the claimed site has the claimed type.

## Pinned packages and licences

`tools/`, `express5/` and `next/` hold a `package.json` with exact versions
and its `package-lock.json`; the script installs each with
`npm ci --ignore-scripts --omit=optional` into the scratch directory.
Nothing is installed into the repository.

| Set | Packages (direct) | Licences of the installed tree |
|---|---|---|
| `tools` | express 4.21.2, celebrate 15.0.3, joi 17.13.3, zod 4.1.12, cookie-parser 1.4.7, express-validator 7.2.1, lodash 4.17.21, typescript 5.6.3, @types/express 4.17.21, @types/cookie-parser 1.4.7 | MIT, ISC, BSD-3-Clause (joi and its @hapi/@sideway dependencies), Apache-2.0 (typescript) |
| `express5` | express 5.1.0, joi 17.13.3, zod 4.1.12 | MIT, ISC, BSD-3-Clause |
| `next` | next 15.5.4, react 19.1.1, react-dom 19.1.1 | MIT, ISC, Apache-2.0, BSD-3-Clause, 0BSD; caniuse-lite (data) CC-BY-4.0. next's optional image packages (sharp, LGPL-3.0-or-later parts) and compiler binaries are not installed (`--omit=optional`): `apiResolver` needs neither. |
