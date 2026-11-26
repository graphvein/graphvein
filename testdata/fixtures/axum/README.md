# Axum fixture

This source-only workspace needs no downloaded Rust dependencies to index.
Its two packages exercise the underscore library name for a hyphenated package
and a binary calling its own library. The compact golden is hand-authored from
these registrations, handler declarations and composition paths, never from
indexer output. Private const PATH does not gain a symbol node.

There are no ignored directory names or live ignore files, and no inert-name
mapping is needed. custom.rs is deliberately adjacent to routes.rs: an outlined
#[path] attribute is relative to the declaring file's directory. nested.rs is
under routes/ because ordinary child modules in routes.rs use that directory.

factory has nine registrations and nested has one. dead contributes repeated
GET / (@2), despite being unserved. The app mounts factory under /kit and,
conditionally, /copy and the local constant /local; its conditional merge adds
/inline. An explicit import of custom::h as a shadows handlers::*'s a. The dynamic method
router, formatted registration and formatted nest prefix produce diagnostics.
/lost has no path because its only consuming nest has an unresolved prefix.
LocalRouter, renamed to Router inside lookalike, must produce no route.
