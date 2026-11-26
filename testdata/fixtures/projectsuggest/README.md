# Project suggestion fixtures (T022 B6)

Hand-written projects for `graphvein project suggest`
(`internal/projectsuggest`, docs/cli/project.md "Suggestions"). Each folder
holds a `graphvein-project.yaml` and its member checkouts; the golden is
`../projectsuggest.<case>.golden.json`. No external source; nothing is run.

- `env/`: env file addresses and ports (rows SG-E1, SG-E2): a match, an
  ambiguous port, the member's own port, a public host, refused URLs
  (credentials, query, interpolation), an invalid port.
- `compose/`: a project-folder Compose file building every member, plus a
  member's own Compose file (SG-C1 to SG-C3, SG-M1, SG-M2): service-name
  and loopback addresses, a provider scope, image-only, `extends`, `<<`,
  interpolated and range ports, a missing build context.
- `devserver/`: Vite (SG-V1), webpack object and array forms (SG-W1,
  SG-W2), Create React App proxy (SG-R1, already declared), a computed Vite
  config and a `proxy` without react-scripts (SG-U).
- `define/`: webpack `DefinePlugin` in a config computed in
  code and Vite `define` (SG-D1, SG-D2; computed values, raw code, a
  config without dotenv, an env file in another folder), service-name
  addresses outside Compose matched through the member's own Compose file
  (SG-M4: built from another member, an image, the member itself, a
  missing container port), and the stored-call check (SG-M5: a backend's
  CORS origin, unused keys, calls through a bare constant that the clients
  extractor records as dynamic).

Every member is indexed by the test before `Suggest` runs (the stored
calls decide SG-M5), so the consumers hold small `src/api.ts` files; their
URLs use `/b6b` so the repository's own self-index links no other fixture.

Inert name mapping: every `_env*` file is restored to `.env*` in the
temporary copy the tests make (env files are often excluded by global
ignore rules and secret scanners; none here holds a real secret).
