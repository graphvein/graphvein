package server

import (
	"context"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/packages"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/testscope"
)

// This file holds the component model of the architecture view (T026, ADR
// 0079): GET /api/v0/architecture and /api/v0/architecture/component
// (docs/cli/serve.md). Components, kinds, visibility, deploy markers, links
// and annotation cells are computed from stored facts on every request and
// never stored, so a configuration change (test scope, architecture.hide)
// applies without a re-index. architecture_links.go holds the links and
// annotation cells. The claim contract rows (CM-A1, CM-R1, CK, CV, CD, CE,
// AP, AA, AT, AE) are in docs/contract/claims/architecture-graph.md; the code
// tables below list them in the same order, and TestComponentContract,
// TestComponentLinkContract and TestAnnotationContract keep them 1:1.

// archRow is one server row of a T026 claim contract. Pending names the
// Implementation step whose facts the row reads when this server has no branch for
// it yet (the row's cells stay on the default branch until it lands).
type archRow struct {
	ID      string
	Pending string
}

// componentRows are the server rows of the components contract, in table
// order (the indexer rows CM-M1..CM-U1 are internal/extract/packages').
var componentRows = []archRow{
	{ID: "CM-A1"}, {ID: "CM-R1"},
	{ID: "CK1"}, {ID: "CK2"}, {ID: "CK3"}, {ID: "CK4"}, {ID: "CK5"}, {ID: "CK6"}, {ID: "CK7"}, {ID: "CKD"},
	{ID: "CV1"}, {ID: "CV2"}, {ID: "CV3"}, {ID: "CV4"}, {ID: "CVD"},
	{ID: "CD1"}, {ID: "CD2"}, {ID: "CD3"}, {ID: "CD4"}, {ID: "CDD"},
}

// archTag is a test-only hook: every branch of the architecture rows
// reports its row ("<row>" or "<row>/<form>").
var archTag func(row string)

func tagRow(row string) {
	if archTag != nil {
		archTag(row)
	}
}

// docsGenerators is row CK6's closed list (O1 (a)): an npm package that
// declares one of them is a docs site.
var docsGenerators = []string{"@docusaurus/core", "vitepress", "@astrojs/starlight", "nextra"}

// manifestBaseNames are the manifest file names of rows CM-M1..M5; an index
// that stated no manifest at all shows their directories as unread.
var manifestBaseNames = map[string]bool{"package.json": true, "Cargo.toml": true, "pyproject.toml": true, "go.mod": true, "setup.py": true, "setup.cfg": true}

// Component states.
const (
	componentRead       = "read"
	componentUnread     = "unread"
	componentUnassigned = "unassigned"
)

// Component kinds (CK rows).
const (
	kindServer       = "server"
	kindTauriCore    = "tauri_core"
	kindClient       = "client"
	kindSchemaSource = "schema_source"
	kindBinary       = "binary"
	kindDocsSite     = "docs_site"
	kindPackage      = "package"
	kindUnassigned   = "unassigned_files"
	kindUnknown      = "unknown"
)

// Visibility values (CV rows).
const (
	visibleShown  = "shown"
	visibleFolded = "folded"
	visibleHidden = "hidden"
)

// archManifest is one manifest of a component: a stored package node, or
// an unread manifest (CM-U1) with the reason.
type archManifest struct {
	File      string  `json:"file"`
	Package   *string `json:"package"`
	Ecosystem *string `json:"ecosystem"`
	Name      *string `json:"name"`
	Unread    *string `json:"unread"`
}

// archMarker is a deploy marker (CD rows). A marker never creates a link.
type archMarker struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	File  string `json:"file"`
	Row   string `json:"row"`
	// Basis is CD4's stated basis: Compose's default merge, or the
	// assumption that two compose files are combined.
	Basis string `json:"basis,omitempty"`
}

// archCounts are a component's stored nodes by kind, test scope excluded.
type archCounts struct {
	Files       int `json:"files"`
	TestFiles   int `json:"test_files"`
	Manifests   int `json:"manifests"`
	Routes      int `json:"routes"`
	ClientCalls int `json:"client_calls"`
	IPCCommands int `json:"ipc_commands"`
	IPCCalls    int `json:"ipc_calls"`
	// WrapperCalls are client and IPC calls with status wrapper: a
	// recognized wrapper's forwarding call, whose requests are the
	// wrapper's call sites (counted above); neither linked nor in the
	// ghost (review fix).
	WrapperCalls int `json:"wrapper_calls"`
	Tables       int `json:"tables"`
}

// archComponent is one L1 component.
type archComponent struct {
	ID         string         `json:"id"`
	Directory  string         `json:"directory"`
	Name       string         `json:"name"`
	State      string         `json:"state"`
	Reason     *string        `json:"reason"`
	Manifests  []archManifest `json:"manifests"`
	Kinds      []string       `json:"kinds"`
	Frameworks []string       `json:"frameworks"`
	Visibility string         `json:"visibility"`
	HiddenBy   *string        `json:"hidden_by"`
	Rows       []string       `json:"rows"`
	Markers    []archMarker   `json:"markers"`
	Ingress    []archIngress  `json:"ingress,omitempty"`
	Counts     archCounts     `json:"counts"`

	packages     []contract.Node
	dependencies map[string][]string // dependency name -> manifests declaring it
}

// archModel is the component model of one request.
type archModel struct {
	components  map[string]*archComponent
	order       []string
	fileOf      map[string]string // stored file -> component directory
	testFile    map[string]bool
	files       []string
	manifests   string  // "read" or "not_read"
	notice      *string // why manifests were not read
	unmapped    []archNotProven
	nodesByKind map[string][]contract.Node
	services    map[string]serviceMap // deploy_service identity -> CD3/CD4 mapping
	// tiedTo is the database card a database service's image is tied to
	// by AE2 (deploy_service identity -> card id), filled by buildLinks.
	tiedTo map[string]string
}

// componentOfFile is the component directory of a stored file, or of a
// node's file; "" for synthetic nodes.
func (m *archModel) componentOfFile(file string) string {
	if c, ok := m.fileOf[file]; ok {
		return c
	}
	return ""
}

// componentOf is the component directory of a node identity.
func (m *archModel) componentOf(id string) string {
	file, ok := identityFile(id)
	if !ok {
		return ""
	}
	return m.componentOfFile(file)
}

// identityFile is the file part of a node identity. Most identities hold
// no escape in their file part, which is then the file as stored; the
// overview reads it for every calls and imports edge, so it skips the full
// parse then (about 0.2 s on langflow).
func identityFile(id string) (string, bool) {
	colon := strings.IndexByte(id, ':')
	hash := strings.IndexByte(id, '#')
	if colon > 0 && hash > colon {
		if part := id[colon+1 : hash]; !strings.Contains(part, "%") {
			return part, true
		}
	}
	_, file, _, err := contract.ParseIdentity(id)
	return file, err == nil
}

// archKinds are the node kinds the model reads besides package nodes.
var archKinds = []string{"route", "client_call", "ipc_command", "ipc_call", "table", "dockerfile", "deploy_service"}

// buildComponents applies rows CM-A1, CM-R1, CK, CV and CD to the stored
// facts.
func buildComponents(rd *reader, cfg config.Config) (*archModel, error) {
	hashes, err := rd.fileHashes()
	if err != nil {
		return nil, err
	}
	pkgs, err := rd.nodes("package")
	if err != nil {
		return nil, err
	}
	unread, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, []string{packages.UnreadCode}, nil)
	if err != nil {
		return nil, err
	}
	m := &archModel{components: map[string]*archComponent{}, fileOf: map[string]string{}, testFile: map[string]bool{}, manifests: "read", nodesByKind: map[string][]contract.Node{}}
	for _, kind := range archKinds {
		nodes, err := rd.nodes(kind)
		if err != nil {
			return nil, err
		}
		m.nodesByKind[kind] = nodes
	}
	for p := range hashes {
		m.files = append(m.files, p)
		m.testFile[p] = testscope.Classify(p, cfg.Tests) == testscope.Test
	}
	sort.Strings(m.files)
	component := func(dir string) *archComponent {
		c, ok := m.components[dir]
		if !ok {
			c = &archComponent{ID: dir, Directory: dir, Manifests: []archManifest{}, Kinds: []string{}, Frameworks: []string{}, Rows: []string{}, Markers: []archMarker{}, dependencies: map[string][]string{}}
			m.components[dir] = c
		}
		return c
	}
	for _, n := range pkgs {
		dir := path.Dir(n.File)
		c := component(dir)
		c.State = componentRead
		c.packages = append(c.packages, n)
		id, file := n.Identity, n.File
		mf := archManifest{File: file, Package: &id, Ecosystem: stringAttr(n, "ecosystem"), Name: stringAttr(n, "name")}
		c.Manifests = append(c.Manifests, mf)
		for _, dep := range stringList(n.Attributes["dependencies"]) {
			c.dependencies[dep] = append(c.dependencies[dep], file)
		}
	}
	for _, d := range unread {
		if _, stored := hashes[d.File]; !stored {
			continue
		}
		tagRow("CM-U1")
		c := component(path.Dir(d.File))
		if c.State == "" {
			c.State = componentUnread
		}
		reason := d.Message
		c.Manifests = append(c.Manifests, archManifest{File: d.File, Unread: &reason})
	}
	if len(pkgs) == 0 && len(unread) == 0 {
		// No manifest stated at all: an index from before the package pass
		// or an indexer that does not read manifests. Every manifest
		// directory is unread rather than guessed (CM-U1).
		for _, p := range m.files {
			if manifestBaseNames[path.Base(p)] {
				if m.manifests == "read" {
					notice := "this index states no package manifests; re-index with a Graphvein indexer that reads them"
					m.manifests, m.notice = "not_read", &notice
				}
				tagRow("CM-U1/stale")
				c := component(path.Dir(p))
				c.State = componentUnread
				reason := "manifest not read by this index (re-index to read manifests)"
				c.Manifests = append(c.Manifests, archManifest{File: p, Unread: &reason})
			}
		}
	}
	// CM-A1: each file to the nearest enclosing component directory, else
	// the root (CM-R1).
	for _, p := range m.files {
		dir := path.Dir(p)
		for {
			if _, ok := m.components[dir]; ok {
				break
			}
			if dir == "." {
				break
			}
			dir = path.Dir(dir)
		}
		if _, ok := m.components[dir]; ok {
			tagRow("CM-A1")
		} else {
			tagRow("CM-R1")
			c := component(".")
			c.State = componentUnassigned
		}
		m.fileOf[p] = dir
		c := m.components[dir]
		c.Counts.Files++
		if m.testFile[p] {
			c.Counts.TestFiles++
		}
		for _, mf := range c.Manifests {
			if mf.File == p {
				c.Counts.Manifests++
				break
			}
		}
	}
	for dir := range m.components {
		m.order = append(m.order, dir)
	}
	sort.Slice(m.order, func(i, j int) bool { return lessRoot(m.order[i], m.order[j]) })
	for _, dir := range m.order {
		c := m.components[dir]
		if c.Counts.Files == 0 {
			// A manifest directory with no stored file cannot occur (the
			// manifest is a file); keep the model total anyway.
			continue
		}
		m.kinds(c, hashes)
		m.visibility(c, cfg)
		m.markers(c, hashes)
		c.Name = componentName(c)
		if c.State == componentUnread {
			reason := "manifest unread: kind unknown"
			c.Reason = &reason
		}
		if c.State == componentUnassigned {
			reason := "files under no package manifest"
			c.Reason = &reason
		}
		sort.Strings(c.Rows)
	}
	m.unmappedServices()
	m.ingress()
	return m, nil
}

// componentName is the package names joined, else the directory.
func componentName(c *archComponent) string {
	var names []string
	for _, mf := range c.Manifests {
		if mf.Name != nil {
			names = append(names, *mf.Name)
		}
	}
	names = distinct(names)
	switch {
	case c.State == componentUnassigned:
		return "unassigned"
	case len(names) > 0:
		return strings.Join(names, ", ")
	case c.Directory == ".":
		return "."
	}
	return path.Base(c.Directory)
}

// kinds applies rows CK1..CK6 and CKD.
func (m *archModel) kinds(c *archComponent, hashes map[string]string) {
	has := map[string]bool{}
	frameworks := map[string]bool{}
	for _, kind := range []string{"route", "client_call", "ipc_command", "ipc_call", "table"} {
		for _, n := range m.nodesByKind[kind] {
			if m.componentOfFile(n.File) != c.Directory {
				continue
			}
			test := n.Attributes["scope"] == "test"
			if !test {
				switch kind {
				case "route":
					c.Counts.Routes++
					if f := stringAttr(n, "framework"); f != nil {
						frameworks[*f] = true
					}
				case "client_call", "ipc_call":
					switch {
					case statusOf(n) == "wrapper":
						c.Counts.WrapperCalls++
					case kind == "client_call":
						c.Counts.ClientCalls++
					default:
						c.Counts.IPCCalls++
					}
				case "ipc_command":
					c.Counts.IPCCommands++
				case "table":
					c.Counts.Tables++
				}
			}
			// CK4 reads any table node; the others only non-test ones.
			if kind == "table" || !test {
				has[kind] = true
			}
		}
	}
	add := func(row, kind string) {
		tagRow(row)
		c.Rows = append(c.Rows, row)
		c.Kinds = append(c.Kinds, kind)
	}
	if c.State == componentUnread {
		// CM-U1: an unread manifest's directory has kind unknown.
		c.Kinds = append(c.Kinds, kindUnknown)
		return
	}
	if has["route"] {
		add("CK1", kindServer)
		for f := range frameworks {
			c.Frameworks = append(c.Frameworks, f)
		}
		sort.Strings(c.Frameworks)
	}
	if has["ipc_command"] {
		add("CK2", kindTauriCore)
	}
	if (has["client_call"] || has["ipc_call"]) && !has["route"] {
		add("CK3", kindClient)
	}
	if has["table"] {
		add("CK4", kindSchemaSource)
	}
	if m.binary(c, hashes) {
		add("CK5", kindBinary)
	}
	if docsSite(c) {
		add("CK6", kindDocsSite)
	}
	switch {
	case len(c.Kinds) == 0 && c.State == componentUnassigned:
		// CK7 (review fix): the CM-R1 root holds files under no
		// manifest; it is no package, so it is never folded (CV3).
		add("CK7", kindUnassigned)
	case len(c.Kinds) == 0:
		add("CKD", kindPackage)
	}
}

// binary is row CK5 over the component's package nodes and stored files.
func (m *archModel) binary(c *archComponent, hashes map[string]string) bool {
	for _, n := range c.packages {
		dir := path.Dir(n.File)
		prefix := ""
		if dir != "." {
			prefix = dir + "/"
		}
		switch n.Attributes["ecosystem"] {
		case "cargo":
			if len(stringList(n.Attributes["bins"])) > 0 {
				tagRow("CK5/cargo-bin")
				return true
			}
			if autobins, _ := n.Attributes["autobins"].(bool); autobins {
				if _, ok := hashes[prefix+"src/main.rs"]; ok {
					tagRow("CK5/cargo-main")
					return true
				}
				for p := range hashes {
					if path.Dir(p) == prefix+"src/bin" && strings.HasSuffix(p, ".rs") {
						tagRow("CK5/cargo-src-bin")
						return true
					}
				}
			}
		case "python":
			if len(stringList(n.Attributes["scripts"])) > 0 {
				tagRow("CK5/python-scripts")
				return true
			}
		case "npm":
			if len(stringList(n.Attributes["bins"])) > 0 {
				tagRow("CK5/npm-bin")
				return true
			}
		case "go":
			for _, f := range stringList(n.Attributes["main_files"]) {
				if _, ok := hashes[f]; ok {
					tagRow("CK5/go-main")
					return true
				}
			}
		}
	}
	return false
}

// docsSite is row CK6: an npm package declaring a docs generator.
func docsSite(c *archComponent) bool {
	for _, n := range c.packages {
		if n.Attributes["ecosystem"] != "npm" {
			continue
		}
		deps := stringList(n.Attributes["dependencies"])
		for _, g := range docsGenerators {
			if contains(deps, g) {
				return true
			}
		}
	}
	return false
}

// visibility applies rows CV1..CV4 and CVD, first match wins. CV1 reads
// the component's files other than its manifests: a manifest is never
// test scope, so "every file" would never hold.
func (m *archModel) visibility(c *archComponent, cfg config.Config) {
	hide := func(row, by string) {
		tagRow(row)
		c.Rows = append(c.Rows, row)
		c.Visibility = visibleHidden
		c.HiddenBy = &by
	}
	switch {
	case c.Counts.Files > c.Counts.Manifests && c.Counts.TestFiles == c.Counts.Files-c.Counts.Manifests:
		hide("CV1", "tests")
	case contains(c.Kinds, kindDocsSite):
		hide("CV2", "docs")
	case cfg.Architecture.HideGiven && hiddenByConfig(c.Directory, cfg.Architecture.Hide):
		hide("CV4", "configured")
	case len(c.Kinds) == 1 && c.Kinds[0] == kindPackage:
		tagRow("CV3")
		c.Rows = append(c.Rows, "CV3")
		c.Visibility = visibleFolded
	default:
		tagRow("CVD")
		c.Visibility = visibleShown
	}
}

// hiddenByConfig matches a component directory against architecture.hide.
func hiddenByConfig(dir string, rules testscope.Rules) bool {
	included, _ := rules.Match(dir + "/")
	return included
}

// markers applies rows CD1..CD4; CDD is every other directory (no marker).
func (m *archModel) markers(c *archComponent, hashes map[string]string) {
	prefix := ""
	if c.Directory != "." {
		prefix = c.Directory + "/"
	}
	for _, n := range m.nodesByKind["dockerfile"] {
		if path.Dir(n.File) == c.Directory {
			tagRow("CD1")
			c.Markers = append(c.Markers, archMarker{Kind: "dockerfile", Label: "Dockerfile " + n.File, File: n.File, Row: "CD1"})
		}
	}
	for _, p := range []string{prefix + "tauri.conf.json", prefix + "src-tauri/tauri.conf.json"} {
		if _, ok := hashes[p]; ok {
			tagRow("CD2")
			c.Markers = append(c.Markers, archMarker{Kind: "tauri_app", Label: "Tauri app", File: p, Row: "CD2"})
		}
	}
	for _, n := range m.nodesByKind["deploy_service"] {
		if mp := m.serviceMapping(n); mp.dir == c.Directory {
			tagRow(mp.tag)
			label := "compose service " + n.Name + " (" + n.File + ")"
			if mp.basis != "" {
				label += ", " + mp.basis
			}
			c.Markers = append(c.Markers, archMarker{Kind: "compose_service", Label: label, File: n.File, Row: mp.row, Basis: mp.basis})
		}
	}
	if len(c.Markers) == 0 {
		tagRow("CDD")
	}
}

// serviceComponent is the component a compose service maps to by rows
// CD3 and CD4 (internal/server/architecture_deploy.go), "" when none.
func (m *archModel) serviceComponent(n contract.Node) (string, string) {
	mp := m.serviceMapping(n)
	return mp.dir, mp.tag
}

// unmappedServices lists compose services CD3 and CD4 map to no component,
// with the reason.
func (m *archModel) unmappedServices() {
	for _, n := range m.nodesByKind["deploy_service"] {
		if mp := m.serviceMapping(n); mp.dir == "" {
			tagRow("CDD/service")
			m.unmapped = append(m.unmapped, archNotProven{Kind: "service_not_mapped", Row: "CD3", Text: "? not mapped: compose service " + n.Name + " (" + n.File + "): " + mp.reason, Count: 1,
				Service: n.Name, File: n.File, Reason: mp.reason, identity: n.Identity})
		}
	}
}

// GET /api/v0/architecture

type architectureResponse struct {
	APIVersion int      `json:"api_version"`
	Repository string   `json:"repository"`
	Index      *lastRun `json:"index"`
	Scope      string   `json:"scope"`
	Manifests  string   `json:"manifests"`
	Notice     *string  `json:"notice"`
	// ParseFailed counts the files the run could not parse (T074): the
	// components, links and counts below miss their facts. Omitted when
	// zero; GET /api/v0/unread lists the files.
	ParseFailed int               `json:"parse_failed,omitempty"`
	Components  []*archComponent  `json:"components"`
	Links       []*archLink       `json:"links"`
	Databases   []*archDatabase   `json:"databases"`
	Stubs       []archStub        `json:"stubs"`
	Ghosts      []archGhost       `json:"ghosts"`
	Documents   []archDocuments   `json:"documents"`
	Hidden      archHidden        `json:"hidden"`
	NotDrawn    map[string]int    `json:"not_drawn"`
	NotProven   []archNotProven   `json:"not_proven"`
	Libraries   archLibrariesCard `json:"libraries"`
	// Project is the link pass's view in project mode (serve --project,
	// row CE8); absent on a single-repository server.
	Project *archProjectDoc `json:"project,omitempty"`
}

// archHidden is the Hidden note: what default visibility leaves out.
type archHidden struct {
	TestComponents       int            `json:"test_components"`
	DocsComponents       int            `json:"docs_components"`
	ConfiguredComponents int            `json:"configured_components"`
	TestEdges            map[string]int `json:"test_edges"`
}

// archLibrariesCard is the folded "Workspace libraries (n)" card (CV3, AG4).
type archLibrariesCard struct {
	Components []string `json:"components"`
	LinksIn    int      `json:"links_in"`
	LinksOut   int      `json:"links_out"`
}

// archNotProven is one entry of the Not proven list.
type archNotProven struct {
	Kind      string `json:"kind"`
	Row       string `json:"row"`
	Component string `json:"component,omitempty"`
	Text      string `json:"text"`
	Count     int    `json:"count"`
	// Service, File and Reason name a compose service entry
	// (service_not_mapped) so a client can group them; Database is the
	// database card of a may_also_use entry, and for a service_not_mapped
	// entry the card AE2 ties the service's database image to (the service
	// maps to no component, but its image is on that card).
	Service  string `json:"service,omitempty"`
	File     string `json:"file,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Database string `json:"database,omitempty"`

	identity string
}

func archScopeParam(q url.Values) (string, error) {
	v, present, err := param(q, "scope")
	if err != nil || !present {
		return scopeProduction, err
	}
	if v == scopeProduction || v == scopeAll {
		return v, nil
	}
	return "", badRequest("scope must be production or all, not %q", v)
}

func computeArchitecture(h *apiHandler, q url.Values) (readFunc, error) {
	scope, err := archScopeParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		cfg, err := h.opt.loadConfig()
		if err != nil {
			return nil, badRequest("reading graphvein.yaml: %v", err)
		}
		m, err := buildComponents(rd, cfg)
		if err != nil {
			return nil, err
		}
		px, err := h.opt.project.archProject(ctx, h.opt.Repository)
		if err != nil {
			return nil, err
		}
		g, err := buildLinks(rd, m, scope, px)
		if err != nil {
			return nil, err
		}
		out := architectureResponse{Repository: h.opt.Repository, Index: indexOf(runs), Scope: scope, Manifests: m.manifests, Notice: m.notice,
			Components: []*archComponent{}, Links: g.links, Databases: g.databases, Stubs: g.stubs, Ghosts: g.ghosts, Documents: g.documents,
			Hidden: archHidden{TestEdges: g.testEdges}, NotDrawn: g.notDrawn, NotProven: []archNotProven{}, Libraries: archLibrariesCard{Components: []string{}}}
		for _, dir := range m.order {
			c := m.components[dir]
			if c.Counts.Files == 0 {
				continue
			}
			out.Components = append(out.Components, c)
			if c.HiddenBy != nil {
				switch *c.HiddenBy {
				case "tests":
					out.Hidden.TestComponents++
				case "docs":
					out.Hidden.DocsComponents++
				case "configured":
					out.Hidden.ConfiguredComponents++
				}
			}
			if c.State == componentUnread {
				out.NotProven = append(out.NotProven, archNotProven{Kind: "component_unread", Row: "CM-U1", Component: dir, Text: "kind unknown: manifest unread", Count: 1})
			}
			if c.Visibility == visibleFolded {
				out.Libraries.Components = append(out.Libraries.Components, dir)
			}
		}
		for _, l := range g.links {
			if l.Family != familyInProcess {
				continue
			}
			src, dst := m.components[l.Source], m.components[l.Target]
			if src.Visibility == visibleFolded && dst.Visibility != visibleFolded {
				out.Libraries.LinksOut += l.Count
				out.Libraries.LinksIn += l.BackCount
			}
			if dst.Visibility == visibleFolded && src.Visibility != visibleFolded {
				out.Libraries.LinksIn += l.Count
				out.Libraries.LinksOut += l.BackCount
			}
		}
		if px != nil {
			out.Project = px.doc
		}
		if out.ParseFailed, err = parseFailedFiles(ctx, rd.s, rd.repo); err != nil {
			return nil, err
		}
		out.NotProven = append(out.NotProven, g.notProven...)
		for _, np := range m.unmapped {
			if np.Kind == "service_not_mapped" {
				np.Database = m.tiedTo[np.identity]
			}
			out.NotProven = append(out.NotProven, np)
		}
		return out, nil
	}, nil
}

// GET /api/v0/architecture/component

type componentResponse struct {
	APIVersion int               `json:"api_version"`
	Repository string            `json:"repository"`
	Index      *lastRun          `json:"index"`
	Component  *archComponent    `json:"component"`
	Files      []componentFile   `json:"files"`
	Routes     []presentedNode   `json:"routes"`
	Calls      []presentedNode   `json:"calls"`
	Commands   []presentedNode   `json:"commands"`
	Tables     []presentedNode   `json:"tables"`
	Unlinked   map[string]int    `json:"unlinked"`
	External   []componentExtern `json:"external_packages"`
}

type componentFile struct {
	Path string `json:"path"`
	Test bool   `json:"test"`
}

// componentExtern is one imported external package (O2 (a): the list
// behind the component's single stub).
type componentExtern struct {
	Identity  string `json:"identity"`
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Imports   int    `json:"imports"`
}

func computeArchitectureComponent(h *apiHandler, q url.Values) (readFunc, error) {
	id, present, err := param(q, "id")
	if err != nil {
		return nil, err
	}
	if !present || id == "" {
		return nil, badRequest("parameter id is required: a component directory")
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		cfg, err := h.opt.loadConfig()
		if err != nil {
			return nil, badRequest("reading graphvein.yaml: %v", err)
		}
		m, err := buildComponents(rd, cfg)
		if err != nil {
			return nil, err
		}
		c, ok := m.components[id]
		if !ok || c.Counts.Files == 0 {
			return nil, &apiError{404, "not_found", "no component " + id}
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		p := h.newPresenter(hashes, editor)
		out := componentResponse{Repository: h.opt.Repository, Index: indexOf(runs), Component: c, Files: []componentFile{},
			Routes: []presentedNode{}, Calls: []presentedNode{}, Commands: []presentedNode{}, Tables: []presentedNode{}, Unlinked: map[string]int{}, External: []componentExtern{}}
		for _, f := range m.files {
			if m.fileOf[f] == id {
				out.Files = append(out.Files, componentFile{Path: f, Test: m.testFile[f]})
			}
		}
		lists := map[string]*[]presentedNode{"route": &out.Routes, "client_call": &out.Calls, "ipc_call": &out.Calls, "ipc_command": &out.Commands, "table": &out.Tables}
		for _, kind := range []string{"route", "client_call", "ipc_call", "ipc_command", "table"} {
			nodes := m.nodesByKind[kind]
			sort.Slice(nodes, func(i, j int) bool { return lessBySite(nodes[i], nodes[j]) })
			for _, n := range nodes {
				if m.componentOfFile(n.File) != id {
					continue
				}
				*lists[kind] = append(*lists[kind], p.node(n))
				if (kind == "client_call" || kind == "ipc_call") && n.Attributes["scope"] != "test" && unlinkedStatus(n) {
					out.Unlinked[statusOf(n)]++
				}
			}
		}
		ext, err := externalImports(rd, m, scopeProduction, nil)
		if err != nil {
			return nil, err
		}
		out.External = ext[id]
		if out.External == nil {
			out.External = []componentExtern{}
		}
		return out, nil
	}, nil
}
