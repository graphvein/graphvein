# Repository fixtures

The TypeScript fixture is copied into a temporary directory by both index and
store tests. Before scanning, they restore these names:

| Committed name | Temporary name |
| --- | --- |
| `typescript/_graphveinignore` | `.graphveinignore` |
| `typescript/_claude/` | `.claude/` |
| `typescript/nested/_gitignore` | `nested/.gitignore` |
| `typescript/nested/_skip.ts` | `nested/skip.ts` |

The last file is ignored by the restored nested Git rules. Never force-add it.
This README stays outside the copied repository to preserve the existing goldens.

## T008b blind-spot cases

Each language adds a separate blind caller file with an unresolved receiver
call matching the CLI target (Help/help/helper/greet) and a nonmatching
Unrelated/unrelated call. These files are deliberately outside the reached graph;
the stream/store goldens retain both sites, and CLI impact goldens retain only
the matching sites (including matches to other reached names already present).

## T008c web-root import

`typescript/src/asset.tsx` is a synthetic web-root SVG import and a small arrow
component. The SVG is deliberately absent: the linker emits a valid unresolved
`web:/assets/icon.svg` external node and a diagnostic without guessing bundler
configuration. No source was copied from the ignored corpus.
