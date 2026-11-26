# Committed bundler output fixture (ADR 0085)

Committed bundler output is still indexed, but its client calls are not
linked to routes (`docs/extract/clients.md`, "Committed bundler output",
markers GB-1 to GB-3 and the default GB-U). `TestGeneratedBundleOutput`
(`internal/index`) checks each client call below, one
`client_calls_generated_output` diagnostic per bundle file, the golden of
the client calls, `calls_route` edges and those diagnostics, and that
`clients.link_generated_output: true` in `graphvein.yaml` (written by the
test into its copy) links every call.

| File | Marker | Expected |
|---|---|---|
| `src/api.ts` | none | linked to `GET /t035-gb/items` |
| `static/web/_next/static/chunks/0t035gbxq9z1.js` | GB-1 (Next.js `_next/static/`, minified) | status `generated`, `generated_output: GB-1`, no edge |
| `public/assets/index-Bx7k_2Qa.js` | GB-2 (Vite `assets/<name>-<hash>.js`, minified) | status `generated`, GB-2, no edge |
| `www/static/js/main.1a2b3c4d.chunk.js` | GB-3 (webpack `static/js/<name>.<hash>.chunk.js`, minified) | status `generated`, GB-3, no edge |
| `static/web/_next/static/chunks/t035-loader.js` | GB-U: the Next.js path, but no minified line | linked |
| `src/assets/helper-abcdefgh.js` | GB-U: a hashed-looking name outside every marker layout (not minified either) | linked |
| `src/assets/user-settings.js` | GB-U: `settings` has no digit or capital, so it is no Vite hash; one long `data:` URI line | linked |
| `src/assets/api-requests.mjs` | GB-U: as above (`requests`) | linked |
| `src/assets/icon-Ab12Cd34.js` | GB-U: a Vite-like hash, but the only long line is a `data:` URI (not dense code), so the file is not minified | linked |

Minified means: lines of at least 1,000 bytes with at least one `;` or
`}` per 100 bytes hold at least half of the file's bytes. The minified
files are single long lines of generated filler statements and one
`fetch`, written for this fixture; they contain no third-party code. No
ignore file or ignored directory name (`dist`, `build`) is used: the
webpack layout sits under `www/`.
