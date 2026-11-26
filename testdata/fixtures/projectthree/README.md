# Three-repository project fixture (T022)

A frontend with two backends that serve the same paths, shaped like the
`react + express + axum-ddd` corpus project. Hand-written; imports are
syntax only and nothing needs installing. No file uses an inert name.

- `web/`: fetch calls through an `import.meta.env.VITE_API_URL` base:
  `GET /t022r/user` and `GET /t022r/articles` (both backends serve them, so
  the link pass leaves them ambiguous, PL-R11) and `POST /t022r/login`
  (only `express` serves it, so it links, PL-L1). `loadUser` is called by
  the `UserPage` UI function.
- `express/`: Express routes `GET /t022r/user`, `GET /t022r/articles`,
  `POST /t022r/login` and `GET /t022r/health` (no caller anywhere).
- `axum/`: an axum router with `GET /t022r/user` and
  `GET /t022r/articles`.

Every path starts with `/t022r` so the members' own indexes link nothing
in other fixtures. Tests copy the folder and write the manifest
(`web`, `express`, `axum`, no declarations).
