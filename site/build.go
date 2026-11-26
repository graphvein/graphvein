package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// options say where the generator reads and writes. In the real build
// Site and Theme are both the site/ directory; the tests point Root and
// Site at a small fixture and keep the real Theme.
type options struct {
	Root  string // repository root (only public files are read)
	Site  string // site.json, content/ and media/
	Theme string // templates/ and static/
	// SiteURL overrides site.json's site_url when site.json leaves it
	// empty (the -site-url flag).
	SiteURL string
	// AllowStaleMedia reports screenshots taken with an older extractor
	// version as notes instead of failures. Only the tests of the real
	// tree set it: a version bump must not fail every unrelated change,
	// while the deploy build still refuses stale screenshots.
	AllowStaleMedia bool
}

// page is one published Markdown document.
type page struct {
	Source string // repository path
	URL    string // site path, "/docs/.../"
	Title  string
	Doc    rendered
}

// site holds everything one build needs.
type site struct {
	opt       options
	tree      *publicTree
	c         *content
	version   string           // extractor version of the code
	pages     map[string]*page // by source path
	order     []string         // source paths, sorted
	withheld  map[string]string
	out       map[string][]byte // output path -> content
	templates *template.Template
	problems  []string
	notes     []string
}

func (s *site) fail(format string, args ...any) {
	s.problems = append(s.problems, fmt.Sprintf(format, args...))
}

// build generates the site into memory and runs every check. The output
// is returned even when checks fail, so the caller can report; it must
// not be published then.
func build(opt options) (*site, error) {
	tree, err := openPublicTree(opt.Root)
	if err != nil {
		return nil, err
	}
	c, err := loadContent(opt.Site)
	if err != nil {
		return nil, err
	}
	if c.Config.SiteURL == "" {
		c.Config.SiteURL = strings.TrimSuffix(opt.SiteURL, "/")
	}
	s := &site{opt: opt, tree: tree, c: c, pages: map[string]*page{}, withheld: map[string]string{}, out: map[string][]byte{}}
	if s.version, err = s.extractorVersion(); err != nil {
		return nil, err
	}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	if err := s.collectPages(); err != nil {
		return nil, err
	}
	if err := s.renderPages(); err != nil {
		return nil, err
	}
	if err := s.copyFiles(); err != nil {
		return nil, err
	}
	if err := s.makeCrops(); err != nil {
		return nil, err
	}
	if err := s.renderSitePages(); err != nil {
		return nil, err
	}
	s.out["_headers"] = []byte(s.headers())
	s.runChecks()
	return s, nil
}

var versionLine = regexp.MustCompile(`(?m)^const Version = "([^"]+)"`)

// extractorVersion reads the indexer's Version constant, which the
// screenshot manifest must match.
func (s *site) extractorVersion() (string, error) {
	data, err := s.tree.ReadFile("internal/index/index.go")
	if err != nil {
		return "", err
	}
	m := versionLine.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("internal/index/index.go: no Version constant")
	}
	return string(m[1]), nil
}

func (s *site) loadTemplates() error {
	t, err := template.New("site").Funcs(template.FuncMap{
		"num": func(id string) (string, error) { return s.c.expand("{" + id + "}") },
		// nocheck and endcheck mark a region the number check skips
		// (screenshot captions, the terminal transcript). html/template
		// drops ordinary comments, so they are passed as trusted HTML.
		"nocheck":  func() template.HTML { return "<!--nocheck-->" },
		"endcheck": func() template.HTML { return "<!--/nocheck-->" },
	}).ParseGlob(filepath.Join(s.opt.Theme, "templates", "*.tmpl"))
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}
	s.templates = t
	return nil
}

// docURL is the site path of a published Markdown source:
// docs/cli/mcp.md -> /docs/cli/mcp/, docs/evidence/benchmark/README.md ->
// /docs/evidence/benchmark/.
func docURL(src string) string {
	p := strings.TrimSuffix(strings.TrimPrefix(src, "docs/"), ".md")
	if path.Base(p) == "README" {
		p = path.Dir(p)
		if p == "." {
			return "/docs/"
		}
	}
	return "/docs/" + p + "/"
}

// generatedDocs are the docs pages the generator writes itself.
var generatedDocs = map[string]string{
	"/docs/":          "Documentation",
	"/docs/adr/":      "Decision records (ADRs)",
	"/docs/evidence/": "Evidence",
}

func (s *site) collectPages() error {
	for _, w := range s.c.Docs.Withheld {
		s.withheld[w.Path] = w.Reason
	}
	urls := map[string]string{}
	for u := range generatedDocs {
		urls[u] = "(generated)"
	}
	for _, src := range s.c.Docs.Sources {
		files, err := s.tree.Markdown(src)
		if err != nil {
			return err
		}
		for _, f := range files {
			if _, ok := s.withheld[f]; ok {
				continue
			}
			u := docURL(f)
			if other, dup := urls[u]; dup {
				return fmt.Errorf("%s and %s both map to %s", f, other, u)
			}
			urls[u] = f
			s.pages[f] = &page{Source: f, URL: u}
			s.order = append(s.order, f)
		}
	}
	for w := range s.withheld {
		if _, ok := s.tree.Exists(w); !ok {
			s.fail("docs.json withholds %s, which does not exist", w)
		}
	}
	sort.Strings(s.order)
	return nil
}

// repoURL is the public repository address of a repository path.
func (s *site) repoURL(rel string, isDir bool) string {
	kind := "blob"
	if isDir {
		kind = "tree"
	}
	return s.c.Config.Repository + "/" + kind + "/main/" + strings.TrimSuffix(rel, "/")
}

var scheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// resolver rewrites the links of a source at `from`: links between
// published pages become site links, links to other public files go to
// the public repository (switch on) or become plain text (switch off),
// and links into the app (#/api) become plain text.
func (s *site) resolver(from string) linkResolver {
	return func(dest string) (string, bool) {
		switch {
		case dest == "":
			return dest, true
		case strings.HasPrefix(dest, "#/"):
			return "", false // a route of the app, not of the site
		case strings.HasPrefix(dest, "#"):
			return dest, true
		case scheme.MatchString(dest):
			if s.isProjectRepo(dest) && !s.c.Config.PublicRepository {
				return "", false
			}
			return dest, true
		}
		p, frag, _ := strings.Cut(dest, "#")
		p, _, _ = strings.Cut(p, "?")
		if unesc, err := url.PathUnescape(p); err == nil {
			p = unesc
		}
		target := path.Clean(path.Join(path.Dir(from), p))
		if strings.HasPrefix(target, "../") || target == ".." {
			s.fail("%s: link %q leaves the repository", from, dest)
			return "", false
		}
		if frag != "" {
			frag = "#" + frag
		}
		if pg, ok := s.pages[target]; ok {
			return pg.URL + frag, true
		}
		if pg, ok := s.pages[path.Join(target, "README.md")]; ok {
			return pg.URL + frag, true
		}
		if target == "docs/adr" || target == "docs/evidence" {
			return "/" + target + "/" + frag, true
		}
		if s.tree.isPrivate(target) {
			s.fail("%s: link %q points to a private path", from, dest)
			return "", false
		}
		isDir, ok := s.tree.Exists(target)
		if !ok {
			s.fail("%s: link %q: %s does not exist", from, dest, target)
			return "", false
		}
		if !s.c.Config.PublicRepository {
			return "", false
		}
		return s.repoURL(target, isDir) + frag, true
	}
}

func (s *site) isProjectRepo(u string) bool {
	repo := strings.TrimSuffix(s.c.Config.Repository, "/")
	return u == repo || strings.HasPrefix(u, repo+"/") || strings.HasPrefix(u, repo+"#")
}

func (s *site) renderPages() error {
	for _, src := range s.order {
		pg := s.pages[src]
		data, err := s.tree.ReadFile(src)
		if err != nil {
			return err
		}
		r, err := renderMarkdown(data, s.resolver(src))
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		pg.Doc = r
		pg.Title = r.Title
		if pg.Title == "" {
			pg.Title = path.Base(strings.TrimSuffix(src, ".md"))
		}
	}
	return nil
}

// sidebarView is the docs sidebar as the templates draw it.
type sidebarView struct {
	Group string
	Items []navItem
}

type navItem struct {
	Title   string
	Href    string
	Current bool
	Note    string
}

func (s *site) sidebar(current string) []sidebarView {
	var out []sidebarView
	for _, g := range s.c.Docs.Sidebar {
		v := sidebarView{Group: g.Group}
		for _, it := range g.Items {
			href, title := it.URL, it.Title
			if it.Page != "" {
				pg, ok := s.pages[it.Page]
				if !ok {
					continue // withheld or missing: listed again once published
				}
				href = pg.URL
				if title == "" {
					title = pg.Title
				}
			}
			v.Items = append(v.Items, navItem{Title: title, Href: href, Current: href == current})
		}
		if len(v.Items) > 0 {
			out = append(out, v)
		}
	}
	return out
}

// group finds the sidebar entry a page belongs to, for pages that are not
// in the sidebar themselves (an ADR belongs to "Decision records").
func (s *site) section(u string) string {
	switch {
	case strings.HasPrefix(u, "/docs/adr/"):
		return "/docs/adr/"
	case strings.HasPrefix(u, "/docs/contract/claims/"):
		if pg, ok := s.pages["docs/contract/claims/README.md"]; ok {
			return pg.URL
		}
	case strings.HasPrefix(u, "/docs/evidence/"):
		for _, g := range s.c.Docs.Sidebar {
			for _, it := range g.Items {
				if pg, ok := s.pages[it.Page]; ok && pg.URL == u {
					return u
				}
			}
		}
		return "/docs/evidence/"
	}
	return u
}

func (s *site) execute(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// outPath is the output file of a site path.
func outPath(u string) string {
	if strings.HasSuffix(u, "/") {
		return strings.TrimPrefix(u, "/") + "index.html"
	}
	return strings.TrimPrefix(u, "/")
}

// copyFiles copies the static theme files, the manifest's media and the
// legal texts into the output.
func (s *site) copyFiles() error {
	static := filepath.Join(s.opt.Theme, "static")
	entries, err := os.ReadDir(static)
	if err != nil {
		return fmt.Errorf("reading %s: %w", static, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "star.js" && !s.c.Config.PublicRepository {
			continue // no request to GitHub with the switch off
		}
		data, err := os.ReadFile(filepath.Join(static, e.Name()))
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		s.out["assets/"+e.Name()] = data
	}
	media := filepath.Join(s.opt.Site, "media")
	entries, err = os.ReadDir(media)
	if err != nil {
		return fmt.Errorf("reading %s: %w", media, err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".txt") {
			continue // transcripts are shown in pages, not served
		}
		data, err := os.ReadFile(filepath.Join(media, e.Name()))
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		s.out["media/"+e.Name()] = data
	}
	for _, f := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES"} {
		data, err := s.tree.ReadFile(f)
		if err != nil {
			return err
		}
		s.out[f+".txt"] = data
	}
	return nil
}

// write replaces dir with the generated files.
func (s *site) write(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing %s: %w", dir, err)
	}
	for _, p := range s.outputPaths() {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, s.out[p], 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", full, err)
		}
	}
	return nil
}

func (s *site) outputPaths() []string {
	var ps []string
	for p := range s.out {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}
