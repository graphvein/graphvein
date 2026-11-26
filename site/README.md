# Graphvein website

The static website of Graphvein: a home page and the public docs, built by
a small Go program in this folder (its own module; the only dependency is
goldmark). Design, hosting and checks: [ADR 0091](../docs/adr/0091-project-website.md).

## Build

From this folder, with Go 1.24 or newer:

```bash
go run . -out dist        # writes dist/ (ignored by Git)
go test ./...             # fixture golden, failing-check cases, the real site
```

The build reads only files of the public snapshot (paths listed in
`scripts/public-exclude.txt` are refused) and writes nothing when a check
fails:

- every number on the home, download, status and explanation pages comes from
  `content/numbers.json`, whose quotes must appear in their evidence files;
- every image has an entry in `content/media.json`, and screenshots and
  transcripts carry the extractor version of `internal/index/index.go`;
- no page names a private path, an internal record (the pattern of
  `scripts/check-public-tree.sh`) or, in its prose (code is skipped), how
  the work behind the project is organised; docs pages that do are listed under `withheld` in
  `content/docs.json` until their text is edited;
- every link inside the site and every fragment resolves;
- the README's `<!-- site:lead -->` block equals the lead and the line
  under it in `content/home.json`, and the status section and table are
  read from the README's `site:status` and `site:status-table` blocks.

## Where things are

| Path | What |
|---|---|
| `site.json` | the public-repository switch (`public_repository`, on since the first release) and the site address (`site_url`, the canonical address used for canonical links and the social image) |
| `content/home.json` | every sentence of the home page; numbers only as `{id}` placeholders |
| `content/numbers.json` | the number registry: id, text shown, evidence file, exact quote |
| `content/media.json` | the screenshot manifest: hero, social image, terminal transcript, gallery, every image |
| `content/docs.json` | which docs become pages, the withheld pages, the docs sidebar |
| `media/` | screenshots (PNG), the terminal transcript and the social preview image |
| `theme/` | templates, style sheet, `site.js` (theme switch and screenshot lightbox), the star-count script, logo and icons |
| `tools/social.cjs` | renders `media/social.png` from the lead and the hero (Node and Playwright) |
| `tools/check-layout.cjs` | the local layout check run by `scripts/check-site.sh` |
| `testdata/fixture/` | a small repository and site for the tests (no ignore files, no renamed paths) |
| `testdata/golden/` | the fixture's generated output; update with `go test -run TestFixtureGolden -update` and review the diff |

To swap the hero or add gallery images, add the file to `media/` with its
manifest entry and change `hero` or `gallery` in `content/media.json`. A
`crop` box (x, y, w, h in source pixels) on an entry makes the build show
that part in the feature rows, linked to the whole screenshot.
Screenshots come in two themes: the light entry names its twin in
`dark` (same view, viewport and crop box), and the page shows the one
opposite to its own theme. Mark a screenshot of a demo edit with
`demo_edit: true`; its caption must say it is a demo edit.
After changing the lead, run `node tools/social.cjs` so the social image
shows it (the build fails until then).
