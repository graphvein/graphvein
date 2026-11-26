package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ProjectFile is the committed project manifest (ADR 0075). One format is
// used whether it sits in a separate project folder or inside one member.
const ProjectFile = "graphvein-project.yaml"

// ProjectLocalFile holds machine-specific member paths next to the
// manifest. It is never committed, so a teammate's checkout layout does not
// leak into the shared manifest.
const ProjectLocalFile = "graphvein-project.local.yaml"

// Project is a validated project manifest with every member resolved. It
// declares which repositories belong together and, optionally, which of
// them may serve which calls; it never changes any member's identities.
type Project struct {
	// Name is the project's logical name (same rules as a repository ID).
	Name string
	// Dir is the absolute directory holding the manifest; member paths are
	// relative to it.
	Dir string
	// Members are in manifest order.
	Members []Member
	// TryIt holds the project-wide "try it" environments (T029): one name
	// and label, a base URL per member. Members' own graphvein.yaml
	// environments apply too.
	TryIt []ProjectEnvironment
}

// Member is one repository of a project.
type Member struct {
	// Repository is the member's repository ID. It is always set in the
	// manifest; when the member's own graphvein.yaml names a repository,
	// the two must be equal (ADR 0075 identity rule 4).
	Repository string
	// RepositorySource says where the ID was confirmed: RepositoryFromManifest
	// or RepositoryFromMemberConfig.
	RepositorySource string
	// Path is the member's absolute, cleaned checkout directory, or empty
	// when neither the manifest nor the local file gives one (a member
	// whose facts come from a shared store).
	Path string
	// PathSource is PathFromManifest, PathFromLocal or empty.
	PathSource string
	// Checkout reports whether Path exists as a directory. A missing
	// checkout is not an error: stored facts are enough to link.
	Checkout bool
	// Consumes narrows the providers this member's calls (outside any
	// declared scope) may link to. Empty means every member is a candidate.
	Consumes []Consume
	// Scopes are the member's declared link scopes, in manifest order.
	Scopes []Scope
}

// Where a member's repository ID and path come from.
const (
	RepositoryFromManifest     = "manifest"
	RepositoryFromMemberConfig = "member graphvein.yaml"
	PathFromManifest           = "manifest"
	PathFromLocal              = "local file"
)

// Consume names one provider a member or scope declares it calls. Scope is
// empty for the provider's whole repository.
type Consume struct {
	Repository string
	Scope      string
	// DefaultScope names only the provider's default scope (its routes
	// outside every declared scope), written `default_scope: true`. An empty
	// Scope cannot say this: it already means the whole repository. It
	// is never set together with Scope.
	DefaultScope bool
	Select       Selector
	// line is the manifest line of the entry, for error messages.
	line int
}

// Selector says which calls a consume entry covers. An empty selector
// covers every call without a literal origin; docs/extract/project-links.md
// ("Candidate providers") says how the project link pass matches the
// lists. Lists are kept in manifest order.
type Selector struct {
	// Bases are base expressions or environment member names as the
	// clients extractor records them (for example __API_URL__).
	Bases []string
	// Origins are literal http(s) origins such as http://localhost:3000.
	Origins []string
	// PathPrefixes are URL path prefixes such as /api.
	PathPrefixes []string
}

// Empty reports whether the selector names nothing, so it covers every call.
func (s Selector) Empty() bool {
	return len(s.Bases) == 0 && len(s.Origins) == 0 && len(s.PathPrefixes) == 0
}

// Scope is a link scope: a named folder inside a member, so several servers
// inside one repository can be told apart (ADR 0075, P10).
type Scope struct {
	Name string
	// Path is slash-separated and relative to the member's root.
	Path     string
	Consumes []Consume
}

// LoadProject reads dir/graphvein-project.yaml and, when present,
// dir/graphvein-project.local.yaml, validates both and resolves every
// member. Like Load it reads no parent directory.
func LoadProject(dir string) (Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Project{}, fmt.Errorf("resolving %s: %w", dir, err)
	}
	manifest := filepath.Join(abs, ProjectFile)
	data, err := os.ReadFile(manifest)
	if err != nil {
		return Project{}, fmt.Errorf("reading %s: %w", manifest, err)
	}
	localPath := filepath.Join(abs, ProjectLocalFile)
	local, err := os.ReadFile(localPath)
	if errors.Is(err, fs.ErrNotExist) {
		local = nil
	} else if err != nil {
		return Project{}, fmt.Errorf("reading %s: %w", localPath, err)
	}
	return ParseProject(abs, data, local)
}

// ParseProject validates manifest (and local, which may be nil) as if they
// were the files in the absolute directory dir. The project CLI uses it to
// validate a new manifest in full before writing it.
func ParseProject(dir string, manifest, local []byte) (Project, error) {
	manifestPath := filepath.Join(dir, ProjectFile)
	p, err := parseManifest(manifestPath, manifest)
	if err != nil {
		return Project{}, err
	}
	p.Dir = dir
	if local != nil {
		if err := applyLocal(&p, filepath.Join(dir, ProjectLocalFile), local); err != nil {
			return Project{}, err
		}
	}
	if err := resolveMembers(&p, manifestPath); err != nil {
		return Project{}, err
	}
	return p, nil
}

// yamlError is a validation error at a line of one file.
type yamlError struct {
	file string
	line int
	msg  string
}

func (e *yamlError) Error() string {
	if e.line == 0 {
		return fmt.Sprintf("%s: %s", e.file, e.msg)
	}
	return fmt.Sprintf("%s: line %d: %s", e.file, e.line, e.msg)
}

// decodeSingleMapping parses data as exactly one YAML document whose root
// is a mapping, refusing aliases anywhere (they make review of a shared
// file harder and are never needed here).
func decodeSingleMapping(file string, data []byte) (*yaml.Node, error) {
	d := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := d.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, &yamlError{file, 0, "file is empty"}
		}
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		return nil, &yamlError{file, extra.Line, "multiple YAML documents are not allowed"}
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &yamlError{file, doc.Line, "expected a mapping"}
	}
	if line := aliasLine(&doc); line != 0 {
		return nil, &yamlError{file, line, "YAML aliases and anchors are not allowed"}
	}
	return doc.Content[0], nil
}

// aliasLine returns the line of the first alias or anchor under n, or 0.
func aliasLine(n *yaml.Node) int {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return n.Line
	}
	for _, c := range n.Content {
		if line := aliasLine(c); line != 0 {
			return line
		}
	}
	return 0
}

// mappingEntries returns the key/value pairs of a mapping, refusing
// non-string and duplicate keys and keys not in allowed.
func mappingEntries(file string, m *yaml.Node, what string, allowed ...string) ([][2]*yaml.Node, error) {
	if m.Kind != yaml.MappingNode {
		return nil, &yamlError{file, m.Line, what + " must be a mapping"}
	}
	seen := map[string]bool{}
	var out [][2]*yaml.Node
	for i := 0; i < len(m.Content); i += 2 {
		key, value := m.Content[i], m.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, &yamlError{file, key.Line, "keys must be strings"}
		}
		if seen[key.Value] {
			return nil, &yamlError{file, key.Line, "duplicate key " + key.Value}
		}
		seen[key.Value] = true
		known := false
		for _, a := range allowed {
			known = known || a == key.Value
		}
		if !known && what == "members" {
			return nil, &yamlError{file, key.Line, "repository " + key.Value + " is not a member of the manifest"}
		}
		if !known {
			return nil, &yamlError{file, key.Line, "unknown key " + key.Value + " in " + what}
		}
		out = append(out, [2]*yaml.Node{key, value})
	}
	return out, nil
}

// idProblem returns why s is not a valid repository or project ID, or "".
func idProblem(s string) string {
	if s == "" || strings.ContainsRune(s, 0) {
		return "must be a nonempty string without NUL"
	}
	if strings.Contains(s, RefMarker) {
		return "must not contain " + RefMarker + " (reserved for ref snapshots)"
	}
	return ""
}

// stringValue returns a string scalar or an error naming what.
func stringValue(file string, n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", &yamlError{file, n.Line, what + " must be a string"}
	}
	return n.Value, nil
}

func parseManifest(file string, data []byte) (Project, error) {
	root, err := decodeSingleMapping(file, data)
	if err != nil {
		return Project{}, err
	}
	entries, err := mappingEntries(file, root, "the manifest", "version", "name", "members", "tryit")
	if err != nil {
		return Project{}, err
	}
	var p Project
	var membersNode, tryitNode *yaml.Node
	seen := map[string]bool{}
	for _, e := range entries {
		key, value := e[0], e[1]
		seen[key.Value] = true
		switch key.Value {
		case "version":
			var v int
			if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Decode(&v) != nil || v != 1 {
				return Project{}, &yamlError{file, value.Line, "version must be integer 1"}
			}
		case "name":
			s, err := stringValue(file, value, "name")
			if err != nil {
				return Project{}, err
			}
			if problem := idProblem(s); problem != "" {
				return Project{}, &yamlError{file, value.Line, "name " + problem}
			}
			p.Name = s
		case "members":
			membersNode = value
		case "tryit":
			tryitNode = value
		}
	}
	for _, required := range []string{"version", "name", "members"} {
		if !seen[required] {
			return Project{}, &yamlError{file, root.Line, required + " is required"}
		}
	}
	if membersNode.Kind != yaml.SequenceNode || len(membersNode.Content) == 0 {
		return Project{}, &yamlError{file, membersNode.Line, "members must be a nonempty list"}
	}
	byRepository := map[string]bool{}
	for _, n := range membersNode.Content {
		m, err := parseMember(file, n)
		if err != nil {
			return Project{}, err
		}
		if byRepository[m.Repository] {
			return Project{}, &yamlError{file, n.Line, "repository " + m.Repository + " is listed twice"}
		}
		byRepository[m.Repository] = true
		p.Members = append(p.Members, m)
	}
	if err := checkConsumes(file, p.Members); err != nil {
		return Project{}, err
	}
	if tryitNode != nil {
		envs, err := decodeProjectTryIt(tryitNode)
		var le *lineError
		if errors.As(err, &le) {
			return Project{}, &yamlError{file, le.line, le.msg}
		}
		if err != nil {
			return Project{}, err
		}
		for _, pe := range envs {
			for _, m := range pe.BaseURLs {
				if !byRepository[m.Repository] {
					return Project{}, &yamlError{file, pe.Line, "tryit environment " + pe.Name + " names " + m.Repository + ", which is not a member"}
				}
			}
		}
		p.TryIt = envs
	}
	return p, nil
}

func parseMember(file string, n *yaml.Node) (Member, error) {
	entries, err := mappingEntries(file, n, "a member", "repository", "path", "consumes", "scopes")
	if err != nil {
		return Member{}, err
	}
	var m Member
	for _, e := range entries {
		key, value := e[0], e[1]
		switch key.Value {
		case "repository":
			s, err := stringValue(file, value, "repository")
			if err != nil {
				return Member{}, err
			}
			if problem := idProblem(s); problem != "" {
				return Member{}, &yamlError{file, value.Line, "repository " + problem}
			}
			m.Repository = s
		case "path":
			s, err := stringValue(file, value, "path")
			if err != nil {
				return Member{}, err
			}
			if problem := manifestPathProblem(s); problem != "" {
				return Member{}, &yamlError{file, value.Line, "path " + problem}
			}
			m.Path, m.PathSource = s, PathFromManifest
		case "consumes":
			if m.Consumes, err = parseConsumes(file, value); err != nil {
				return Member{}, err
			}
		case "scopes":
			if m.Scopes, err = parseScopes(file, value); err != nil {
				return Member{}, err
			}
		}
	}
	if m.Repository == "" {
		return Member{}, &yamlError{file, n.Line, "member repository is required (the directory name is never used)"}
	}
	return m, nil
}

// manifestPathProblem refuses paths that only work on one machine or one
// operating system: the manifest is committed and shared.
func manifestPathProblem(s string) string {
	switch {
	case s == "" || strings.ContainsRune(s, 0):
		return "must be a nonempty string without NUL"
	case strings.Contains(s, `\`):
		return "must use forward slashes"
	case strings.HasPrefix(s, "/") || filepath.IsAbs(s) || hasDriveLetter(s):
		return "must be relative to the manifest; put machine-specific paths in " + ProjectLocalFile
	}
	return ""
}

// hasDriveLetter reports a Windows volume prefix such as C: on any OS.
func hasDriveLetter(s string) bool {
	return len(s) >= 2 && s[1] == ':' && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z')
}

func parseConsumes(file string, n *yaml.Node) ([]Consume, error) {
	if n.Kind != yaml.SequenceNode || len(n.Content) == 0 {
		return nil, &yamlError{file, n.Line, "consumes must be a nonempty list"}
	}
	var out []Consume
	type target struct {
		repository, scope string
		defaultScope      bool
	}
	seen := map[target]bool{}
	for _, item := range n.Content {
		c := Consume{line: item.Line}
		if item.Kind == yaml.ScalarNode {
			s, err := stringValue(file, item, "a consumes entry")
			if err != nil {
				return nil, err
			}
			c.Repository = s
		} else {
			entries, err := mappingEntries(file, item, "a consumes entry", "repository", "scope", "default_scope", "select")
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				key, value := e[0], e[1]
				switch key.Value {
				case "repository":
					if c.Repository, err = stringValue(file, value, "repository"); err != nil {
						return nil, err
					}
				case "scope":
					if c.Scope, err = stringValue(file, value, "scope"); err != nil {
						return nil, err
					}
					if c.Scope == "" {
						return nil, &yamlError{file, value.Line, "scope must be nonempty"}
					}
				case "default_scope":
					if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" || value.Value != "true" {
						return nil, &yamlError{file, value.Line, "default_scope must be true (leave it out to name the whole repository)"}
					}
					c.DefaultScope = true
				case "select":
					if c.Select, err = parseSelector(file, value); err != nil {
						return nil, err
					}
				}
			}
			if c.DefaultScope && c.Scope != "" {
				return nil, &yamlError{file, item.Line, "a consumes entry names either scope or default_scope, not both"}
			}
		}
		if problem := idProblem(c.Repository); problem != "" {
			return nil, &yamlError{file, item.Line, "consumes repository " + problem}
		}
		key := target{c.Repository, c.Scope, c.DefaultScope}
		if seen[key] {
			return nil, &yamlError{file, item.Line, "consumes names " + consumeName(c) + " twice"}
		}
		seen[key] = true
		out = append(out, c)
	}
	return out, nil
}

// consumeName renders a consume target for messages.
func consumeName(c Consume) string {
	if c.DefaultScope {
		return c.Repository + " default scope"
	}
	if c.Scope == "" {
		return c.Repository
	}
	return c.Repository + " scope " + c.Scope
}

func parseSelector(file string, n *yaml.Node) (Selector, error) {
	entries, err := mappingEntries(file, n, "select", "bases", "origins", "path_prefixes")
	if err != nil {
		return Selector{}, err
	}
	var s Selector
	for _, e := range entries {
		key, value := e[0], e[1]
		if value.Kind != yaml.SequenceNode || len(value.Content) == 0 {
			return Selector{}, &yamlError{file, value.Line, key.Value + " must be a nonempty list of strings"}
		}
		for _, item := range value.Content {
			v, err := stringValue(file, item, "each of "+key.Value)
			if err != nil {
				return Selector{}, err
			}
			var problem string
			switch key.Value {
			case "bases":
				if v == "" || strings.ContainsAny(v, " \t\r\n\x00") {
					problem = "base must be nonempty without whitespace"
				}
				s.Bases = append(s.Bases, v)
			case "origins":
				problem = originProblem(v)
				s.Origins = append(s.Origins, v)
			case "path_prefixes":
				if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, "?# \t\r\n\x00") {
					problem = "path prefix must start with / and contain no query, fragment or whitespace"
				}
				s.PathPrefixes = append(s.PathPrefixes, v)
			}
			if problem != "" {
				return Selector{}, &yamlError{file, item.Line, fmt.Sprintf("%s (%q)", problem, v)}
			}
		}
	}
	if s.Empty() {
		return Selector{}, &yamlError{file, n.Line, "select must name bases, origins or path_prefixes"}
	}
	return s, nil
}

// originProblem accepts exactly scheme://host[:port] with an http(s)
// scheme, so a selector can only name an origin a call can spell.
func originProblem(v string) string {
	const msg = "origin must be http(s)://host[:port] without path, query or user"
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.HasSuffix(v, "?") || strings.HasSuffix(v, "#") {
		return msg
	}
	if strings.ToLower(v) != v {
		return "origin must be lower case"
	}
	return ""
}

func parseScopes(file string, n *yaml.Node) ([]Scope, error) {
	if n.Kind != yaml.SequenceNode || len(n.Content) == 0 {
		return nil, &yamlError{file, n.Line, "scopes must be a nonempty list"}
	}
	var out []Scope
	for _, item := range n.Content {
		entries, err := mappingEntries(file, item, "a scope", "name", "path", "consumes")
		if err != nil {
			return nil, err
		}
		var s Scope
		for _, e := range entries {
			key, value := e[0], e[1]
			switch key.Value {
			case "name":
				if s.Name, err = stringValue(file, value, "scope name"); err != nil {
					return nil, err
				}
				if !scopeNameValid(s.Name) {
					return nil, &yamlError{file, value.Line, "scope name must use only letters, digits, '.', '_' and '-'"}
				}
			case "path":
				if s.Path, err = stringValue(file, value, "scope path"); err != nil {
					return nil, err
				}
				if !scopePathValid(s.Path) {
					return nil, &yamlError{file, value.Line, "scope path must be a folder inside the member: relative, forward slashes, no '.' or '..' elements"}
				}
			case "consumes":
				if s.Consumes, err = parseConsumes(file, value); err != nil {
					return nil, err
				}
			}
		}
		if s.Name == "" || s.Path == "" {
			return nil, &yamlError{file, item.Line, "a scope needs name and path"}
		}
		for _, other := range out {
			if other.Name == s.Name {
				return nil, &yamlError{file, item.Line, "scope " + s.Name + " is declared twice"}
			}
			if pathWithin(s.Path, other.Path) || pathWithin(other.Path, s.Path) {
				return nil, &yamlError{file, item.Line, "scope " + s.Name + " overlaps scope " + other.Name + " (scopes may not nest)"}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func scopeNameValid(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// scopePathValid accepts a clean, relative, slash-separated folder path
// that stays inside the member and is portable to Windows.
func scopePathValid(s string) bool {
	return s != "." && fs.ValidPath(s) && !strings.ContainsAny(s, `\:`)
}

// pathWithin reports whether slash path p is dir or below it.
func pathWithin(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// checkConsumes resolves every consume entry against the members and their
// scopes, after all members are known.
func checkConsumes(file string, members []Member) error {
	scopes := map[string]map[string]bool{}
	for _, m := range members {
		scopes[m.Repository] = map[string]bool{}
		for _, s := range m.Scopes {
			scopes[m.Repository][s.Name] = true
		}
	}
	check := func(owner, ownerScope string, list []Consume) error {
		for _, c := range list {
			line := c.line
			known, ok := scopes[c.Repository]
			if !ok {
				return &yamlError{file, line, "consumes names " + c.Repository + ", which is not a member"}
			}
			if c.Scope != "" && !known[c.Scope] {
				return &yamlError{file, line, "consumes names scope " + c.Scope + ", which " + c.Repository + " does not declare"}
			}
			if c.DefaultScope && len(known) == 0 {
				return &yamlError{file, line, "consumes names the default scope of " + c.Repository + ", which declares no scopes (name the repository)"}
			}
			if c.Repository == owner && (c.Scope == ownerScope && !c.DefaultScope || c.DefaultScope && ownerScope == "") {
				return &yamlError{file, line, "a member or scope cannot consume itself"}
			}
		}
		return nil
	}
	for _, m := range members {
		if err := check(m.Repository, "", m.Consumes); err != nil {
			return err
		}
		for _, s := range m.Scopes {
			if err := check(m.Repository, s.Name, s.Consumes); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyLocal overrides member paths from the local file. It may only name
// members of the manifest and only set path; the path may be absolute.
func applyLocal(p *Project, file string, data []byte) error {
	root, err := decodeSingleMapping(file, data)
	if err != nil {
		return err
	}
	entries, err := mappingEntries(file, root, "the local file", "version", "members")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		key, value := e[0], e[1]
		seen[key.Value] = true
		switch key.Value {
		case "version":
			var v int
			if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Decode(&v) != nil || v != 1 {
				return &yamlError{file, value.Line, "version must be integer 1"}
			}
		case "members":
			if value.Kind != yaml.MappingNode {
				return &yamlError{file, value.Line, "members must be a mapping from repository to settings"}
			}
			members, err := mappingEntries(file, value, "members", memberNames(p)...)
			if err != nil {
				return err
			}
			for _, me := range members {
				settings, err := mappingEntries(file, me[1], "local member settings", "path")
				if err != nil {
					return err
				}
				for _, s := range settings {
					v, err := stringValue(file, s[1], "path")
					if err != nil {
						return err
					}
					if v == "" || strings.ContainsRune(v, 0) {
						return &yamlError{file, s[1].Line, "path must be a nonempty string without NUL"}
					}
					for i := range p.Members {
						if p.Members[i].Repository == me[0].Value {
							p.Members[i].Path, p.Members[i].PathSource = v, PathFromLocal
						}
					}
				}
			}
		}
	}
	if !seen["version"] {
		return &yamlError{file, root.Line, "version is required"}
	}
	return nil
}

func memberNames(p *Project) []string {
	var out []string
	for _, m := range p.Members {
		out = append(out, m.Repository)
	}
	return out
}

// resolveMembers turns member paths absolute, checks that no two members
// share a checkout, and confirms each name against the member's own
// graphvein.yaml when the checkout is present.
func resolveMembers(p *Project, manifestPath string) error {
	type seenDir struct {
		repository string
		info       fs.FileInfo
		path       string
	}
	var dirs []seenDir
	for i := range p.Members {
		m := &p.Members[i]
		m.RepositorySource = RepositoryFromManifest
		if m.Path == "" {
			continue
		}
		raw := m.Path
		if m.PathSource == PathFromManifest {
			raw = filepath.FromSlash(raw)
		}
		if !filepath.IsAbs(raw) {
			raw = filepath.Join(p.Dir, raw)
		}
		m.Path = filepath.Clean(raw)
		for _, d := range dirs {
			if d.path == m.Path {
				return fmt.Errorf("%s: members %s and %s use the same directory %s", manifestPath, d.repository, m.Repository, m.Path)
			}
		}
		info, err := os.Stat(m.Path)
		if errors.Is(err, fs.ErrNotExist) {
			dirs = append(dirs, seenDir{m.Repository, nil, m.Path})
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: member %s: %w", manifestPath, m.Repository, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s: member %s: %s is not a directory", manifestPath, m.Repository, m.Path)
		}
		m.Checkout = true
		for _, d := range dirs {
			if d.info != nil && os.SameFile(d.info, info) {
				return fmt.Errorf("%s: members %s and %s use the same directory %s", manifestPath, d.repository, m.Repository, m.Path)
			}
		}
		dirs = append(dirs, seenDir{m.Repository, info, m.Path})
		own, err := Load(m.Path)
		if err != nil {
			return fmt.Errorf("%s: member %s: %w", manifestPath, m.Repository, err)
		}
		if own.Repository != "" {
			if own.Repository != m.Repository {
				return fmt.Errorf("%s: member %s: its graphvein.yaml names repository %q; the member's own name wins, so change the manifest to %q", manifestPath, m.Repository, own.Repository, own.Repository)
			}
			m.RepositorySource = RepositoryFromMemberConfig
		}
	}
	return nil
}
