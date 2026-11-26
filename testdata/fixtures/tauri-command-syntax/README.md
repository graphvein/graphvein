# Tauri command syntax fixture

A source-only Cargo workspace (no downloaded dependencies, no `target/`) for
the Tauri syntax cache (T015c1, ADR 0037). The golden
`../tauri-command-syntax.golden.json` projects only the cached Tauri payload of
each Rust file; files without Tauri syntax have no payload and are absent.

- `app/src/commands/` holds command attribute forms: `#[tauri::command]`,
  `#[::tauri::command]`, bare `#[command]` through a plain use (plain.rs), a
  multi-line grouped use (grouped.rs) and an alias (aliased.rs, `#[cmd]`),
  `cfg_attr`, an extra `#[cfg]`, commands in an inline module, a nested fn and
  an impl member. arguments.rs covers rename, rename_all, root, async, a raw
  string, a non-literal rename and a bare path argument (both invalid), and a
  raw identifier `r#type`. mod.rs also has a multi-line cfg_attr predicate
  (recorded with LF), a nested cfg_attr, a function with both
  `#[poise::command]` and `#[tauri::command]` (two attributes) and a command
  in a `#[cfg(test)]` module (Cfg false: a documented gap). A later pass added
  `#[cfg_attr(.., cfg(..))]` at one and two nesting levels (Cfg true) and a
  cfg_attr that applies no cfg (Cfg false).
- `app/src/negatives.rs`: clap `#[command(...)]` on a struct and a field (no
  record), `#[poise::command]` on a fn (captured; T015c2 rejects it by
  resolution), and a generate_handler! and a command function inside
  macro_rules! bodies (not captured).
- `app/src/chains.rs`: a direct chain, two invoke_handler calls on one chain,
  the kftray rebinding shape with a cfg-gated if/else, `let mut` with
  assignments and a match value with a branch that has no tail.
- `app/src/shapes.rs` (added later): chains through parentheses, `?` and
  `.await`; chain values used as a tail, a return value, an argument and a
  reference; and flow roots shadowed by `if let`, closure, match arm, `for`,
  destructuring-let and function parameter binders, plus a closure parameter
  that does not enclose the root (no shadow).
- `app/src/rebinding.rs` (added later): field and deref assignment targets
  (Use other), later `invoke_handler` calls with a let-bound handler and a
  closure (Uncaptured, recorded in the flow's Calls), a match guard pattern and
  a `macro_rules!` rebinding (Shadows), and cfg-gated blocks and statements
  with nested bindings (Cfg inherited, Conditional).
- `app/src/handlers.rs`: an empty handler, a malformed token tree (missing
  comma) and a let-bound handler.
- `app/src/plugin.rs`: `Builder::<R>::new("my-plugin")` with
  `#![plugin(my_plugin)]` and `tauri::plugin::Builder::new("x")`.
- `app/build.rs`: `tauri_build::try_build(Attributes::new().app_manifest(..))`,
  which stays in the existing Rust calls only.

The index test also indexes a CRLF copy and requires identical payloads
(the scanner normalizes line endings before extraction; the extractor also
normalizes predicates and string values for direct callers).
There are no ignored directory names or live ignore files, and no inert-name
mapping is needed.
