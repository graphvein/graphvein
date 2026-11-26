# Launch material for the public repository

Files used when the public repository is created from a snapshot of this
tree (ADR 0080).

| File | Purpose |
|---|---|
| `AGENTS.md` | The public instructions for contributors' coding agents. `scripts/public-snapshot.sh` moves it to the root of the snapshot, in place of the internal one. |
| `cla/individual.md`, `cla/corporate.md`, `cla/privacy.md` | The contributor licence agreements and the CLA privacy notice, version 1.0, in force since 2026-11-26. |
| `ci-plan.md` | The plan for CI in the public repository. |
| `launch-file-list.txt` | The exact file list of the snapshot (private-side record; not part of the snapshot). |

`launch-file-list.txt` changes with every added or removed file, so it is
not kept current by a test. Regenerate it from a clean, committed tree just
before the launch snapshot and commit the result:

```bash
bash scripts/public-snapshot.sh "$(mktemp -d)/public" docs/public/launch-file-list.txt
```

The snapshot itself is checked by `scripts/check-public-tree.sh`, which
`TestPublicSnapshot` runs on every `go test ./...`.
