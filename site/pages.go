package main

import (
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// pageData is what every template receives.
type pageData struct {
	Title       string
	Description string
	URL         string
	Nav         string // header entry marked current: home, docs, evidence, gaps, download
	Public      bool
	Repo        string
	RepoSlug    string // owner/name, for the star button
	SiteURL     string
	OGImage     string
	Footer      string
	Home        *homeView
	Doc         *docView
	Text        map[string]template.HTML // named blocks of the small pages
}

type homeView struct {
	Lead, Sublead, Eyebrow, Intro string
	StatusShort                   string
	DecidesShort, VerifiedShort   []shortView
	Hero                          *mediaView
	CardsTitle                    string
	Cards                         []cardView
	Reads                         reads
	FeaturesTitle, FeaturesIntro  string
	Features                      []featureView
	Install                       installView
	Terminal                      terminalView
	StatusIntro, StatusTable      template.HTML
	Decides                       []decision
	Verified                      []verifiedView
	GalleryTitle                  string
	Gallery                       []*mediaView
	Coming                        []string
}

type shortView struct{ Text, Href string }

type cardView struct{ Big, Baseline, Label, Text, Href, Link string }

type featureView struct {
	Title, Text, Href string
	Shot              *mediaView
}

type verifiedView struct {
	Title, Body, Href, LinkText string
	Planned                     bool
}

// installView is the build-from-source block. CloneURL is set only with the
// public-repository switch on; the block then shows the clone command and
// no longer says that there is no release (the site goes public with the
// first release).
type installView struct {
	Note, NoSource, CloneURL string
	Clone, Build, Run        []string
	BuildNote, Then          string
	ServeNote, More          string
}

type terminalView struct{ Command, Output, Caption string }

type mediaView struct {
	Src, Alt, Caption string
	Width, Height     int
	Full              string // the whole screenshot, when Src is a crop
	// DarkSrc and DarkFull are the dark-theme twin's image and whole
	// screenshot; empty when the screenshot has no twin.
	DarkSrc, DarkFull string
}

type docView struct {
	Title     string
	Body      template.HTML
	Headings  []heading
	Sidebar   []sidebarView
	Crumb     string
	Source    string
	ReportURL string
}

func (s *site) base(title, desc, u, nav string) pageData {
	d := pageData{
		Title:       title,
		Description: desc,
		URL:         u,
		Nav:         nav,
		Public:      s.c.Config.PublicRepository,
		SiteURL:     s.c.Config.SiteURL,
		Footer:      s.c.Home.Footer,
	}
	if d.Public {
		d.Repo = s.c.Config.Repository
		d.RepoSlug = strings.TrimPrefix(s.c.Config.Repository, "https://github.com/")
	}
	if d.SiteURL != "" && s.c.Media.Social != "" {
		d.OGImage = d.SiteURL + "/media/" + s.c.Media.Social
	}
	return d
}

func (s *site) put(u, tmpl string, data pageData) error {
	b, err := s.execute(tmpl, data)
	if err != nil {
		return err
	}
	s.out[outPath(u)] = b
	return nil
}

func (s *site) renderSitePages() error {
	if err := s.renderHome(); err != nil {
		return err
	}
	for _, src := range s.order {
		if err := s.renderDoc(s.pages[src]); err != nil {
			return err
		}
	}
	if err := s.renderIndexes(); err != nil {
		return err
	}
	return s.renderSmallPages()
}

var readmeBlockRe = regexp.MustCompile(`(?s)<!-- site:([a-z-]+) -->\n(.*?)\n<!-- /site:([a-z-]+) -->`)

// readmeBlocks returns the README's marked blocks: the copy the README
// shares with the site (lead, status).
func (s *site) readmeBlocks() (map[string]string, error) {
	data, err := s.tree.ReadFile("README.md")
	if err != nil {
		return nil, err
	}
	blocks := map[string]string{}
	for _, m := range readmeBlockRe.FindAllStringSubmatch(string(data), -1) {
		if m[1] != m[3] {
			return nil, fmt.Errorf("README.md: block %s closed as %s", m[1], m[3])
		}
		blocks[m[1]] = m[2]
	}
	for _, want := range []string{"lead", "status", "status-table", "trademark"} {
		if _, ok := blocks[want]; !ok {
			return nil, fmt.Errorf("README.md: missing <!-- site:%s --> block", want)
		}
	}
	return blocks, nil
}

func (s *site) readmeHTML(md string) (template.HTML, error) {
	r, err := renderMarkdown([]byte(md), s.resolver("README.md"))
	if err != nil {
		return "", fmt.Errorf("README.md: %w", err)
	}
	return template.HTML(r.HTML), nil
}

// media returns the view of a screenshot. With cropped, a screenshot
// with a crop box shows its crop (the feature rows); the hero and the
// gallery show the whole screenshot.
func (s *site) media(name string, cropped bool) (*mediaView, error) {
	f, ok := s.mediaEntry(name)
	if !ok {
		return nil, fmt.Errorf("media.json has no entry for %s", name)
	}
	m := &mediaView{Alt: f.Alt, Caption: f.Caption}
	m.Src, m.Full = shownImage(f, cropped)
	if f.Viewport.Width > 0 && f.DeviceScaleFactor > 0 {
		m.Width, m.Height = f.Viewport.Width, f.Viewport.Height
		if cropped && f.Crop != nil {
			// Shown at its source pixels' size over the device scale,
			// like the uncropped screenshots.
			m.Width = int(float64(f.Crop.W) / f.DeviceScaleFactor)
			m.Height = int(float64(f.Crop.H) / f.DeviceScaleFactor)
		}
	}
	if d, ok := s.mediaEntry(f.Dark); ok && f.Dark != "" {
		m.DarkSrc, m.DarkFull = shownImage(d, cropped)
	} // a missing twin is reported by checkTwin
	return m, nil
}

func (s *site) mediaEntry(name string) (mediaFile, bool) {
	for _, f := range s.c.Media.Files {
		if f.File == name {
			return f, true
		}
	}
	return mediaFile{}, false
}

// shownImage returns the image a page shows for a screenshot (its crop
// when cropped and it has a crop box) and the whole screenshot.
func shownImage(f mediaFile, cropped bool) (src, full string) {
	full = "/media/" + f.File
	if cropped && f.Crop != nil {
		return "/media/" + cropName(f.File), full
	}
	return full, full
}

// pageHref is the site URL of a published page, or "" when it is not
// published (withheld).
func (s *site) pageHref(src string) string {
	if strings.HasPrefix(src, "/") {
		return src // a site page; the link check verifies it
	}
	if src == "docs/adr/" {
		return "/docs/adr/"
	}
	if pg, ok := s.pages[src]; ok {
		return pg.URL
	}
	return ""
}

func (s *site) renderHome() error {
	h := s.c.Home
	x := func(v string) string {
		out, err := s.c.expand(v)
		if err != nil {
			s.fail("home.json: %v", err)
		}
		return out
	}
	v := &homeView{
		Lead: x(h.Lead), Sublead: x(h.Sublead), Eyebrow: x(h.Eyebrow), StatusShort: x(h.StatusShort),
		CardsTitle: x(h.CardsTitle), FeaturesTitle: x(h.FeatTitle), FeaturesIntro: x(h.FeatIntro),
		GalleryTitle: x(h.Gallery),
	}
	var err error
	if v.Hero, err = s.media(s.c.Media.Hero, false); err != nil {
		return err
	}
	for _, c := range h.Cards {
		cv := cardView{Big: x("{" + c.Number + "}"), Baseline: x(c.Baseline), Label: x(c.Label), Text: x(c.Text), Link: c.Link, Href: s.pageHref(c.Source)}
		if cv.Href == "" {
			s.fail("home.json: card %s cites %s, which is not published", c.Number, c.Source)
		}
		v.Cards = append(v.Cards, cv)
	}
	v.Reads = reads{Title: x(h.Reads.Title), Note: x(h.Reads.Note), Link: x(h.Reads.Link)}
	for _, l := range h.Reads.Languages {
		v.Reads.Languages = append(v.Reads.Languages, language{Name: x(l.Name), Detail: x(l.Detail)})
	}
	v.Reads.Page = s.pageHref(h.Reads.Page)
	for _, f := range h.Features {
		fv := featureView{Title: x(f.Title), Text: x(f.Text), Href: s.pageHref(f.Page)}
		if fv.Href == "" {
			s.fail("home.json: feature %q cites %s, which is not published", f.Title, f.Page)
		}
		if f.Shot != "" {
			if fv.Shot, err = s.media(f.Shot, true); err != nil {
				return err
			}
		}
		v.Features = append(v.Features, fv)
	}
	in := h.Install
	v.Install = installView{Note: x(in.Note), NoSource: x(in.NoSource), Clone: in.Clone, Build: in.Build, Run: in.Run,
		BuildNote: x(in.BuildNote), Then: x(in.Then), ServeNote: x(in.ServeNote), More: x(in.More)}
	if s.c.Config.PublicRepository {
		v.Install.CloneURL = s.c.Config.Repository
	}
	term, err := os.ReadFile(filepath.Join(s.opt.Site, "media", s.c.Media.Terminal))
	if err != nil {
		return fmt.Errorf("reading the terminal transcript: %w", err)
	}
	for _, f := range s.c.Media.Files {
		if f.File == s.c.Media.Terminal {
			v.Terminal = terminalView{Command: f.Command, Output: strings.TrimRight(string(term), "\n"), Caption: f.Caption}
		}
	}
	if v.Terminal.Command == "" {
		s.fail("media.json: the terminal transcript %s has no entry with its command", s.c.Media.Terminal)
	}
	blocks, err := s.readmeBlocks()
	if err != nil {
		return err
	}
	if v.StatusIntro, err = s.readmeHTML(blocks["status"]); err != nil {
		return err
	}
	if v.StatusTable, err = s.readmeHTML(blocks["status-table"]); err != nil {
		return err
	}
	for _, d := range h.Decides {
		v.Decides = append(v.Decides, decision{Kind: d.Kind, Title: x(d.Title), Rank: x(d.Rank), Body: x(d.Body)})
	}
	for _, it := range h.Verified {
		vv := verifiedView{Title: x(it.Title), Body: x(it.Body), Planned: it.Planned}
		switch {
		case it.Page != "":
			vv.Href, vv.LinkText = s.pageHref(it.Page), "Read"
			if vv.Href == "" {
				s.fail("home.json: %q cites %s, which is not published", it.Title, it.Page)
			}
		case it.Repo != "" && s.c.Config.PublicRepository:
			vv.Href, vv.LinkText = s.repoURL(it.Repo, strings.HasSuffix(it.Repo, "/")), "In the repository"
		}
		v.Verified = append(v.Verified, vv)
	}
	short := func(items []shortItem) []shortView {
		var out []shortView
		for _, it := range items {
			sv := shortView{Text: x(it.Text), Href: s.pageHref(it.Page)}
			if sv.Href == "" {
				s.fail("home.json: %q cites %s, which is not published", it.Text, it.Page)
			}
			out = append(out, sv)
		}
		return out
	}
	v.DecidesShort, v.VerifiedShort = short(h.DecidesShort), short(h.VerifiedShort)
	for _, g := range s.c.Media.Gallery {
		m, err := s.media(g, false)
		if err != nil {
			return err
		}
		v.Gallery = append(v.Gallery, m)
	}
	for _, c := range h.Coming {
		v.Coming = append(v.Coming, x(c))
	}
	d := s.base("Graphvein", v.Lead, "/", "home")
	d.Home = v
	if err := s.put("/", "home", d); err != nil {
		return err
	}
	// The longer copy behind the home page's short lists.
	pages := []struct{ url, title, desc, intro, tmpl string }{
		{"/status/", "Status", "What works in Graphvein today, and its measured evidence.", "", "status"},
		{"/how-graphvein-decides/", "How Graphvein decides", "Facts, match rules with fixed ranks, unknowns and precision labels.", x(h.DecidesIntro), "decides"},
		{"/how-it-is-verified/", "How it is verified", "Claim contracts, decision records, golden fixtures, CI and the published benchmark.", x(h.VerifiedIntro), "verified"},
	}
	for _, pg := range pages {
		v.Intro = pg.intro
		d := s.base(pg.title+" · Graphvein", pg.desc, pg.url, "")
		d.Home = v
		d.Title = pg.title + " · Graphvein"
		if err := s.put(pg.url, pg.tmpl, d); err != nil {
			return err
		}
	}
	return nil
}

func (s *site) crumb(u string) string {
	sec := s.section(u)
	for _, g := range s.c.Docs.Sidebar {
		for _, it := range g.Items {
			href := it.URL
			if pg, ok := s.pages[it.Page]; ok {
				href = pg.URL
			}
			if href == sec {
				return g.Group
			}
		}
	}
	return ""
}

func (s *site) docNav(u string) string {
	switch {
	case strings.HasPrefix(u, "/docs/evidence/"):
		return "evidence"
	case u == "/docs/known-gaps/":
		return "gaps"
	}
	return "docs"
}

func (s *site) renderDoc(pg *page) error {
	dv := &docView{
		Title:    pg.Title,
		Body:     template.HTML(pg.Doc.HTML),
		Headings: pg.Doc.Headings,
		Sidebar:  s.sidebar(s.section(pg.URL)),
		Crumb:    s.crumb(pg.URL),
		Source:   pg.Source,
	}
	if s.c.Config.PublicRepository {
		dv.ReportURL = s.c.Config.Repository + "/issues/new?template=docs-problem.yml&page=" + url.QueryEscape(pg.Source)
	}
	d := s.base(pg.Title+" · Graphvein docs", "Graphvein documentation: "+pg.Title, pg.URL, s.docNav(pg.URL))
	d.Doc = dv
	return s.put(pg.URL, "doc", d)
}

// indexEntry is one row of a generated docs index.
type indexEntry struct {
	Title, Href, Note string
}

type indexGroup struct {
	Title   string
	Intro   string
	Entries []indexEntry
}

var adrStatus = regexp.MustCompile(`(?m)^\*\*Status:\*\*\s*(.+)$`)

func (s *site) renderIndexes() error {
	// /docs/: the sidebar as a page.
	var groups []indexGroup
	for _, g := range s.sidebar("") {
		ig := indexGroup{Title: g.Group}
		for _, it := range g.Items {
			ig.Entries = append(ig.Entries, indexEntry{Title: it.Title, Href: it.Href})
		}
		groups = append(groups, ig)
	}
	if err := s.renderIndex("/docs/", "Documentation", "", groups); err != nil {
		return err
	}

	// /docs/adr/: every decision record with its status. A record that
	// is not published yet is listed without a link.
	adrs, err := s.tree.Markdown("docs/adr")
	if err != nil {
		return err
	}
	adr := indexGroup{}
	for _, f := range adrs {
		data, err := s.tree.ReadFile(f)
		if err != nil {
			return err
		}
		e := indexEntry{Title: strings.TrimSuffix(filepath.Base(f), ".md")}
		if pg, ok := s.pages[f]; ok {
			e.Title, e.Href = pg.Title, pg.URL
		} else if m := regexp.MustCompile(`(?m)^# (.+)$`).FindSubmatch(data); m != nil {
			e.Title = string(m[1])
		}
		if m := adrStatus.FindSubmatch(data); m != nil {
			e.Note = strings.TrimSpace(string(m[1]))
		}
		if e.Href == "" {
			e.Note = strings.TrimSpace(e.Note + " · not published on this site yet")
		}
		adr.Entries = append(adr.Entries, e)
	}
	intro := "Every design decision that affects the architecture, the indexer contract, storage, dependencies or public interfaces is recorded here: context, options considered, decision, consequences and known gaps."
	if err := s.renderIndex("/docs/adr/", "Decision records (ADRs)", intro, []indexGroup{adr}); err != nil {
		return err
	}

	// /docs/evidence/: every published evidence page.
	ev := indexGroup{}
	for _, src := range s.order {
		if strings.HasPrefix(src, "docs/evidence/") {
			ev.Entries = append(ev.Entries, indexEntry{Title: s.pages[src].Title, Href: s.pages[src].URL})
		}
	}
	sort.SliceStable(ev.Entries, func(i, j int) bool { return ev.Entries[i].Href < ev.Entries[j].Href })
	return s.renderIndex("/docs/evidence/", "Evidence", "Measurements of what Graphvein gets right and what it misses, with their method, data and limitations.", []indexGroup{ev})
}

func (s *site) renderIndex(u, title, intro string, groups []indexGroup) error {
	body, err := s.execute("index-body", struct {
		Intro  string
		Groups []indexGroup
	}{intro, groups})
	if err != nil {
		return err
	}
	d := s.base(title+" · Graphvein docs", title, u, s.docNav(u))
	d.Doc = &docView{Title: title, Body: template.HTML(body), Sidebar: s.sidebar(u), Crumb: s.crumb(u)}
	return s.put(u, "doc", d)
}

func (s *site) renderSmallPages() error {
	notice, err := s.tree.ReadFile("NOTICE")
	if err != nil {
		return err
	}
	blocks, err := s.readmeBlocks()
	if err != nil {
		return err
	}
	trademark := blocks["trademark"]
	changelog := ""
	if _, ok := s.tree.Exists("CHANGELOG.md"); ok {
		data, err := s.tree.ReadFile("CHANGELOG.md")
		if err != nil {
			return err
		}
		r, err := renderMarkdown(data, s.resolver("CHANGELOG.md"))
		if err != nil {
			return fmt.Errorf("CHANGELOG.md: %w", err)
		}
		changelog = r.HTML
	}
	x := func(v string) string {
		out, err := s.c.expand(v)
		if err != nil {
			s.fail("%v", err)
		}
		return out
	}
	in := s.c.Home.Install
	install := installView{Note: x(in.Note), NoSource: x(in.NoSource), Clone: in.Clone, Build: in.Build, Run: in.Run,
		BuildNote: x(in.BuildNote), Then: x(in.Then), ServeNote: x(in.ServeNote), More: x(in.More)}
	if s.c.Config.PublicRepository {
		install.CloneURL = s.c.Config.Repository
	}
	desc := "Build Graphvein from source; release binaries come with the first release."
	if s.c.Config.PublicRepository {
		desc = "Download Graphvein for Windows, macOS and Linux, check the download, or build it from source."
	}
	d := s.base("Download · Graphvein", desc, "/download/", "download")
	d.Home = &homeView{Install: install}
	if err := s.put("/download/", "download", d); err != nil {
		return err
	}
	tm, err := s.readmeHTML(trademark)
	if err != nil {
		return err
	}
	d = s.base("Licence · Graphvein", "Graphvein is licensed under the Apache License 2.0.", "/licence/", "")
	d.Text = map[string]template.HTML{"notice": template.HTML(template.HTMLEscapeString(string(notice))), "trademark": tm}
	if err := s.put("/licence/", "licence", d); err != nil {
		return err
	}
	d = s.base("Changelog · Graphvein", "Changes per release of Graphvein.", "/changelog/", "")
	d.Text = map[string]template.HTML{"changelog": template.HTML(changelog)}
	if err := s.put("/changelog/", "changelog", d); err != nil {
		return err
	}
	d = s.base("Page not found · Graphvein", "This page does not exist.", "/404.html", "")
	return s.put("/404.html", "notfound", d)
}
