# Test scope beside an unknown production prefix (T057)

Hand-written Go (gin v1) and TypeScript syntax; no external corpus source,
nothing is installed or executed. No inert-name mapping is needed.

The production engine is passed on after mounting (`SetRouter` hands a
dispatcher to `NoRoute`), so no production path below it is proven: the
routes store `unresolved_prefix_paths` entries. `server/router/api_test.go`
builds the same router on a fresh engine, so the routes also have
test-scope full paths.

- `web/src/api.ts:2`, `:6`: only the test-composed `/api/about` and
  `/api/token/` match, but the production entries `…/api/about` and
  `…/api/token/` may serve the calls: `unmatched`, not `test_only`.
- `web/src/api.ts:10`: `/api/debug` is registered only in the test file:
  `test_only` stays.
- `web/src/api.ts:14`: `/stats` matches the test mount of
  `stats.Register`; its production path `…/admin/stats` cannot be `/stats`:
  `test_only` stays.
- `web/src/api.ts:18`: `/admin/stats` links through the unknown-prefix
  tier (row UP-19), as before.
- `web/src/api.test.ts:2`: a test call ignores scope and links the
  test-composed path.
