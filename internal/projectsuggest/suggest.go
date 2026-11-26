package projectsuggest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// Evidence is one value read from one file.
type Evidence struct {
	// Row is the read row that produced it.
	Row string `json:"row"`
	// Member is the repository whose checkout holds the file, or empty for
	// a Compose file in the project folder.
	Member string `json:"member"`
	// File is slash-separated and relative to the member's checkout (or to
	// the project folder when Member is empty).
	File string `json:"file"`
	Line int    `json:"line"`
	// Key names what was read: an env key, a proxy path, or a Compose
	// service and field.
	Key string `json:"key"`
	// Value is the value read: a URL without credentials or query, or a
	// port. Secrets are never read, so never shown.
	Value string `json:"value"`
}

// Selector is a proposed `select`, in the manifest's field names.
type Selector struct {
	Bases        []string `json:"bases,omitempty"`
	Origins      []string `json:"origins,omitempty"`
	PathPrefixes []string `json:"path_prefixes,omitempty"`
}

// Proposal is a `consumes` entry the evidence supports. It changes nothing
// until accepted.
type Proposal struct {
	// Number is the proposal's 1-based position, the N of --accept N.
	Number int `json:"number"`
	// Matches are the match rows that made it (RowMatchLocal,
	// RowMatchService).
	Matches []string `json:"matches"`
	// Member (and Scope, when the evidence lies in a declared scope) is
	// the consumer whose consumes list gains the entry.
	Member string `json:"member"`
	Scope  string `json:"scope,omitempty"`
	// Provider (and ProviderScope) is the entry's repository (and scope).
	Provider      string `json:"provider"`
	ProviderScope string `json:"provider_scope,omitempty"`
	// Select is empty for a proxy that forwards every path (Create React
	// App): the entry then covers every call without a literal origin.
	Select Selector `json:"select"`
	// Evidence: the consumer's addresses, then the provider's ports.
	Evidence []Evidence `json:"evidence"`
	// Declared is set when the consumer already has a consumes entry for
	// this provider; accepting is refused (edit that entry instead), since
	// the manifest allows one entry per provider.
	Declared bool `json:"declared"`
}

// Unmatched is an address read without a proposal, with the reason.
type Unmatched struct {
	Row      string   `json:"row"`
	Evidence Evidence `json:"evidence"`
	Reason   string   `json:"reason"`
}

// Unread is a form the readers recognised the file for but did not read
// (RowUnread), with the reason. It carries no value: an unread value may
// be a secret.
type Unread struct {
	Row    string `json:"row"`
	Member string `json:"member"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Result is everything Suggest found, in a stable order. It is the
// structured data `graphvein project suggest --json` prints and the B10
// UI reads.
type Result struct {
	Proposals []Proposal  `json:"proposals"`
	Unmatched []Unmatched `json:"unmatched"`
	Unread    []Unread    `json:"unread"`
}

// maxFileSize bounds every file a reader opens.
const maxFileSize = 1 << 20

// skippedDirs are never walked: dependencies, build output, VCS data and
// Graphvein's own store.
var skippedDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"out": true, "coverage": true, "__pycache__": true,
}

// address is a URL a member calls, read from one file.
type address struct {
	consumer *config.Member
	scope    string
	evidence Evidence
	scheme   string
	host     string
	port     int
	origin   string
	// base is the env key that holds the URL (RowEnvURL,
	// RowComposeEnvironment); prefixes are proxy paths; neither for a
	// proxy forwarding every path.
	base     string
	prefixes []string
	// composeFile identifies the Compose file of a RowComposeEnvironment
	// address, so service names only match inside it.
	composeFile string
	// dir is the folder of a RowEnvURL address's env file, where define
	// constants reading its key are looked for.
	dir string
}

// listen is a port a member declares.
type listen struct {
	member   *config.Member
	scope    string
	evidence Evidence
	port     int
	// local: reachable on the loopback host (published Compose port, env
	// port); otherwise a container port of service in composeFile.
	local       bool
	service     string
	composeFile string
}

// state collects facts across members.
type state struct {
	project   config.Project
	addresses []address
	listens   []listen
	unread    []Unread
	scripts   []scriptJob
	defines   []define
	services  []service
	calls     Calls
}

// Calls are the members' stored indexes as the project link pass reads
// them. A proposal with a `bases` or `origins` selector is made only when
// the consumer has a stored call the selector covers (row RowNoCalls), so
// the selector names what the calls record. The zero value holds no index.
type Calls struct {
	members map[string]*projectlink.Member
}

// ReadCalls reads every member's stored index of p from st (only reads).
func ReadCalls(ctx context.Context, st store.Store, p config.Project) (Calls, error) {
	in, err := projectlink.Read(ctx, st, p, "")
	if err != nil {
		return Calls{}, err
	}
	c := Calls{members: map[string]*projectlink.Member{}}
	for i := range in.Members {
		m := &in.Members[i]
		if m.Run != "" {
			c.members[m.Repository] = m
		}
	}
	return c, nil
}

// Suggest reads every member checkout of p (and Compose files directly in
// the project folder) and returns the proposals, checked against the
// consumers' stored calls. It reads files and calls only.
func Suggest(ctx context.Context, p config.Project, calls Calls) (Result, error) {
	s := &state{project: p, calls: calls}
	for i := range p.Members {
		m := &p.Members[i]
		if !m.Checkout {
			continue
		}
		if err := s.walkMember(m); err != nil {
			return Result{}, err
		}
	}
	if !s.insideMember(p.Dir) {
		if err := s.readProjectDir(); err != nil {
			return Result{}, err
		}
	}
	if err := s.readScripts(ctx); err != nil {
		return Result{}, err
	}
	return s.result(), nil
}

// insideMember reports whether dir is a member checkout or inside one.
func (s *state) insideMember(dir string) bool {
	return s.memberFor(dir) != nil
}

// memberFor returns the member with a checkout whose directory is dir or
// the deepest one containing it.
func (s *state) memberFor(dir string) *config.Member {
	var best *config.Member
	for i := range s.project.Members {
		m := &s.project.Members[i]
		if !m.Checkout || !within(m.Path, dir) {
			continue
		}
		if best == nil || len(m.Path) > len(best.Path) {
			best = m
		}
	}
	return best
}

// within reports whether path is dir or inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// scopeOf returns the declared scope of m whose folder holds path.
func scopeOf(m *config.Member, path string) string {
	for _, sc := range m.Scopes {
		if within(filepath.Join(m.Path, filepath.FromSlash(sc.Path)), path) {
			return sc.Name
		}
	}
	return ""
}

func (s *state) walkMember(m *config.Member) error {
	err := filepath.WalkDir(m.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.unread = append(s.unread, Unread{RowUnread, m.Repository, s.rel(m, path), 0, fmt.Sprintf("not readable: %v", err)})
			if d != nil && d.IsDir() && path != m.Path {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == m.Path {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || skippedDirs[name] {
				return fs.SkipDir
			}
			// A nested member is read as that member.
			if s.memberFor(path) != m {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		s.readFile(m, path, d.Name())
		return nil
	})
	if err != nil {
		return fmt.Errorf("reading member %s: %w", m.Repository, err)
	}
	return nil
}

// readProjectDir reads the Compose files directly in the project folder
// (a folder holding the checkouts often holds the Compose file that
// builds them).
func (s *state) readProjectDir() error {
	entries, err := os.ReadDir(s.project.Dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.project.Dir, err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() && isComposeFile(e.Name()) {
			path := filepath.Join(s.project.Dir, e.Name())
			if data, ok := s.load(nil, path); ok {
				s.readCompose(nil, path, data)
			}
		}
	}
	return nil
}

// readFile dispatches one file to its reader by name.
func (s *state) readFile(m *config.Member, path, name string) {
	kind := ""
	switch {
	case isEnvFile(name):
		kind = "env"
	case isComposeFile(name):
		kind = "compose"
	case name == "package.json":
		kind = "package"
	case scriptLanguage(name) != "" && (isViteConfig(name) || isWebpackConfig(name)):
		kind = "script"
	default:
		return
	}
	data, ok := s.load(m, path)
	if !ok {
		return
	}
	switch kind {
	case "env":
		s.readEnv(m, path, data)
	case "compose":
		s.readCompose(m, path, data)
	case "package":
		s.readPackage(m, path, data)
	case "script":
		s.scripts = append(s.scripts, scriptJob{member: m, path: path, name: name, data: data})
	}
}

// load reads a file within the size bound; a failure is an unread entry.
func (s *state) load(m *config.Member, path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err == nil && info.Size() > maxFileSize {
		s.unreadAt(m, path, 0, fmt.Sprintf("larger than %d bytes", maxFileSize))
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.unreadAt(m, path, 0, fmt.Sprintf("not readable: %v", err))
		return nil, false
	}
	return data, true
}

func (s *state) unreadAt(m *config.Member, path string, line int, reason string) {
	name := ""
	if m != nil {
		name = m.Repository
	}
	s.unread = append(s.unread, Unread{RowUnread, name, s.rel(m, path), line, reason})
}

// rel is path relative to the member's checkout (or the project folder).
func (s *state) rel(m *config.Member, path string) string {
	base := s.project.Dir
	if m != nil {
		base = m.Path
	}
	r, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

func (s *state) evidence(m *config.Member, row, path string, line int, key, value string) Evidence {
	name := ""
	if m != nil {
		name = m.Repository
	}
	return Evidence{Row: row, Member: name, File: s.rel(m, path), Line: line, Key: key, Value: value}
}

// addressKind orders and separates proposals for one provider: the
// manifest allows one entry per provider, and an entry's path_prefixes
// apply to its bases too, so the kinds are never merged.
type addressKind int

const (
	kindBase addressKind = iota
	kindPath
	kindAll
)

func (a address) kind() addressKind {
	switch {
	case a.base != "":
		return kindBase
	case len(a.prefixes) > 0:
		return kindPath
	}
	return kindAll
}

// isLoopback reports whether host is the local machine as a browser or
// dev server reaches it.
func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}

// result matches every address against the listens (rows SG-M1 to SG-M3)
// and groups the proposals.
func (s *state) result() Result {
	type key struct {
		member, scope, provider, providerScope string
		kind                                   addressKind
	}
	groups := map[key]*Proposal{}
	providerEvidence := map[key][]Evidence{}
	var order []key
	var unmatched []Unmatched
	sort.SliceStable(s.addresses, func(i, j int) bool { return evidenceLess(s.addresses[i].evidence, s.addresses[j].evidence) })
	for _, a := range s.addresses {
		row, matches, reason := s.match(a)
		if row == RowNoMatch {
			unmatched = append(unmatched, Unmatched{Row: RowNoMatch, Evidence: a.evidence, Reason: reason})
			continue
		}
		var sel Selector
		var used []define
		if a.kind() == kindBase {
			if sel, used, reason = s.covered(a); reason != "" {
				unmatched = append(unmatched, Unmatched{Row: RowNoCalls, Evidence: a.evidence, Reason: reason})
				continue
			}
		}
		provider := matches[0]
		k := key{a.consumer.Repository, a.scope, provider.member.Repository, provider.scope, a.kind()}
		p := groups[k]
		if p == nil {
			p = &Proposal{Member: k.member, Scope: k.scope, Provider: k.provider, ProviderScope: k.providerScope}
			groups[k] = p
			order = append(order, k)
		}
		p.Matches = appendNew(p.Matches, row)
		p.Evidence = append(p.Evidence, a.evidence)
		for _, d := range used {
			p.Evidence = append(p.Evidence, d.evidence)
		}
		for _, l := range matches {
			providerEvidence[k] = append(providerEvidence[k], l.evidence)
		}
		for _, b := range sel.Bases {
			p.Select.Bases = appendNew(p.Select.Bases, b)
		}
		for _, o := range sel.Origins {
			p.Select.Origins = appendNew(p.Select.Origins, o)
		}
		for _, prefix := range a.prefixes {
			p.Select.PathPrefixes = appendNew(p.Select.PathPrefixes, prefix)
		}
	}
	memberIndex := map[string]int{}
	for i, m := range s.project.Members {
		memberIndex[m.Repository] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		switch {
		case a.member != b.member:
			return memberIndex[a.member] < memberIndex[b.member]
		case a.scope != b.scope:
			return a.scope < b.scope
		case a.provider != b.provider:
			return memberIndex[a.provider] < memberIndex[b.provider]
		case a.providerScope != b.providerScope:
			return a.providerScope < b.providerScope
		}
		return a.kind < b.kind
	})
	out := Result{Proposals: []Proposal{}, Unmatched: unmatched, Unread: s.unread}
	for i, k := range order {
		p := groups[k]
		p.Number = i + 1
		sort.Strings(p.Matches)
		// The consumer's addresses (in file order), then the provider's
		// ports.
		listens := providerEvidence[k]
		sort.SliceStable(listens, func(i, j int) bool { return evidenceLess(listens[i], listens[j]) })
		p.Evidence = dedupeEvidence(append(p.Evidence, listens...))
		p.Declared = s.declared(*p)
		out.Proposals = append(out.Proposals, *p)
	}
	if out.Unmatched == nil {
		out.Unmatched = []Unmatched{}
	}
	sort.SliceStable(out.Unread, func(i, j int) bool {
		a, b := out.Unread[i], out.Unread[j]
		if a.Member != b.Member {
			return memberIndex[a.Member] < memberIndex[b.Member]
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	if out.Unread == nil {
		out.Unread = []Unread{}
	}
	return out
}

// match applies rows SG-M1 to SG-M3 to one address: the listens of other
// members it reaches, or the reason it reaches none.
func (s *state) match(a address) (string, []listen, string) {
	var matches []listen
	self := false
	named := !isLoopback(a.host)
	if named && a.composeFile == "" {
		return s.matchServiceName(a)
	}
	for _, l := range s.listens {
		var hit bool
		if named {
			hit = !l.local && l.composeFile == a.composeFile && l.service == a.host && l.port == a.port
		} else {
			hit = l.local && l.port == a.port
		}
		if !hit {
			continue
		}
		if l.member == a.consumer {
			self = true
			continue
		}
		matches = append(matches, l)
	}
	providers := map[string]bool{}
	var names []string
	for _, l := range matches {
		id := l.member.Repository
		if l.scope != "" {
			id += "/" + l.scope
		}
		if !providers[id] {
			providers[id] = true
			names = append(names, id)
		}
	}
	switch {
	case len(names) == 1 && named:
		return RowMatchService, matches, ""
	case len(names) == 1:
		return RowMatchLocal, matches, ""
	case len(names) > 1:
		sort.Strings(names)
		return RowNoMatch, nil, fmt.Sprintf("several members declare %s: %s", where(a, named), strings.Join(names, ", "))
	case self:
		return RowNoMatch, nil, fmt.Sprintf("only the member itself declares %s", where(a, named))
	}
	return RowNoMatch, nil, fmt.Sprintf("no other member declares %s", where(a, named))
}

// matchServiceName applies row RowMatchServiceName to an address read
// outside a Compose file whose host is not the local machine: it names a
// Compose service only when every service of that name in the Compose
// files of the address's own member is built from one other member (or
// one scope of it), and such a service declares the address's port as a
// container port. A service of that name built from nothing known (an
// image, an unread service) leaves the name unproven.
func (s *state) matchServiceName(a address) (string, []listen, string) {
	files := map[string]bool{}
	type endpoint struct {
		member *config.Member
		scope  string
	}
	builders := map[endpoint]bool{}
	var names []string
	for _, sv := range s.services {
		if sv.owner != a.consumer || sv.name != a.host {
			continue
		}
		if sv.member == nil {
			return RowNoMatch, nil, fmt.Sprintf("Compose service %s (%s:%d) %s, so the name is not proven to reach a member", a.host, s.rel(sv.owner, sv.file), sv.line, sv.reason)
		}
		files[sv.file] = true
		e := endpoint{sv.member, sv.scope}
		if !builders[e] {
			builders[e] = true
			id := sv.member.Repository
			if sv.scope != "" {
				id += "/" + sv.scope
			}
			names = append(names, id)
		}
	}
	switch {
	case len(files) == 0:
		return RowNoMatch, nil, fmt.Sprintf("host %s is not the local machine and names no Compose service of %s (a public name is never mapped to a member)", a.host, a.consumer.Repository)
	case len(names) > 1:
		sort.Strings(names)
		return RowNoMatch, nil, fmt.Sprintf("Compose services named %s are built from several members: %s", a.host, strings.Join(names, ", "))
	}
	var matches []listen
	for _, l := range s.listens {
		if !l.local && files[l.composeFile] && l.service == a.host && l.port == a.port {
			matches = append(matches, l)
		}
	}
	switch {
	case len(matches) == 0:
		return RowNoMatch, nil, fmt.Sprintf("Compose service %s of %s declares no container port %d", a.host, names[0], a.port)
	case matches[0].member == a.consumer:
		return RowNoMatch, nil, fmt.Sprintf("only the member itself builds Compose service %s", a.host)
	}
	return RowMatchServiceName, matches, ""
}

// covered applies row RowNoCalls to a matched address with a base: of the
// base, the define constants reading it (RowDefineEnv) and its origin, it
// keeps those a stored call of the consumer (in the address's scope)
// records, so the selector names what the calls record. It returns the
// reason when none does.
func (s *state) covered(a address) (Selector, []define, string) {
	consumer := a.consumer.Repository
	if a.scope != "" {
		consumer += "/" + a.scope
	}
	m := s.calls.members[a.consumer.Repository]
	if m == nil {
		return Selector{}, nil, fmt.Sprintf("%s has no stored index, so no call is known to use this address; run graphvein index --project, then suggest again", a.consumer.Repository)
	}
	aliases := s.aliases(a)
	names := []string{a.base}
	for _, d := range aliases {
		names = appendNew(names, d.name)
	}
	var sel Selector
	var used []define
	for _, b := range names {
		if projectlink.CoveredCalls(m, a.scope, config.Consume{Select: config.Selector{Bases: []string{b}}}) == 0 {
			continue
		}
		sel.Bases = append(sel.Bases, b)
		for _, d := range aliases {
			if d.name == b {
				used = append(used, d)
			}
		}
	}
	if projectlink.CoveredCalls(m, a.scope, config.Consume{Select: config.Selector{Origins: []string{a.origin}}}) > 0 {
		sel.Origins = []string{a.origin}
	}
	if len(sel.Bases) == 0 && len(sel.Origins) == 0 {
		return sel, nil, fmt.Sprintf("no calls use this base: no stored call of %s records base %s or literal origin %s", consumer, strings.Join(names, " or "), a.origin)
	}
	return sel, used, ""
}

func where(a address, named bool) string {
	if named {
		return fmt.Sprintf("Compose service %s with container port %d in this file", a.host, a.port)
	}
	return fmt.Sprintf("local port %d (published Compose port or env port)", a.port)
}

// declared reports whether the consumer already has an entry for the
// provider.
func (s *state) declared(p Proposal) bool {
	for _, m := range s.project.Members {
		if m.Repository != p.Member {
			continue
		}
		list := m.Consumes
		if p.Scope != "" {
			list = nil
			for _, sc := range m.Scopes {
				if sc.Name == p.Scope {
					list = sc.Consumes
				}
			}
		}
		for _, c := range list {
			if c.Repository == p.Provider && c.Scope == p.ProviderScope {
				return true
			}
		}
	}
	return false
}

// Consume returns the manifest entry p proposes.
func (p Proposal) Consume() config.Consume {
	return config.Consume{Repository: p.Provider, Scope: p.ProviderScope, Select: config.Selector{
		Bases: p.Select.Bases, Origins: p.Select.Origins, PathPrefixes: p.Select.PathPrefixes,
	}}
}

func evidenceLess(a, b Evidence) bool {
	if a.Member != b.Member {
		return a.Member < b.Member
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Key < b.Key
}

func dedupeEvidence(list []Evidence) []Evidence {
	seen := map[Evidence]bool{}
	var out []Evidence
	for _, e := range list {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func appendNew(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// parsePort reads a decimal port 1-65535.
func parsePort(s string) (int, bool) {
	if s == "" || len(s) > 5 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= 65535
}
