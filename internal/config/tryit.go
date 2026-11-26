package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file holds the "try it" environments (T029, ADR 0081): the
// `tryit` section of graphvein.yaml and of graphvein-project.yaml, and the
// writers the server's guarded environment edit uses. An environment is
// the only thing that makes a host dialable for a try-it request; nothing
// here is ever inferred from file names or code.

// TryIt is the `tryit` section of graphvein.yaml.
type TryIt struct {
	// Environments are in file order; names are unique.
	Environments []Environment
}

// Environment is one named place a repository's API runs.
type Environment struct {
	// Name identifies the environment in requests and in the file.
	Name string
	// Label is the user's word for the environment (local, staging,
	// production, ...). Only the labels in ProductionLabels change the
	// server's behaviour: they need an acknowledgement before any send.
	Label string
	// BaseURL is the URL every operation path is appended to.
	BaseURL BaseURL
	// Auth, when set, names the header the server fills from an
	// environment variable (or from a value typed in the UI for the
	// session).
	Auth *Auth
	// Options are TLS trust, proxy use and limits (all off by default).
	Options EnvironmentOptions
	// Line is the file line of the entry, for messages.
	Line int
}

// Auth names one request header whose value never passes through the
// browser or the file: it comes from the server's environment variable Env,
// or from a value typed in the UI and held only in server memory.
type Auth struct {
	Header string
	Env    string
	// Prefix is put before the variable's value, for example "Bearer ".
	Prefix string
}

// ProductionLabels are the labels (compared case-insensitively) that make an
// environment a production one. The list is deliberately short and fixed:
// a label is the user's choice, and these are the words a user picks to say
// "this is the real system".
var ProductionLabels = []string{"production", "prod"}

// Production reports whether the environment's label marks it production.
func Production(label string) bool {
	l := strings.ToLower(label)
	for _, p := range ProductionLabels {
		if l == p {
			return true
		}
	}
	return false
}

// BaseURL is a validated http(s) base URL: scheme, host, optional port and
// optional base path, never user info, query or fragment.
type BaseURL struct {
	// Text is the normalised URL: scheme and host lower-cased, no trailing
	// slash.
	Text   string
	Scheme string
	// Host is a DNS name or an IP literal (without brackets).
	Host string
	// Port is explicit or the scheme's default.
	Port uint16
	// Path is "" or a path starting with "/" and not ending with one.
	Path string
}

// Origin is scheme://host[:port], the one destination the URL allows.
func (b BaseURL) Origin() string {
	host := b.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if (b.Scheme == "http" && b.Port == 80) || (b.Scheme == "https" && b.Port == 443) {
		return b.Scheme + "://" + host
	}
	return b.Scheme + "://" + host + ":" + strconv.Itoa(int(b.Port))
}

// ParseBaseURL validates s as an environment base URL.
func ParseBaseURL(s string) (BaseURL, error) {
	if s == "" || len(s) > 2048 || strings.ContainsAny(s, " \t\r\n\x00") {
		return BaseURL{}, fmt.Errorf("base_url must be a nonempty URL without spaces or control characters")
	}
	u, err := url.Parse(s)
	if err != nil {
		return BaseURL{}, fmt.Errorf("base_url %q is not a URL", s)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return BaseURL{}, fmt.Errorf("base_url %q must start with http:// or https://", s)
	}
	if u.Opaque != "" || u.Host == "" {
		return BaseURL{}, fmt.Errorf("base_url %q has no host", s)
	}
	if u.User != nil {
		return BaseURL{}, fmt.Errorf("base_url must not contain user info; put credentials in auth (an environment variable)")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(s, "?#") {
		return BaseURL{}, fmt.Errorf("base_url %q must not have a query or fragment", s)
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "%") {
		return BaseURL{}, fmt.Errorf("base_url %q: IPv6 zones (%%name) are not supported; use an address without a zone", s)
	}
	if host == "" {
		return BaseURL{}, fmt.Errorf("base_url %q has no valid host", s)
	}
	if _, err := netip.ParseAddr(host); err != nil && !validDNSName(host) {
		return BaseURL{}, fmt.Errorf("base_url %q: host %q is not a DNS name or IP address", s, host)
	}
	port := uint16(80)
	if scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return BaseURL{}, fmt.Errorf("base_url %q: port must be 1-65535", s)
		}
		port = uint16(n)
	} else if strings.HasSuffix(u.Host, ":") {
		return BaseURL{}, fmt.Errorf("base_url %q: empty port", s)
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		// Compared decoded: %2e%2e is a dot segment to many servers.
		if decoded, err := url.PathUnescape(seg); err != nil || decoded == "." || decoded == ".." {
			return BaseURL{}, fmt.Errorf("base_url %q must not contain . or .. path segments", s)
		}
	}
	b := BaseURL{Scheme: scheme, Host: host, Port: port, Path: path}
	b.Text = b.Origin() + path
	return b, nil
}

// validDNSName accepts letters, digits, hyphens and dots, each label 1-63
// characters, as hosts in URLs are written.
func validDNSName(h string) bool {
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// EnvironmentNameProblem says why s is not a valid environment name, or "".
func EnvironmentNameProblem(s string) string {
	if s == "" || len(s) > 64 {
		return "must be 1 to 64 characters"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return "may contain only letters, digits, '-', '_' and '.'"
		}
	}
	return ""
}

// LabelProblem says why s is not a valid environment label, or "".
func LabelProblem(s string) string {
	if strings.TrimSpace(s) == "" || len(s) > 32 {
		return "must be 1 to 32 characters, not only spaces"
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return "must not contain control characters"
		}
	}
	return ""
}

// forbiddenHeaders are request headers neither an environment nor the
// browser may set: the HTTP client owns them (framing, connection and
// proxy handling).
var forbiddenHeaders = map[string]bool{
	"host": true, "connection": true, "content-length": true, "transfer-encoding": true,
	"te": true, "trailer": true, "upgrade": true, "keep-alive": true, "expect": true,
}

// HeaderNameProblem says why name may not be set on a try-it request, or
// "". It is the one rule for configured auth headers and headers typed in
// the UI (row TI-13).
func HeaderNameProblem(name string) string {
	if name == "" || len(name) > 128 {
		return "a header name is 1 to 128 characters"
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0) {
			return "a header name is an RFC 9110 token (letters, digits and !#$%&'*+-.^_`|~)"
		}
	}
	l := strings.ToLower(name)
	if forbiddenHeaders[l] || strings.HasPrefix(l, "proxy-") {
		return "the HTTP client sets " + name + " itself"
	}
	return ""
}

// envNameValid accepts portable environment variable names.
func envNameValid(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// lineError is a validation error at a line of the file being read.
type lineError struct {
	line int
	msg  string
}

func (e *lineError) Error() string { return fmt.Sprintf("line %d: %s", e.line, e.msg) }

func errAt(n *yaml.Node, format string, args ...any) error {
	return &lineError{n.Line, fmt.Sprintf(format, args...)}
}

// entries returns a mapping's pairs, refusing non-string and duplicate keys
// and keys outside allowed.
func entries(n *yaml.Node, what string, allowed ...string) ([][2]*yaml.Node, error) {
	if n.Kind != yaml.MappingNode {
		return nil, errAt(n, "%s must be a mapping", what)
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return nil, errAt(n, "YAML aliases and anchors are not allowed in %s", what)
	}
	seen := map[string]bool{}
	var out [][2]*yaml.Node
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return nil, errAt(k, "%s keys must be strings", what)
		}
		if seen[k.Value] {
			return nil, errAt(k, "duplicate %s key %s", what, k.Value)
		}
		seen[k.Value] = true
		if v.Kind == yaml.AliasNode || v.Anchor != "" {
			return nil, errAt(v, "YAML aliases and anchors are not allowed in %s", what)
		}
		known := allowed == nil
		for _, a := range allowed {
			known = known || a == k.Value
		}
		if !known {
			return nil, errAt(k, "unknown %s key %s", what, k.Value)
		}
		out = append(out, [2]*yaml.Node{k, v})
	}
	return out, nil
}

func scalarString(n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", errAt(n, "%s must be a string", what)
	}
	return n.Value, nil
}

// environmentList returns the items of a section's environments list.
func environmentList(n *yaml.Node, section string) ([]*yaml.Node, error) {
	fields, err := entries(n, section, "environments")
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		list := f[1]
		if list.Kind != yaml.SequenceNode {
			return nil, errAt(list, "%s.environments must be a list", section)
		}
		return list.Content, nil
	}
	return nil, nil
}

// decodeTryIt reads graphvein.yaml's tryit section.
func decodeTryIt(n *yaml.Node) (TryIt, error) {
	var t TryIt
	items, err := environmentList(n, "tryit")
	if err != nil {
		return t, err
	}
	names := map[string]bool{}
	for _, item := range items {
		fields, err := entries(item, "tryit environment", append([]string{"name", "label", "base_url", "auth"}, optionKeys...)...)
		if err != nil {
			return t, err
		}
		e := Environment{Line: item.Line}
		var baseNode *yaml.Node
		for _, f := range fields {
			k, v := f[0], f[1]
			switch k.Value {
			case "name", "label", "base_url":
				s, err := scalarString(v, k.Value)
				if err != nil {
					return t, err
				}
				switch k.Value {
				case "name":
					if p := EnvironmentNameProblem(s); p != "" {
						return t, errAt(v, "environment name %s", p)
					}
					e.Name = s
				case "label":
					if p := LabelProblem(s); p != "" {
						return t, errAt(v, "environment label %s", p)
					}
					e.Label = s
				case "base_url":
					baseNode = v
					if e.BaseURL, err = ParseBaseURL(s); err != nil {
						return t, errAt(v, "%v", err)
					}
				}
			case "auth":
				a, err := decodeAuth(v)
				if err != nil {
					return t, err
				}
				e.Auth = &a
			default:
				if err := decodeOption(&e.Options, k, v, false); err != nil {
					return t, err
				}
			}
		}
		for _, need := range []struct {
			ok   bool
			name string
		}{{e.Name != "", "name"}, {e.Label != "", "label"}, {baseNode != nil, "base_url"}} {
			if !need.ok {
				return t, errAt(item, "a tryit environment needs %s", need.name)
			}
		}
		if err := checkPinOrigins(item, e.Options, e.BaseURL.Origin()); err != nil {
			return t, err
		}
		if names[e.Name] {
			return t, errAt(item, "environment %s is defined twice", e.Name)
		}
		names[e.Name] = true
		t.Environments = append(t.Environments, e)
	}
	return t, nil
}

func decodeAuth(n *yaml.Node) (Auth, error) {
	var a Auth
	fields, err := entries(n, "auth", "header", "env", "prefix")
	if err != nil {
		return a, err
	}
	for _, f := range fields {
		k, v := f[0], f[1]
		s, err := scalarString(v, "auth."+k.Value)
		if err != nil {
			return a, err
		}
		switch k.Value {
		case "header":
			if p := HeaderNameProblem(s); p != "" {
				return a, errAt(v, "auth.header %q: %s", s, p)
			}
			a.Header = s
		case "env":
			if !envNameValid(s) {
				return a, errAt(v, "auth.env must name an environment variable (letters, digits, _), not hold a value")
			}
			a.Env = s
		case "prefix":
			if len(s) > 64 || strings.ContainsAny(s, "\r\n\x00") {
				return a, errAt(v, "auth.prefix must be at most 64 characters without line breaks")
			}
			a.Prefix = s
		}
	}
	if a.Header == "" || a.Env == "" {
		return a, errAt(n, "auth needs header and env (the environment variable holding the value)")
	}
	return a, nil
}

// ProjectEnvironment is a project-wide environment: one name and label, a
// base URL per member that runs in it.
type ProjectEnvironment struct {
	Name  string
	Label string
	// BaseURLs are in file order.
	BaseURLs []MemberBaseURL
	Auth     *Auth
	// Options apply to every member's base URL in the environment.
	Options EnvironmentOptions
	Line    int
}

// MemberBaseURL is one member's base URL in a project environment.
type MemberBaseURL struct {
	Repository string
	BaseURL    BaseURL
}

// For returns the member's base URL in the environment.
func (pe ProjectEnvironment) For(repository string) (BaseURL, bool) {
	for _, m := range pe.BaseURLs {
		if m.Repository == repository {
			return m.BaseURL, true
		}
	}
	return BaseURL{}, false
}

// decodeProjectTryIt reads the manifest's tryit section. Member names in
// base_urls are checked against the members by the caller.
func decodeProjectTryIt(n *yaml.Node) ([]ProjectEnvironment, error) {
	items, err := environmentList(n, "tryit")
	if err != nil {
		return nil, err
	}
	var out []ProjectEnvironment
	names := map[string]bool{}
	for _, item := range items {
		fields, err := entries(item, "tryit environment", append([]string{"name", "label", "base_urls", "auth"}, optionKeys...)...)
		if err != nil {
			return nil, err
		}
		pe := ProjectEnvironment{Line: item.Line}
		seenURLs := false
		for _, f := range fields {
			k, v := f[0], f[1]
			switch k.Value {
			case "name":
				s, err := scalarString(v, "name")
				if err != nil {
					return nil, err
				}
				if p := EnvironmentNameProblem(s); p != "" {
					return nil, errAt(v, "environment name %s", p)
				}
				pe.Name = s
			case "label":
				s, err := scalarString(v, "label")
				if err != nil {
					return nil, err
				}
				if p := LabelProblem(s); p != "" {
					return nil, errAt(v, "environment label %s", p)
				}
				pe.Label = s
			case "base_urls":
				seenURLs = true
				urls, err := entries(v, "base_urls")
				if err != nil {
					return nil, err
				}
				for _, u := range urls {
					s, err := scalarString(u[1], "base_urls."+u[0].Value)
					if err != nil {
						return nil, err
					}
					b, err := ParseBaseURL(s)
					if err != nil {
						return nil, errAt(u[1], "%v", err)
					}
					pe.BaseURLs = append(pe.BaseURLs, MemberBaseURL{Repository: u[0].Value, BaseURL: b})
				}
			case "auth":
				a, err := decodeAuth(v)
				if err != nil {
					return nil, err
				}
				pe.Auth = &a
			default:
				if err := decodeOption(&pe.Options, k, v, true); err != nil {
					return nil, err
				}
			}
		}
		if pe.Name == "" || pe.Label == "" || !seenURLs || len(pe.BaseURLs) == 0 {
			return nil, errAt(item, "a project tryit environment needs name, label and base_urls (member: URL)")
		}
		var origins []string
		for _, m := range pe.BaseURLs {
			origins = append(origins, m.BaseURL.Origin())
		}
		if err := checkPinOrigins(item, pe.Options, origins...); err != nil {
			return nil, err
		}
		if names[pe.Name] {
			return nil, errAt(item, "environment %s is defined twice", pe.Name)
		}
		names[pe.Name] = true
		out = append(out, pe)
	}
	return out, nil
}

// environmentNode is the YAML of one new environment entry.
func environmentNode(name, label string, urls *yaml.Node, urlKey string, auth *Auth, o EnvironmentOptions) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, str("name"), str(name), str("label"), str(label), str(urlKey), urls)
	if auth != nil {
		a := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("header"), str(auth.Header), str("env"), str(auth.Env)}}
		if auth.Prefix != "" {
			a.Content = append(a.Content, str("prefix"), str(auth.Prefix))
		}
		m.Content = append(m.Content, str("auth"), a)
	}
	m.Content = append(m.Content, optionNodes(o)...)
	return m
}

// environmentsSequence returns root's tryit.environments list, creating the
// section and the list when absent.
func environmentsSequence(root *yaml.Node) (*yaml.Node, error) {
	section := mappingValue(root, "tryit")
	if section == nil {
		section = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, str("tryit"), section)
	}
	if section.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: tryit must be a mapping", section.Line)
	}
	list := mappingValue(section, "environments")
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		section.Content = append(section.Content, str("environments"), list)
	}
	if list.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: tryit.environments must be a list", list.Line)
	}
	return list, nil
}

// AddTryItEnvironment returns graphvein.yaml's text with e appended to
// tryit.environments. data nil means the file does not exist yet: the
// result is a new file with version 1. Comments of the rest are kept;
// yaml.v3 re-indents the document with two spaces. The caller validates
// the result with Parse before writing it.
func AddTryItEnvironment(data []byte, e Environment) ([]byte, error) {
	if data == nil {
		data = []byte("version: 1\n")
	}
	return editManifest(data, func(root *yaml.Node) error {
		list, err := environmentsSequence(root)
		if err != nil {
			return err
		}
		list.Content = append(list.Content, environmentNode(e.Name, e.Label, str(e.BaseURL.Text), "base_url", e.Auth, e.Options))
		return nil
	})
}

// AddProjectTryItEnvironment returns the manifest with a new project
// environment pe appended.
func AddProjectTryItEnvironment(data []byte, pe ProjectEnvironment) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		list, err := environmentsSequence(root)
		if err != nil {
			return err
		}
		urls := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, m := range pe.BaseURLs {
			urls.Content = append(urls.Content, str(m.Repository), str(m.BaseURL.Text))
		}
		list.Content = append(list.Content, environmentNode(pe.Name, pe.Label, urls, "base_urls", pe.Auth, pe.Options))
		return nil
	})
}

// SetProjectTryItBaseURL returns the manifest with member's base URL in the
// existing project environment name set to b (added, or replaced).
func SetProjectTryItBaseURL(data []byte, name, member string, b BaseURL) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		list, err := environmentsSequence(root)
		if err != nil {
			return err
		}
		item := findByKey(list, "name", name)
		if item == nil {
			return fmt.Errorf("the manifest has no tryit environment %s", name)
		}
		urls := mappingValue(item, "base_urls")
		if urls == nil || urls.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: environment %s has no base_urls mapping", item.Line, name)
		}
		if v := mappingValue(urls, member); v != nil {
			*v = *str(b.Text)
			return nil
		}
		urls.Content = append(urls.Content, str(member), str(b.Text))
		return nil
	})
}
