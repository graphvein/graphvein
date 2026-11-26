# auth-heyapi fixture

Row DA-H1 of `docs/contract/claims/architecture-graph.md` (T026,
`docs/extract/auth.md`). `openapi.json` declares five security schemes and
operations with the global scheme, two alternatives, an OAuth2 password
flow, HTTP basic and `security: []`. `client/sdk.gen.ts`,
`client/client.gen.ts`, `client/index.ts` and `client/types.gen.ts` are the
unmodified output of `@hey-api/openapi-ts` 0.99.0 (MIT) for
`openapi-ts.config.ts`; `client/client/index.ts` is a hand-written stand-in
for the generator's bundled client (its bodies are not read).
`scripts/oracle/heyapi_security.sh` regenerates the client from the spec
and checks that the committed files are the generator's output and that
the stored `security` attributes equal the generated arrays.

`app.ts` adds hand-written calls whose `security` is not a literal array
or has a computed key (unknown), and the `health` operation has no
`security` property (DAD: no attribute).

No ignore files or renamed ignored paths are needed.
