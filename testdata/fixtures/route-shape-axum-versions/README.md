# route-shape-axum-versions

Golden-only fixture for the Axum route shape forms whose answer depends
on the axum version (claim contract
`docs/contract/claims/route-shape-axum.md`, forms `version:*`). Every
crate has the same handlers: a `String` and an `Html` response, `Form` on
`head` and on `any`, and a `HeaderMap` before a `Json` extractor.

| Crate | axum version as read |
|---|---|
| `workspace/inherit` | 0.7.9: the workspace `Cargo.lock` (the manifest inherits `axum = "0.7"` from the workspace) |
| `workspace/locked-old` | 0.3.4: the `Cargo.lock` wins over the manifest's `0.7` |
| `workspace/unlisted` | not proven: the workspace `Cargo.lock` does not lock it |
| `unlocked/any` | not proven: `>=0.3` (no lockfile) |

The crates are not built; the lockfile lists only the entries the reader
uses. No file needs an inert name.
