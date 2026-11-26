# ADR 0091 - Project website: generator, hosting and checks

**Status:** Accepted (delegated review, 2026-11-25)

## Context

Graphvein's first preview launch needs a public website: a home page that
says what the tool does, with real screenshots, the measured numbers and
how they were measured, and the public documentation (guide, known gaps,
command line, evidence, claim contract tables, ADRs). The site states
facts about the product, so it is held to the product's own rule: no
confident wrong fact. A number on it must be the number on its evidence
page, a screenshot must show the current build, and nothing private may
leave the repository through it. The repository itself stays private
until the launch; only its public snapshot (ADR 0080) is published.

## Options considered

**Generator.**

1. A static site generator (Hugo, Astro, a docs theme). Fast to start,
   but a new toolchain, a theme to keep up and many dependencies, and the
   checks below would be plugins or scripts around it.
2. A hand-written page per section. No build, but the docs pages would
   drift from `docs/`, and the checks would have nothing to run on.
3. **A small Go program in its own module (`site/`)** with goldmark as
   its only dependency and `html/template`. One language the project
   already uses, the checks are ordinary code with tests, and the main
   module's dependencies do not change.

**Hosting.**

1. GitHub Pages: tied to Actions minutes and to a public repository.
2. Cloudflare Pages connected to this private repository through its Git
   integration: gives a third party read access to the private
   repository. Rejected.
3. Cloudflare Pages by Direct Upload of a built folder: no repository
   access, but a Direct Upload project can never switch to the Git
   integration later. Rejected.
4. **Cloudflare Pages with its Git integration on the public repository,
   created at the launch.** Cloudflare builds from the public tree, so it
   sees only what is public anyway; every branch gets a preview URL; no
   Actions workflow and no API token are needed.

**Analytics.** Cloudflare Web Analytics (a beacon script) or none.
**None**: the footer can then say "no cookies, no tracking, no
third-party scripts or fonts" without qualification.

## Decision

- **Generator.** `site/` is a Go module (`go 1.24`, no `toolchain`
  line, so Cloudflare's default Go 1.24.3 builds it without downloading
  another toolchain; checked by building a clean public snapshot with
  Go 1.24.3) depending only on github.com/yuin/goldmark (GFM
  tables, strikethrough, autolinks). `go run . -out dist` writes the site;
  it needs no Git history (a shallow clone is enough) and no network
  beyond the Go module proxy. It reads only public files: every read goes
  through a reader that refuses the paths of `scripts/public-exclude.txt`.
- **Content.** The home page is short and scannable: a headline, one
  line, three number cards, one line per section of the product, and
  short lists that link to the longer pages (`/status/`,
  `/how-graphvein-decides/`, `/how-it-is-verified/`) and the docs.
  `site/content/home.json` holds every sentence of these pages, with numbers only as `{id}` placeholders of the number registry
  `site/content/numbers.json`. The lead and the line under it live there
  once; the README carries the same text in a `<!-- site:lead -->` block.
  The README is the source of the full status paragraph and table
  (`site:status`, `site:status-table`), which the site's status page
  reads, so they are one copy; the home page shows a one-line summary.
  Screenshots, the terminal transcript and the social image are listed in
  `site/content/media.json`; the hero and the gallery are names in it, so
  swapping a screenshot is a data change.
- **Docs pages.** Rendered from `docs/guide`, `docs/known-gaps.md`,
  `docs/cli`, `docs/evidence`, `docs/contract/claims`, `docs/adr`,
  `docs/extract` and `docs/lang` (the status table links the last two).
  Links between published pages become site links (GitHub-style heading
  anchors); links to other repository files go to the public repository
  with the switch on and become plain text with it off; links into the
  app (`#/api`) become plain text. Raw HTML in the docs is shown as text.
- **One switch.** `site/site.json` `public_repository` (false until the
  launch). Off: no GitHub link, no star button, no request to GitHub, and pages are served with `X-Robots-Tag: noindex`. On: GitHub
  and issue links, "Report a problem with this page" (the
  `docs-problem.yml` form with the source path filled in), and the Star
  button, always shown as a plain link to the repository; only its count
  is fetched in the browser from GitHub's unauthenticated API and shown
  from 100 stars up, and on any failure the button stays without a count.
- **Screenshots in the feature rows.** A manifest entry may carry a crop
  box (source pixels); the build crops the PNG with the standard library
  and shows the crop, so the derivative cannot drift from its source and
  the current-build check still applies to the source. Each feature image
  links to the whole screenshot; the site's one first-party script
  (`site.js`) opens it in a modal dialog (Esc, the close button or a
  click outside closes it; focus returns to the link). Without the script
  the link opens the PNG.
- **Theme.** Pages follow the system theme; a header switch (System,
  Light, Dark) overrides it, remembered per browser in `localStorage`
  (every access in try/catch; without storage or script the page follows
  the system). `site.js` is loaded in `<head>` without `defer`, so a
  stored choice is applied before the body is drawn and the page does not
  flash in the wrong theme; the price is one small render-blocking,
  cacheable request on every page, the only way without an inline script
  (the content security policy forbids them). Screenshots show in the
  theme opposite to the page's (a dark screenshot on a light page, a
  light one on a dark page), so they read as pictures of the app: a
  light screenshot's manifest entry names its dark twin (`dark`), the
  build checks that both show the same view (viewport, scale, crop box),
  the page carries both and the style sheet shows one; the lightbox opens
  the variant that was clicked. A screenshot of a demo edit is marked
  `demo_edit` and its caption must say so.
- **Output.** Static files, `404.html` and `_headers`: a content security
  policy of `default-src 'self'`, `script-src 'self'` (the site's own
  scripts only), no inline scripts or styles, `connect-src` only
  `https://api.github.com` and only with the switch on, plus `nosniff`, `frame-ancestors 'none'`, a referrer policy, a
  permissions policy and HSTS. No analytics.
- **Checks that fail the build** (nothing is written):
  1. Numbers: each registry entry's quote is in its evidence file
     (whitespace and `**` normalised) and holds every digit the site shows
     for it; home.json has no digit outside a placeholder; the home,
     download, status and the two explanation pages show no number
     outside the registry (screenshot
     captions and the terminal transcript are excluded: they are not
     claims).
  2. Screenshots: every image served has a manifest entry; screenshots,
     transcripts and derived images carry the extractor version of
     `internal/index/index.go`; the social image records the lead it was
     drawn with and must match the current lead.
  3. Privacy: the build read no private path; no output file contains a
     private path or file name of the exclude list, the reference pattern
     of `scripts/check-public-tree.sh` (read from the script, so both use
     the same check), or words describing how the work behind the project
     is organised (the work words are looked for in prose only, not in
     code spans or code blocks, where a page may quote a UI label such as
     `Enter a value for this session`; paths and record names are looked
     for everywhere); with the switch off, no output links to the repository
     and no site file calls the GitHub API. A docs page with such words is
     withheld (`withheld` in `site/content/docs.json`) and the build fails
     when a withheld page becomes clean, so the list only shrinks.
  4. Links: every site link reaches a generated file and every fragment
     an element id.
  5. README: its lead block equals the home page's lead and line.
- **Tests.** `go test ./...` in `site/` builds a fixture repository and
  compares the output with a golden copy, runs each check against a
  fixture that breaks it, checks both switch states, and builds the real
  site with every check. In that real-site test a screenshot older than
  the extractor version is reported, not failed, so an extractor change
  does not have to retake screenshots; the deploy build refuses it.
  `scripts/test.sh` runs these tests, so CI does. `scripts/ci-changes.sh`
  runs a push to main that changes only `site/` (outside docs and
  Markdown) on the Linux job only.
- **Layout check.** `scripts/check-site.sh` (local, Playwright, not in CI)
  opens the home, a docs, an ADR and the download page at 1440, 900 and
  390 px in light and dark, and fails on horizontal overflow, when the
  primary call to action is below the fold at 1440 x 900, or when the
  hero screenshot is not in the theme opposite to the page's; at 1440 and
  390 it also opens the screenshot lightbox (same variant as the
  thumbnail; Esc closes it and focus returns) and uses the theme switch
  (the page and screenshots follow it, the choice is applied before the
  body after a reload, System restores the system theme).
- **Hosting.** At the launch, a Cloudflare Pages project `graphvein`
  connected to the public repository: framework preset None, root
  directory empty (the repository root), build command
  `cd site && go run . -out dist`, output directory `site/dist`,
  production branch `main`, no environment variables (Go 1.24.3 of the
  build image suffices; `GO_VERSION=1.24.3` pins it), build watch paths limited to what the
  generator reads (`site/*`, `docs/*`, `README.md`, `CHANGELOG.md`,
  `NOTICE`, `LICENSE`, `THIRD_PARTY_NOTICES`, `internal/index/index.go`,
  `scripts/public-exclude.txt`, `scripts/check-public-tree.sh`).

## Consequences

- The site cannot show a number its evidence page does not print, a
  screenshot of an older extractor, or a private path; the build fails
  instead and the last good deployment stays up.
- Every extractor version change makes the deploy build fail until the
  screenshots are retaken. That is the intended cost of current-build
  screenshots.
- Changing the lead means editing `site/content/home.json` and the
  README block, then regenerating the social image
  (`node site/tools/social.cjs`).
- goldmark (MIT) is used only at build time; nothing of it is served.

## Known gaps

- No docs page is withheld at the launch; the `withheld` list stays as
  the mechanism for a page whose text a later change makes private.
- The number check covers digits, not number words ("three").
- Whether Cloudflare's clone is shallow is not documented; the build does
  not depend on it.
- Release downloads and the changelog are placeholders until the first
  release; the changelog page renders `CHANGELOG.md` once it exists.

## Review notes (delegated review, 2026-11-25, T048)

- Gate: CI run 45709945797 (Linux, Postgres and Windows green; the
  postgres job 0 FAIL, 291 `/postgres` PASS, skip set identical to the
  previous gate). The site was built from a clean public snapshot with
  Go 1.24.3 (Cloudflare's default) and checked at 1440, 900 and 390 px in
  both themes.
- The fixture golden records PNG outputs by size and a hash of the decoded
  pixels, not of the encoded bytes: the cropped PNGs re-encoded to
  different bytes on the CI runner than locally on the same Go version.
- Every extractor version change blocks the deploy build until the
  screenshots are retaken (Consequences). Releases that bump the
  extractor version plan a screenshot refresh with them.
- Hosting is set up at the launch, on the public repository; until then
  the site is built and checked locally only.
