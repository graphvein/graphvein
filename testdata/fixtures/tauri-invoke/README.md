# Tauri invoke linking fixture

Source-only Rust crates and TypeScript files (no downloaded dependencies, no
`target/`, no `node_modules/`) for Tauri invoke linking (T015c2, ADR 0038).
App scope is decided per repository (one proven app registry, no unproven
handler), so the tests index separate scenario repositories, each a copy of
this directory that keeps only some top-level directories. The golden
`../tauri-invoke.golden.json` holds the IPC projection (ipc_command and
ipc_call nodes, exposes_command, invokes_command and their contains edges,
tauri_* diagnostics) of the `linked`, `registries`, `test-only`, `unsure`,
`pairs` and `second-bin` scenarios.

| scenario | directories | covers |
|---|---|---|
| linked | `app`, `frontend` | links and every invoke form |
| registries | `registries` | registries the proof rejects (one reason each) |
| test-only | `test-only` | a test-scope app registry: test_only, shadow_possible, unknown_command |
| ambiguous | `app`, `frontend`, `second-app` | two proven apps: ambiguous |
| acl | `app`, `frontend`, with `app/_permissions` renamed to `app/permissions` | acl_possible |
| blocked | `app`, `frontend`, `registries` | an unproven handler blocks app matches |
| shadowed | `shadowed`, with `_package.json` restored to `package.json` | a scanned package named `@tauri-apps/api` (no ipc_call) |
| other-app | `app`, `frontend`, `other-app` | a closure invoke handler: registry_unproven |
| bare-app | `app`, `frontend`, `bare-app` | another app's aliased Builder root, generate_context! and config directory: ambiguous |
| mock-tests | `app`, `frontend`, `mock-tests` | cfg(test) and mock_builder registries neither prove nor block |
| unsure | `unsure` | probes q1/q2: the wrapper write check fails closed (only u2 is a wrapper) |
| pairs | `pairs` | probe e1: `cfg(P)` / `cfg(not(P))` arms cover every build |
| second-bin | `second-bin` | probes b0-bl: second apps in `src/bin/` sharing the library's context: ambiguous |

- `app/src/main.rs`: the kftray registry shape (`let builder` rebound through
  a cfg-gated if/else, `let app = app.…`, consumed by `build`), entries for
  `#[tauri::command]` (commands.rs), bare `#[command]` through
  `use tauri::command` (bare.rs), an aliased use (`#[cmd]`), `cfg_attr`, a
  last-wins rename, a cfg-gated entry, a duplicate entry, a command in an
  inline `#[cfg(test)] mod` (test-only), and a cfg-gated `legacy_status`
  renamed `status` before `status` (two arms, both linked). `main` carries
  `cfg_attr(mobile, tauri::mobile_entry_point)` and `allow`; `app` has a
  `tauri.conf.json` with a `tauri.linux.conf.json` overlay. `app/src/plugins.rs`: a plugin registry named by
  its builder and one with `#![plugin(a_b)]` holding a non-literal rename, a
  function with two Tauri attributes and the poise and clap-like negatives
  (negatives.rs).
- `frontend/src`: named, aliased, namespace, `core.invoke`, type-only and
  `@tauri-apps/api/tauri` calls; an imported wrapper (generic and await
  forms, lib/ipc.ts) and a second-level wrapper (lib/relay.ts); a local
  wrapper with three colliding intents and a shadowed local wrapper
  (local.ts); a conditional name with two targets, a dynamic name, an
  unregistered name, `plugin:fs|read_file`, `plugin:x`, a shadowed imported
  wrapper; both global forms (global.js); test-scope calls (app.test.ts,
  one to the cfg(test) command); callables that write their parameter before
  forwarding it (not wrappers) and one that only reads it (writes.ts).
- `registries/src/main.rs`: a foreign `generate_handler`, a malformed token
  tree, path-form and let-bound handlers, `?` in the chain, a later
  invoke_handler, an unconsumed chain, an extension-trait method, turbofish
  and helper roots, an if without else, if-let and destructuring shadows, a
  body `macro_rules!`, an opaque macro, an attribute macro on the function,
  a mock_builder root and a plugin name mismatch.
- `second-app`: a direct `Builder::default()…run(..)` registry in a
  `#[tokio::main]` function.
- `other-app`: an app whose invoke handler is a closure.
- `bare-app`: an app without an invoke handler (`use tauri::Builder as App`)
  with its own `tauri.conf.json`.
- `mock-tests`: a registry in a `#[cfg(test)]` module and two
  `tauri::test::mock_builder()` registries in `tests/`.
- `unsure`: a proven app and callables whose parameter write hides behind a
  regex after `)` or `}`, a division after `++`, an identifier escape or JSX
  text (`web/view.tsx`); u2 has an apostrophe inside a template and no write.
- `pairs`: `#[cfg(feature = "a")] g1, #[cfg(not(feature = "a"))] g2, greet`,
  all named `greet`.
- `second-bin`: one crate (`probe_app`) whose library re-exports
  `tauri::Builder as AppBuilder` and holds the only `generate_context!`;
  `src/main.rs` is the proven app and each `src/bin/<probe>.rs` builds
  another app in one form (plain, type alias, typed `Default::default()`,
  use rename, turbofish, qualified path, re-export, function value,
  `macro_rules!`, glob, renamed turbofish).
- `test-only/tests/ipc.rs`: `Builder::new()` in a test file, with an
  unresolved entry before `pong`; `test-only/web` calls it.

Inert names: `app/_permissions/` is an ordinary directory name chosen so that
the default scenarios have no `permissions/` directory; only the acl scenario
renames it to `app/permissions/`. `shadowed/packages/tauri-api/_package.json`
is the shadowing package's `package.json`; only the shadowed scenario restores
it, so the fixture never shadows `@tauri-apps/api` for the repository that
holds it (the Graphvein self-index). There are no live ignore files and no
ignored directory names (the shadowing package lives under `packages/`, not
`node_modules/` or `vendor/`). The index test also indexes a CRLF copy of the
linked scenario and requires identical records, and checks that editing only
a TypeScript file relinks without reparsing Rust.

Each scenario with TypeScript sources has a `package.json` declaring
`@tauri-apps/api`: an import is an npm package only when a `package.json`
at or above the file declares it (row TS-I5).
