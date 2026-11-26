package main

import (
	"fmt"
	"html"
	"path"
	"regexp"
	"sort"
	"strings"
)

func (s *site) runChecks() {
	s.checkNumbers()
	s.checkMedia()
	s.checkPrivacy()
	s.checkLinks()
	s.checkReadme()
}

// normalize collapses whitespace and drops Markdown bold markers, so a
// quote matches its evidence file however the file wraps its lines.
func normalize(v string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(v, "**", "")), " ")
}

// numberToken matches one number as a reader sees it: 388, 94.6%, 0.9,
// 1.27.4, 127.0.0.1:7420, 1:1.
var numberToken = regexp.MustCompile(`\d+(?:[.:]\d+)*%?`)

func tokens(v string) map[string]bool {
	out := map[string]bool{}
	for _, t := range numberToken.FindAllString(v, -1) {
		out[t] = true
	}
	return out
}

// checkNumbers enforces the number registry: every entry's quote is in its
// evidence file and holds every digit the site shows for it; the copy
// writes numbers only as registry placeholders; and the home and download
// pages show no number outside the registry (the screenshot captions and
// the terminal transcript are marked nocheck: they are not claims).
func (s *site) checkNumbers() {
	allowed := map[string]bool{}
	ids := map[string]bool{}
	for _, n := range s.c.Numbers {
		if ids[n.ID] {
			s.fail("numbers.json: duplicate id %s", n.ID)
		}
		ids[n.ID] = true
		data, err := s.tree.ReadFile(n.File)
		if err != nil {
			s.fail("numbers.json %s: %v", n.ID, err)
			continue
		}
		if !strings.Contains(normalize(string(data)), normalize(n.Quote)) {
			s.fail("numbers.json %s: the quote is not in %s: %q", n.ID, n.File, n.Quote)
			continue
		}
		quoted := tokens(n.Quote)
		for t := range tokens(n.Show) {
			if !quoted[t] {
				s.fail("numbers.json %s: %s is shown but not in the quote", n.ID, t)
			}
			allowed[t] = true
		}
	}
	for _, v := range copyStrings(s.c.Home) {
		if bare := placeholder.ReplaceAllString(v, ""); numberToken.MatchString(bare) {
			s.fail("home.json: a number outside a {placeholder}: %q", v)
		}
	}
	for _, p := range []string{"index.html", "download/index.html", "status/index.html", "how-graphvein-decides/index.html", "how-it-is-verified/index.html"} {
		text := visibleText(string(s.out[p]))
		var bad []string
		for t := range tokens(text) {
			if !allowed[t] {
				bad = append(bad, t)
			}
		}
		sort.Strings(bad)
		if len(bad) > 0 {
			s.fail("%s shows numbers that are not in the registry: %s", p, strings.Join(bad, ", "))
		}
	}
}

// copyStrings lists every sentence of home.json.
func copyStrings(h homeCopy) []string {
	v := []string{h.Lead, h.Sublead, h.Eyebrow, h.StatusShort, h.DecidesIntro, h.VerifiedIntro, h.CardsTitle, h.FeatTitle, h.FeatIntro, h.Gallery, h.Terminal, h.Footer,
		h.Install.Note, h.Install.NoSource, h.Install.BuildNote, h.Install.Then, h.Install.ServeNote, h.Install.More, h.Install.NoteRelease, h.Install.ThenRelease}
	v = append(v, h.Install.Clone...)
	v = append(v, h.Install.Build...)
	v = append(v, h.Install.Run...)
	v = append(v, h.Coming...)
	for _, c := range h.Cards {
		v = append(v, c.Label, c.Text, c.Link, c.RangeSep)
	}
	v = append(v, h.Reads.Title, h.Reads.Note, h.Reads.Link)
	for _, l := range h.Reads.Languages {
		v = append(v, l.Name, l.Detail)
	}
	for _, f := range h.Features {
		v = append(v, f.Title, f.Text)
	}
	for _, it := range append(append([]shortItem{}, h.DecidesShort...), h.VerifiedShort...) {
		v = append(v, it.Text)
	}
	for _, d := range h.Decides {
		v = append(v, d.Title, d.Rank, d.Body)
	}
	for _, it := range h.Verified {
		v = append(v, it.Title, it.Body)
	}
	return v
}

var (
	nocheckRe = regexp.MustCompile(`(?s)<!--nocheck-->.*?<!--/nocheck-->`)
	tagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
	headRe    = regexp.MustCompile(`(?s)<head>.*?</head>`)
)

// visibleText is the text a reader sees on a page, without the nocheck
// regions.
func visibleText(page string) string {
	page = headRe.ReplaceAllString(page, " ")
	page = nocheckRe.ReplaceAllString(page, " ")
	return html.UnescapeString(tagRe.ReplaceAllString(page, " "))
}

// checkMedia enforces the screenshot manifest: every image served has an
// entry, every screenshot and transcript was taken with the extractor
// version of the code, and an image derived from a screenshot carries the
// current lead.
func (s *site) checkMedia() {
	entries := map[string]mediaFile{}
	for _, f := range s.c.Media.Files {
		if _, dup := entries[f.File]; dup {
			s.fail("media.json: duplicate entry %s", f.File)
		}
		entries[f.File] = f
		switch f.Kind {
		case "screenshot", "terminal", "derived":
		default:
			s.fail("media.json %s: unknown kind %q", f.File, f.Kind)
		}
		if f.ExtractorVersion != s.version {
			msg := fmt.Sprintf("media.json %s: taken with %s, but the code is %s: retake it", f.File, f.ExtractorVersion, s.version)
			if s.opt.AllowStaleMedia {
				s.notes = append(s.notes, msg)
			} else {
				s.fail("%s", msg)
			}
		}
		if f.MainCommit == "" {
			s.fail("media.json %s: no main_commit", f.File)
		}
		if f.DemoEdit && !strings.Contains(strings.ToLower(f.Caption), "demo") {
			s.fail("media.json %s: a screenshot of a demo edit needs a caption that says it is a demo edit", f.File)
		}
		if f.Kind == "screenshot" {
			if f.Alt == "" || f.Corpus == "" || f.CorpusPin == "" || f.Viewport.Width == 0 || f.DeviceScaleFactor == 0 || f.Shown == "" {
				s.fail("media.json %s: a screenshot needs alt, corpus, corpus_pin, viewport, device_scale_factor and shown", f.File)
			}
		}
		if f.Kind == "derived" {
			src, ok := entries[f.DerivedFrom]
			if !ok {
				for _, g := range s.c.Media.Files {
					if g.File == f.DerivedFrom {
						src, ok = g, true
					}
				}
			}
			if !ok || src.Kind != "screenshot" {
				s.fail("media.json %s: derived_from %q is not a screenshot entry", f.File, f.DerivedFrom)
			}
			lead, _ := s.c.expand(s.c.Home.Lead)
			if normalize(f.Text) != normalize(lead) {
				s.fail("media.json %s: drawn with the lead %q, but the lead is now %q: regenerate it (site/tools/social.cjs)", f.File, f.Text, lead)
			}
		}
	}
	for _, f := range s.c.Media.Files {
		if f.Dark != "" {
			s.checkTwin(f, entries)
		}
	}
	assets := map[string]bool{}
	for _, a := range s.c.Media.Assets {
		assets[a.File] = true
	}
	for _, p := range s.outputPaths() {
		switch strings.ToLower(path.Ext(p)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg":
		default:
			continue
		}
		name := path.Base(p)
		switch {
		case strings.HasPrefix(p, "media/"):
			if src, isCrop := sourceOfCrop(name); isCrop {
				if e, ok := entries[src]; !ok || e.Crop == nil {
					s.fail("%s is a crop, but media.json has no crop box for %s", p, src)
				}
			} else if _, ok := entries[name]; !ok {
				s.fail("%s has no entry in media.json", p)
			}
		case strings.HasPrefix(p, "assets/"):
			if !assets[name] {
				s.fail("%s is not listed under assets in media.json", p)
			}
		default:
			s.fail("%s: an image outside media/ and assets/ has no manifest entry", p)
		}
	}
	for name, f := range entries {
		if _, ok := s.out["media/"+name]; !ok && f.Kind != "terminal" {
			s.fail("media.json lists %s, but site/media has no such file", name)
		}
	}
}

// checkTwin enforces that a light screenshot and its dark twin show the
// same view: both are screenshots, the twin is dark and has no twin of its
// own, and viewport, scale and crop box are equal, so the page can swap
// one for the other without moving anything.
func (s *site) checkTwin(f mediaFile, entries map[string]mediaFile) {
	d, ok := entries[f.Dark]
	switch {
	case !ok:
		s.fail("media.json %s: its dark twin %s has no entry", f.File, f.Dark)
		return
	case f.Kind != "screenshot" || d.Kind != "screenshot":
		s.fail("media.json %s: only a screenshot has a dark twin, and the twin %s must be one", f.File, f.Dark)
	case f.Theme != "light" || d.Theme != "dark" || d.Dark != "":
		s.fail("media.json %s: a light screenshot's twin %s must be dark and have no twin", f.File, f.Dark)
	case f.Viewport != d.Viewport || f.DeviceScaleFactor != d.DeviceScaleFactor:
		s.fail("media.json %s: its dark twin %s has another viewport or scale", f.File, f.Dark)
	case (f.Crop == nil) != (d.Crop == nil) || (f.Crop != nil && *f.Crop != *d.Crop):
		s.fail("media.json %s: its dark twin %s has another crop box", f.File, f.Dark)
	}
}

// workWords are the words that describe how the work behind the project
// is organised. The site never shows them: it presents the product and
// its evidence only.
var workWords = regexp.MustCompile(`\b[Ss]essions? [0-9]+\b|\b[Ss]ubagents?\b|\b[Pp]ersonas?\b|\b[Oo]rchestrat(?:or|ors|ion|ed|ing|e)\b|\b[Uu]nits? [A-Z]{1,4}[0-9]*(?:-[A-Z0-9]+)?\b|\b(?:review|build|mock|shot|server) units?\b|\bunit (?:brief|summar)|\bsession.s (?:last )?gate|\b(?:this|next|previous|earlier|later|cloud) session\b`)

// privateMarkers returns the strings no published file may contain: the
// private paths of the exclude list and the names of private files, and
// the reference pattern of scripts/check-public-tree.sh (read from the
// script, so the site and the public tree use the same check).
func (s *site) privateMarkers() ([]string, *regexp.Regexp) {
	var lits []string
	for _, e := range s.tree.excluded {
		if e == "AGENTS.md" {
			continue // the public tree has its own AGENTS.md
		}
		lits = append(lits, strings.TrimSuffix(e, "/"))
		if base := path.Base(strings.TrimSuffix(e, "/")); strings.Contains(base, ".") {
			lits = append(lits, base)
		}
	}
	data, err := s.tree.ReadFile("scripts/check-public-tree.sh")
	if err != nil {
		s.fail("privacy check: %v", err)
		return lits, nil
	}
	m := regexp.MustCompile(`(?m)^pattern='([^']+)'$`).FindSubmatch(data)
	if m == nil {
		s.fail("privacy check: no pattern='...' line in scripts/check-public-tree.sh")
		return lits, nil
	}
	re, err := regexp.Compile(string(m[1]))
	if err != nil {
		s.fail("privacy check: the reference pattern does not compile: %v", err)
		return lits, nil
	}
	return lits, re
}

func isText(p string) bool {
	switch path.Ext(p) {
	case ".html", ".txt", ".css", ".js", ".svg", ".xml", ".json":
		return true
	}
	return p == "_headers"
}

// checkPrivacy enforces that the build read only public files and that
// the output names no private path and no internal record, describes no
// part of how the work is organised, and, with the public switch off,
// neither links to nor calls GitHub. It also keeps the withheld list
// exact: a docs page with such words is withheld, and a withheld page
// that is clean must be published.
func (s *site) checkPrivacy() {
	for _, p := range s.tree.ReadPaths() {
		if s.tree.isPrivate(p) {
			s.fail("privacy: the build read the private path %s", p)
		}
	}
	lits, ref := s.privateMarkers()
	// private checks paths and record names everywhere, and the work
	// words in the text outside code (stripCode): a docs page may quote a
	// UI label or a code identifier that happens to read like one.
	private := func(text string, stripCode func(string) string) []string {
		var hits []string
		for _, l := range lits {
			if strings.Contains(text, l) {
				hits = append(hits, l)
			}
		}
		if ref != nil {
			hits = append(hits, first(ref.FindAllString(text, 3))...)
		}
		return append(hits, first(workWords.FindAllString(stripCode(text), 3))...)
	}
	repo := strings.TrimSuffix(s.c.Config.Repository, "/")
	for _, p := range s.outputPaths() {
		if !isText(p) {
			continue
		}
		text := string(s.out[p])
		hits := private(text, keepAll)
		if path.Ext(p) == ".html" {
			hits = private(text, stripHTMLCode)
		}
		if !s.c.Config.PublicRepository {
			// No link to the repository anywhere, and no request to
			// GitHub from the site's own pages, scripts and headers (a
			// docs page may mention the API in its text).
			if strings.Contains(text, repo) {
				hits = append(hits, repo)
			}
			if !strings.HasPrefix(p, "docs/") && strings.Contains(text, "api.github.com") {
				hits = append(hits, "api.github.com")
			}
		}
		if len(hits) > 0 {
			s.fail("privacy: %s contains %q", p, hits)
		}
	}
	for _, src := range s.c.Docs.Sources {
		files, err := s.tree.Markdown(src)
		if err != nil {
			continue
		}
		for _, f := range files {
			data, err := s.tree.ReadFile(f)
			if err != nil {
				continue
			}
			hits := private(string(data), stripMarkdownCode)
			_, withheld := s.withheld[f]
			switch {
			case len(hits) > 0 && !withheld:
				s.fail("privacy: %s says %q: withhold it in docs.json until its text is edited", f, first(hits))
			case len(hits) == 0 && withheld:
				s.fail("docs.json withholds %s, but it is clean now: remove it from the list", f)
			}
		}
	}
}

var (
	htmlCode   = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`)
	fencedCode = regexp.MustCompile("(?ms)^ {0,3}```.*?^ {0,3}```|^ {0,3}~~~.*?^ {0,3}~~~")
	inlineCode = regexp.MustCompile("`[^`\\n]+`")
	keepAll    = func(v string) string { return v }
)

// stripHTMLCode drops the content of <code> elements (inline code and
// code blocks) of a generated page; the work-word check reads only prose.
func stripHTMLCode(v string) string { return htmlCode.ReplaceAllString(v, " ") }

// stripMarkdownCode drops fenced code blocks and inline code spans of a
// Markdown source, matching stripHTMLCode on its rendered page.
func stripMarkdownCode(v string) string {
	return inlineCode.ReplaceAllString(fencedCode.ReplaceAllString(v, " "), " ")
}

func first(v []string) []string {
	if len(v) > 3 {
		return v[:3]
	}
	return v
}

var (
	idAttr   = regexp.MustCompile(`\sid="([^"]*)"`)
	linkAttr = regexp.MustCompile(`\s(?:href|src)="([^"]*)"`)
)

// checkLinks enforces that every link inside the site reaches a generated
// file and every fragment an element id on that page.
func (s *site) checkLinks() {
	ids := map[string]map[string]bool{}
	for _, p := range s.outputPaths() {
		if path.Ext(p) != ".html" {
			continue
		}
		set := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(string(s.out[p]), -1) {
			set[html.UnescapeString(m[1])] = true
		}
		ids[p] = set
	}
	for _, p := range s.outputPaths() {
		if path.Ext(p) != ".html" {
			continue
		}
		for _, m := range linkAttr.FindAllStringSubmatch(string(s.out[p]), -1) {
			link := html.UnescapeString(m[1])
			if link == "" {
				s.fail("links: %s has an empty link", p)
				continue
			}
			if scheme.MatchString(link) || strings.HasPrefix(link, "//") {
				continue // external; the privacy check covers GitHub links
			}
			target, frag, _ := strings.Cut(link, "#")
			targetFile := p
			if target != "" {
				if !strings.HasPrefix(target, "/") {
					s.fail("links: %s links %q, not a site-absolute path", p, link)
					continue
				}
				targetFile = outPath(target)
				if _, ok := s.out[targetFile]; !ok {
					s.fail("links: %s links %q, which the build does not generate", p, link)
					continue
				}
			}
			if frag != "" {
				set, ok := ids[targetFile]
				if !ok || !set[frag] {
					s.fail("links: %s links %q, but %s has no id %q", p, link, targetFile, frag)
				}
			}
		}
	}
}

// checkReadme enforces that the README opens with the same lead and line
// under it as the home page.
func (s *site) checkReadme() {
	blocks, err := s.readmeBlocks()
	if err != nil {
		s.fail("%v", err)
		return
	}
	lead, err1 := s.c.expand(s.c.Home.Lead)
	sub, err2 := s.c.expand(s.c.Home.Sublead)
	if err1 != nil || err2 != nil {
		return // reported by the home page
	}
	if got, want := normalize(blocks["lead"]), normalize(lead+" "+sub); got != want {
		s.fail("README.md lead block differs from site/content/home.json:\n  README: %s\n  site:   %s", got, want)
	}
}
