package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the fixture build")

// fixture copies testdata/fixture to a temporary directory, adds the
// withheld page (its words are built here so no committed file carries
// them), applies edit, and returns the build options.
func fixture(t *testing.T, edit func(repo, siteDir string)) options {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/fixture")); err != nil {
		t.Fatal(err)
	}
	repo, siteDir := filepath.Join(dir, "repo"), filepath.Join(dir, "site")
	held := "# ADR 0002 - Held back\n\n**Status:** Proposed\n\nWritten in " + "session" + " 4 by a " + "unit" + " Q.\n"
	writeFile(t, filepath.Join(repo, "docs/adr/0002-held.md"), held)
	if edit != nil {
		edit(repo, siteDir)
	}
	return options{Root: repo, Site: siteDir, Theme: "theme"}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceIn(t *testing.T, p, old, new string) {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", p, old)
	}
	writeFile(t, p, strings.Replace(string(data), old, new, 1))
}

func mustBuild(t *testing.T, opt options) *site {
	t.Helper()
	s, err := build(opt)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return s
}

// TestFixtureGolden builds the fixture and compares every generated file
// with testdata/golden: text files byte for byte, binary files by size and
// SHA-256 in files.txt.
func TestFixtureGolden(t *testing.T) {
	s := mustBuild(t, fixture(t, nil))
	if len(s.problems) > 0 {
		t.Fatalf("checks failed:\n%s", strings.Join(s.problems, "\n"))
	}
	var list []string
	got := map[string][]byte{}
	for _, p := range s.outputPaths() {
		if isText(p) {
			got[p] = s.out[p]
			list = append(list, p)
		} else if strings.HasSuffix(p, ".png") {
			list = append(list, p+" "+pngDigest(t, s.out[p]))
		} else {
			list = append(list, fmt.Sprintf("%s %d %x", p, len(s.out[p]), sha256.Sum256(s.out[p])))
		}
	}
	got["files.txt"] = []byte(strings.Join(list, "\n") + "\n")
	golden := filepath.Join("testdata", "golden")
	if *update {
		if err := os.RemoveAll(golden); err != nil {
			t.Fatal(err)
		}
		for p, data := range got {
			writeFile(t, filepath.Join(golden, filepath.FromSlash(p)), string(data))
		}
		return
	}
	want := map[string]bool{}
	err := filepath.WalkDir(golden, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(golden, p)
		want[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for p := range got {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(golden, filepath.FromSlash(p)))
		if err != nil {
			t.Errorf("%s: not in the golden output (run go test -update and review)", p)
			continue
		}
		if string(data) != string(got[p]) {
			t.Errorf("%s differs from its golden file (run go test -update and review the diff)", p)
			if p == "files.txt" {
				// Binary outputs are compared by size and hash; show both
				// lists so a mismatch seen only on another machine can be
				// diagnosed from the log.
				t.Logf("golden files.txt:\n%s\ngenerated files.txt:\n%s", data, got[p])
			}
		}
		delete(want, p)
	}
	for p := range want {
		t.Errorf("%s: in the golden output but no longer generated", p)
	}
}

// expectProblem builds the fixture after edit and wants a failed check
// whose message contains want.
func expectProblem(t *testing.T, want string, edit func(repo, siteDir string)) {
	t.Helper()
	s := mustBuild(t, fixture(t, edit))
	for _, p := range s.problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no check failed with %q; problems:\n%s", want, strings.Join(s.problems, "\n"))
}

func TestChecksFail(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(repo, siteDir string)
	}{
		{"quote missing from its evidence", "the quote is not in docs/evidence/link-precision.md", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/evidence/link-precision.md"), "| all | 9 | 9 |", "| all | 9 | 8 |")
		}},
		{"shown digit not in the quote", "8 is shown but not in the quote", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/numbers.json"), `"show": "9 of 9"`, `"show": "9 of 8"`)
		}},
		{"number written outside the registry", "a number outside a {placeholder}", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/home.json"), `"label": "links correct"`, `"label": "links correct on 2 corpora"`)
		}},
		{"page number outside the registry", "shows numbers that are not in the registry: 7", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "README.md"), "**Pre-release.**", "**Pre-release, 7 routes.**")
		}},
		{"stale screenshot", "taken with extractors-7, but the code is extractors-8", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "internal/index/index.go"), "extractors-7", "extractors-8")
		}},
		{"image without manifest entry", "media/extra.png has no entry in media.json", func(_, site string) {
			writeFile(t, filepath.Join(site, "media/extra.png"), "not really a png")
		}},
		{"social image with an old lead", "regenerate it", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/home.json"), `"lead": "Which calls hit this route?"`, `"lead": "Which calls reach this route?"`)
		}},
		{"README lead differs", "README.md lead block differs", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "README.md"), "The fixture links calls to routes.", "The fixture links calls.")
		}},
		{"private path in a page", "contains [\"docs/private\"", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "Calls are Best effort", "See docs/private for more. Calls are Best effort")
		}},
		{"reference pattern of the public tree check", "names", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "Calls are Best effort", "See X01-names.md. Calls are Best effort")
		}},
		{"work words in a published page", "withhold it in docs.json", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "Calls are Best effort", "Decided by the "+"orchestrator. Calls are Best effort")
		}},
		{"work words in a page's prose next to code", "withhold it in docs.json", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "Calls are Best effort", "`Enter a value`, said this "+"session. Calls are Best effort")
		}},
		{"dark twin without an entry", "its dark twin hero-night.png has no entry", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/media.json"), `"dark": "hero-dark.png"`, `"dark": "hero-night.png"`)
		}},
		{"dark twin with another crop box", "has another crop box", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/media.json"), `"x": 1,`, `"x": 2,`)
		}},
		{"dark twin that is light", "must be dark and have no twin", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/media.json"), `"theme": "dark"`, `"theme": "light"`)
		}},
		{"demo edit without saying so", "needs a caption that says it is a demo edit", func(_, site string) {
			replaceIn(t, filepath.Join(site, "content/media.json"), `"caption": "Stand-in screenshot 1.",`, `"caption": "Stand-in screenshot 1.", "demo_edit": true,`)
		}},
		{"withheld page that is clean", "it is clean now", func(repo, _ string) {
			writeFile(t, filepath.Join(repo, "docs/adr/0002-held.md"), "# ADR 0002 - Held back\n\n**Status:** Proposed\n")
		}},
		{"link to a private path", "points to a private path", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "## Calls", "See [the plan](private/plan.md).\n\n## Calls")
		}},
		{"broken repository link", "does not exist", func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "## Calls", "See [gone](gone.md).\n\n## Calls")
		}},
		{"broken anchor", `has no id "nowhere"`, func(repo, _ string) {
			replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "## Calls", "See [this](#nowhere).\n\n## Calls")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { expectProblem(t, c.want, c.edit) })
	}
}

// TestWorkWordsInCode checks that the work-word check reads prose only: a
// UI label or identifier quoted as code that reads like one (the Try it
// out label "Enter a value for this session") does not withhold a page.
func TestWorkWordsInCode(t *testing.T) {
	s := mustBuild(t, fixture(t, func(repo, _ string) {
		replaceIn(t, filepath.Join(repo, "docs/known-gaps.md"), "Calls are Best effort", "The button `Enter a value for this "+"session` keeps it.\n\n```\nthis "+"session\n```\n\nCalls are Best effort")
	}))
	if len(s.problems) > 0 {
		t.Fatalf("checks failed:\n%s", strings.Join(s.problems, "\n"))
	}
}

// TestThemeVariants checks that a screenshot with a dark twin is written in
// both variants, each marked for the page theme it shows on, that the
// lightbox links of a feature lead to the variant shown, and that the
// theme switch and its script are on every page.
func TestThemeVariants(t *testing.T) {
	s := mustBuild(t, fixture(t, nil))
	home := string(s.out["index.html"])
	for _, want := range []string{
		`<a class="zoom shot-dark" href="/media/hero-dark.png"`, `src="/media/hero-dark-crop.png"`,
		`<a class="zoom shot-light" href="/media/hero.png"`, `src="/media/hero-crop.png"`,
		`<img class="shot-dark" src="/media/hero-dark.png"`, `<img class="shot-light" src="/media/hero.png"`,
		`data-theme-choice="system"`, `<script src="/assets/site.js"></script>`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("home page lacks %s", want)
		}
	}
	if strings.Contains(home, "<script>") {
		t.Error("a page has an inline script")
	}
	if !strings.Contains(string(s.out["docs/known-gaps/index.html"]), `data-theme-choice="dark"`) {
		t.Error("a docs page has no theme switch")
	}
}

// TestPrivateFilesAreNotRead checks that the build never opens a private
// path, even one the docs name.
func TestPrivateFilesAreNotRead(t *testing.T) {
	s := mustBuild(t, fixture(t, nil))
	for _, p := range s.tree.ReadPaths() {
		if p == "AGENTS.md" || p == "NOTES.md" || strings.HasPrefix(p, "docs/private/") {
			t.Errorf("the build read %s", p)
		}
	}
	if _, err := s.tree.ReadFile("docs/private/plan.md"); err == nil {
		t.Error("reading a private file succeeded")
	}
}

// TestPublicSwitch checks both states of the public-repository switch:
// off, nothing links to or calls GitHub; on, the star button, its script
// and the GitHub links appear and the content security policy allows
// only the GitHub API.
func TestPublicSwitch(t *testing.T) {
	off := mustBuild(t, fixture(t, nil))
	if _, ok := off.out["assets/star.js"]; ok {
		t.Error("star.js is published with the switch off")
	}
	if h := string(off.out["_headers"]); !strings.Contains(h, "connect-src 'none'") || !strings.Contains(h, "X-Robots-Tag: noindex") {
		t.Errorf("headers with the switch off:\n%s", h)
	}
	for p, data := range off.out {
		if isText(p) && strings.Contains(string(data), "github.com") {
			t.Errorf("%s mentions github.com with the switch off", p)
		}
	}

	on := mustBuild(t, fixture(t, func(_, site string) {
		replaceIn(t, filepath.Join(site, "site.json"), `"public_repository": false`, `"public_repository": true`)
	}))
	if len(on.problems) > 0 {
		t.Fatalf("checks failed with the switch on:\n%s", strings.Join(on.problems, "\n"))
	}
	home := string(on.out["index.html"])
	// The GitHub link and the Star button always show with the switch
	// on; only the count waits for the API (hidden until star.js sets it).
	for _, want := range []string{`<a class="gh" href="https://github.com/example/fixture"`, `data-star-repo="example/fixture" data-star-min="100">`, `<span class="star-count" hidden>`, `src="/assets/star.js"`, "https://github.com/example/fixture/issues"} {
		if !strings.Contains(home, want) {
			t.Errorf("home page with the switch on lacks %s", want)
		}
	}
	doc := string(on.out["docs/guide/1-start-here/index.html"])
	for _, want := range []string{"issues/new?template=docs-problem.yml&amp;page=docs%2Fguide%2F1-start-here.md", "https://github.com/example/fixture/blob/main/cmd/tool/main.go"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs page with the switch on lacks %s", want)
		}
	}
	// The download page links the latest release and shows the checksum,
	// provenance and first-run steps only with the switch on.
	dl := string(on.out["download/index.html"])
	for _, want := range []string{`href="https://github.com/example/fixture/releases/latest"`, "sha256sum -c", "gh attestation verify graphvein-*-linux-amd64.tar.gz --repo example/fixture", "Run anyway", "com.apple.quarantine", "go install github.com/example/fixture/cmd/graphvein@latest"} {
		if !strings.Contains(dl, want) {
			t.Errorf("download page with the switch on lacks %s", want)
		}
	}
	if strings.Contains(dl, "No release yet") {
		t.Error("download page with the switch on says there is no release")
	}
	if off := string(off.out["download/index.html"]); strings.Contains(off, "releases/latest") || !strings.Contains(off, "No release yet") {
		t.Error("download page with the switch off links a release")
	}
	if h := string(on.out["_headers"]); !strings.Contains(h, "connect-src https://api.github.com") || strings.Contains(h, "noindex") {
		t.Errorf("headers with the switch on:\n%s", h)
	}
}

// TestRealSite builds the real site from this repository with every
// check: the number registry against the evidence pages, the screenshot
// manifest, privacy, links, and the README sharing the home page's lead
// and status copy. A screenshot older than the extractor version is
// reported but does not fail here (the deploy build refuses it), so an
// extractor change does not need new screenshots in the same change.
func TestRealSite(t *testing.T) {
	s, err := build(options{Root: "..", Site: ".", Theme: "theme", AllowStaleMedia: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, n := range s.notes {
		t.Log(n)
	}
	if len(s.problems) > 0 {
		t.Fatalf("%d check(s) failed:\n%s", len(s.problems), strings.Join(s.problems, "\n"))
	}
	var cfg siteConfig
	data, err := os.ReadFile("site.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PublicRepository {
		t.Error("site.json: the public-repository switch is on before the launch")
	}
}

func TestGithubSlug(t *testing.T) {
	seen := map[string]int{}
	for _, c := range []struct{ in, want string }{
		{"Re-measurement after the fix", "re-measurement-after-the-fix"},
		{"After the follow-ups (committed bundler output, ADR 0085)", "after-the-follow-ups-committed-bundler-output-adr-0085"},
		{"`graphvein serve` flags", "graphvein-serve-flags"},
		{"Round 3", "round-3"},
		{"Round 3", "round-3-1"},
	} {
		if got := githubSlug(c.in, seen); got != c.want {
			t.Errorf("githubSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// pngDigest describes a PNG by its size and decoded pixels. The encoded
// bytes of a re-encoded image (the crops) differ between machines even on
// the same Go version, so a byte hash would make the golden depend on the
// machine; the pixels do not.
func pngDigest(t *testing.T, data []byte) string {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding a generated PNG: %v", err)
	}
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return fmt.Sprintf("%dx%d pixels:%x", b.Dx(), b.Dy(), sha256.Sum256(rgba.Pix))
}
