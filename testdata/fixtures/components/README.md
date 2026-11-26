# components fixture (T026, ADR 0079)

Every row of the components claim contract
(`docs/contract/claims/architecture-graph.md`, rows CM, CK, CV, CD) and the
link and annotation rows the stored forms reach. Goldens:
`testdata/fixtures/components.golden.json` (package nodes and
`component_manifest_unread` diagnostics of the indexed fixture) and
`internal/server/testdata/architecture-components*.golden.json`.

Tests copy this directory into a temporary repository, renaming
`_graphvein.yaml` to `graphvein.yaml` (it hides `internal-tools/`, row
CV4). There are no ignore-file mappings. Route URLs use `/t026` so the
repository's self-index links nothing outside this fixture.

| directory | rows |
|---|---|
| `.` | `package.json` with `workspaces` and no `name`: CM-W1; `README.md`, `scripts/` and `nosetup/` fall to the root component (CM-R1) |
| `web` | CM-M1, client (CK3): axios calls linked to `api` (CE1), an https origin (AT3), unmatched and dynamic calls (ghost); imports `shared` (CE4) and axios (CE6) |
| `api` | CM-M1 with `pg` (AE3 evidence), Express server (CK1): route middleware (AA1), OpenAPI security (AA2, CE5) |
| `shared` | CM-M1 library: folded (CKD, CV3) |
| `docs-site` | `@docusaurus/core`: docs site (CK6), hidden (CV2) |
| `tools/cli` | npm `bin`: binary (CK5) |
| `crates` | Cargo virtual workspace: CM-W1 |
| `crates/core` | CM-M2 with a multi-line inline table, `[lib]`: library |
| `crates/app` | `Cargo.toml` and `package.json` in one directory (one component), `src/main.rs` (CK5), sqlx Postgres migrations (CK4, CE3, AE1), `_tauri.conf.json` restored as `tauri.conf.json` (CD2; inert in the repository so the self-index gains no Tauri app site) |
| `py` | CM-M3 with `[project.scripts]` (CK5); `py/pkg/setup.py` is enclosed (CM-M5, not a package) |
| `legacy` | `setup.py` calling setuptools' `setup(name=...)`: CM-M5 |
| `legacy2` | `setup.cfg` `[metadata] name`: CM-M5 |
| `nosetup` | `setup.py` without a setup call: not a package |
| `tooling` | `pyproject.toml` with tool configuration only and `package.json` without a name or workspaces: not a package |
| `uvws` | uv workspace root without `[project]`: CM-W1 |
| `gomod` | CM-M4, `package main` with `func main` (CK5) |
| `broken` | invalid `package.json`: CM-U1, unread component of unknown kind |
| `badgo` | `go.mod` without a module directive: CM-U1 |
| `e2e` | every file but its manifest is test scope: hidden (CV1) |
| `internal-tools` | hidden by `architecture.hide` (CV4) |
