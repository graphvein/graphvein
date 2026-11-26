# Editor logos

**Not covered by the Apache License.** The logo files in this folder (and
their copies in the built UI under `internal/server/ui/static/`) are
trademarks of their owners and stay under their owners' terms, listed in
the table below; the project's Apache License 2.0 does not apply to them
(see `NOTICE`). A fork must check these conditions itself or remove the
files.

Official editor logos shown by the editor picker and the "Open in" buttons
(T016k, T016k2, ADR 0050), under the conditions of
the T016k task record (internal). Every other editor is drawn as a
monogram by `src/components/EditorMark.tsx`.

| File | Source | Changes | Conditions |
|---|---|---|---|
| `vscode.svg` | `https://code.visualstudio.com/assets/branding/visual-studio-code-icons.zip` (version 2021-06-21), `visual-studio-code-icons/vscode.svg` | none | blue icon only, unmodified, clear space of 25% of the icon, nothing added to it; <https://code.visualstudio.com/brand> |
| `intellij-idea.svg` | `https://github.com/JetBrains/logos`, `web/intellij-idea/intellij-idea.svg` (the brand page host `resources.jetbrains.com` was blocked when it was added) | none | Licence: <https://github.com/JetBrains/logos/blob/master/LICENSE.txt> (the logos are not part of the Apache 2.0 licence; use only to point to JetBrains, linked to jetbrains.com; secondary to Graphvein's branding; unaltered, original colours; a product sold commercially must ask JetBrains for explicit permission). Brand page <https://www.jetbrains.com/company/brand/>: unaltered except size, at least 24 px, not combined with other symbols, clear space of half the black square's width (1/8 of the icon width past its edge); attribution and link on the licences page |
| `zed-logomark-black.png`, `zed-logomark-white.png` | <https://zed.dev/brand> "Logomark" downloads `logo_wordmark_black_bigger.png`, `logo_wordmark_white_bigger.png` | scaled proportionally from 1012 to 128 px | brand blue, full white or full black only, no distortion; trademark line "The Zed name and logos are trademarks of Zed Industries, Inc." (wording from <https://zed.dev/brand>, "Trademark") on the licences page |
| `cursor-cube-2d-light.svg`, `cursor-cube-2d-dark.svg` | <https://cursor.com/brand> "Download brand assets" (`https://ptht05hbb1ssoooe.public.blob.vercel-storage.com/assets/brand/cursor-brand-assets.zip`, SHA-256 `97488a7751914e60f9ff532bc33810cdeaebdddc017548abe6ca2bc29bbc3928`, fetched 2026-11-16), `General Logos/Cube/SVG/CUBE_2D_LIGHT.svg` and `CUBE_2D_DARK.svg` (T016k2) | none (renamed only) | The brand page says only "Resources to represent Cursor consistently and accurately." and "Refer to us as Cursor. Not Cursor AI or Cursor Code."; it states no clear space, minimum size, licence or prohibition, and the package holds no guideline or licence file. 2D is the page's default ("2.5D (for larger applications)"); LIGHT (dark cube) on the light theme, DARK (light cube) on the dark theme; drawn at its own aspect ratio, no implied affiliation |

`src/components/EditorMark.tsx` places the source marker and caveat badge
beside a logo's tile, outside the logo and its clear space (`CLEAR_SPACE`),
never on it; only monograms carry them on their corners.

`web/scripts/third-party-licenses.mjs` records each file's SHA-256 and its
attribution text, writes them to `/third-party-licenses.txt` (the only place
the attribution appears) and fails the build when a file here is not listed
or its bytes changed. A new or changed logo needs a new review against the
brand's guidelines. Bundling in a distributed product is not yet confirmed
with Microsoft or JetBrains (ADR 0050, known gaps).
