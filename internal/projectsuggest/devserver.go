package projectsuggest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/parse"
)

// scriptJob is a dev-server config file waiting for the parser.
type scriptJob struct {
	member *config.Member
	path   string
	name   string
	data   []byte
}

// scriptLanguage is the grammar for a config file name, or "".
func scriptLanguage(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	switch name[i:] {
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".mts", ".cts":
		return "typescript"
	}
	return ""
}

func isViteConfig(name string) bool    { return strings.HasPrefix(name, "vite.config.") }
func isWebpackConfig(name string) bool { return strings.HasPrefix(name, "webpack.") }

// readScripts parses every dev-server config (the parser reads, it never
// runs the file) and reads rows SG-V1, SG-W1 and SG-W2.
func (s *state) readScripts(ctx context.Context) error {
	if len(s.scripts) == 0 {
		return nil
	}
	jobs := make([]parse.Job, len(s.scripts))
	for i, j := range s.scripts {
		jobs[i] = parse.Job{Language: scriptLanguage(j.name), Source: j.data}
	}
	results, err := parse.Batch(ctx, jobs, parse.Options{})
	if err != nil {
		return fmt.Errorf("parsing dev-server configs: %w", err)
	}
	for i, r := range results {
		j := s.scripts[i]
		switch {
		case r.Err != nil:
			s.unreadAt(j.member, j.path, 0, fmt.Sprintf("not parsed: %v", r.Err))
		case r.SyntaxError:
			s.unreadAt(j.member, j.path, 0, "syntax error")
		case isViteConfig(j.name):
			s.readDevServer(j, r.Root, RowViteProxy, "server")
			s.readViteDefines(j, r.Root)
		default:
			s.readDevServer(j, r.Root, "", "devServer")
			s.readWebpackDefines(j, r.Root)
		}
	}
	return nil
}

// exportedObject returns the literal object the file exports:
// `export default {...}`, `export default defineConfig({...})` (Vite) or
// `module.exports = {...}`. Anything else is config computed in code.
func exportedObject(root *parse.Node, vite bool) (*parse.Node, *parse.Node) {
	for _, st := range root.Children {
		var value *parse.Node
		switch st.Kind {
		case "export_statement":
			value = st.Fields["value"]
		case "expression_statement":
			for _, c := range st.Children {
				if c.Kind == "assignment_expression" && c.Fields["left"] != nil && c.Fields["left"].Text == "module.exports" {
					value = c.Fields["right"]
				}
			}
		}
		if value == nil {
			continue
		}
		if value.Kind == "parenthesized_expression" && len(value.Children) == 1 {
			value = value.Children[0]
		}
		if vite && value.Kind == "call_expression" && value.Fields["function"] != nil && value.Fields["function"].Text == "defineConfig" {
			if args := value.Fields["arguments"]; args != nil && len(nonComments(args.Children)) == 1 {
				value = nonComments(args.Children)[0]
			}
		}
		if value.Kind == "object" {
			return value, st
		}
		return nil, st
	}
	return nil, nil
}

func nonComments(list []*parse.Node) []*parse.Node {
	var out []*parse.Node
	for _, n := range list {
		if n.Kind != "comment" {
			out = append(out, n)
		}
	}
	return out
}

// property returns the value of key in a literal object. spread is set
// when the object has a spread element, which may hold the key.
func property(obj *parse.Node, key string) (value *parse.Node, keyNode *parse.Node, spread bool) {
	for _, c := range obj.Children {
		switch c.Kind {
		case "spread_element":
			spread = true
		case "pair":
			if k, ok := literalKey(c.Fields["key"]); ok && k == key {
				value, keyNode = c.Fields["value"], c
			}
		case "shorthand_property_identifier":
			if c.Text == key {
				value, keyNode = c, c
			}
		}
	}
	return value, keyNode, spread
}

// literalKey reads a property name or a string key without escapes or
// substitutions.
func literalKey(n *parse.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.Kind == "property_identifier" {
		return n.Text, true
	}
	return literalString(n)
}

// literalString reads a string literal or a template string without
// substitutions or escapes.
func literalString(n *parse.Node) (string, bool) {
	if n == nil || (n.Kind != "string" && n.Kind != "template_string") {
		return "", false
	}
	var b strings.Builder
	for _, c := range n.Children {
		if c.Kind != "string_fragment" {
			return "", false
		}
		b.WriteString(c.Text)
	}
	return b.String(), true
}

// readDevServer walks root.<section>.proxy in the exported literal object.
func (s *state) readDevServer(j scriptJob, root *parse.Node, viteRow, section string) {
	vite := viteRow != ""
	obj, st := exportedObject(root, vite)
	if obj == nil {
		line := 0
		if st != nil {
			line = int(st.Row) + 1
		}
		s.unreadAt(j.member, j.path, line, "config is computed in code (not a literal exported object)")
		return
	}
	sec, secKey, spread := property(obj, section)
	if sec == nil {
		if spread {
			s.unreadAt(j.member, j.path, int(obj.Row)+1, fmt.Sprintf("exported object has a spread that may hold %s", section))
		}
		return
	}
	if sec.Kind != "object" {
		s.unreadAt(j.member, j.path, int(secKey.Row)+1, fmt.Sprintf("%s is computed in code", section))
		return
	}
	proxy, proxyKey, spread := property(sec, "proxy")
	if proxy == nil {
		if spread {
			s.unreadAt(j.member, j.path, int(sec.Row)+1, fmt.Sprintf("%s has a spread that may hold proxy", section))
		}
		return
	}
	switch {
	case proxy.Kind == "object":
		row := viteRow
		if !vite {
			row = RowWebpackProxyObject
		}
		s.proxyObject(j, proxy, row, section)
	case proxy.Kind == "array" && !vite:
		s.proxyArray(j, proxy, section)
	default:
		s.unreadAt(j.member, j.path, int(proxyKey.Row)+1, fmt.Sprintf("%s.proxy is computed in code", section))
	}
}

// pathRewriting options change the path the provider sees, so the
// browser's prefix no longer names the provider's routes.
var pathRewriting = []string{"rewrite", "pathRewrite", "router", "bypass"}

// proxyObject reads `{ '/api': 'http://...' }` and
// `{ '/api': { target: 'http://...' } }` (rows SG-V1, SG-W1).
func (s *state) proxyObject(j scriptJob, proxy *parse.Node, row, section string) {
	for _, c := range proxy.Children {
		line := int(c.Row) + 1
		switch c.Kind {
		case "comment":
			continue
		case "pair":
		default:
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy entry is not a literal key and value", section))
			continue
		}
		prefix, ok := literalKey(c.Fields["key"])
		if !ok {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy key is computed", section))
			continue
		}
		if reason := prefixProblem(prefix); reason != "" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy key %q %s", section, prefix, reason))
			continue
		}
		target, reason := proxyTarget(c.Fields["value"])
		if reason != "" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy %q: %s", section, prefix, reason))
			continue
		}
		s.proxyAddress(j, row, line, prefix, target)
	}
}

// proxyArray reads `[{ context: ['/api'], target: 'http://...' }]`
// (row SG-W2).
func (s *state) proxyArray(j scriptJob, proxy *parse.Node, section string) {
	for _, item := range proxy.Children {
		line := int(item.Row) + 1
		if item.Kind == "comment" {
			continue
		}
		if item.Kind != "object" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy item is not a literal object", section))
			continue
		}
		target, reason := proxyTarget(item)
		if reason != "" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy item: %s", section, reason))
			continue
		}
		ctxNode, _, _ := property(item, "context")
		var prefixes []string
		switch {
		case ctxNode == nil:
			reason = "has no context"
		case ctxNode.Kind == "array":
			for _, c := range nonComments(ctxNode.Children) {
				p, ok := literalString(c)
				if !ok {
					reason = "context is computed"
					break
				}
				prefixes = append(prefixes, p)
			}
		default:
			p, ok := literalString(ctxNode)
			if !ok {
				reason = "context is computed (a function or a variable)"
			}
			prefixes = append(prefixes, p)
		}
		if reason == "" && len(prefixes) == 0 {
			reason = "context is empty"
		}
		for _, p := range prefixes {
			if reason != "" {
				break
			}
			if problem := prefixProblem(p); problem != "" {
				reason = fmt.Sprintf("context %q %s", p, problem)
			}
		}
		if reason != "" {
			s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s.proxy item %s", section, reason))
			continue
		}
		for _, p := range prefixes {
			s.proxyAddress(j, RowWebpackProxyArray, line, p, target)
		}
	}
}

// prefixProblem refuses proxy keys that are patterns: Vite reads a key
// starting with ^ as a regular expression, webpack's proxy reads globs.
func prefixProblem(p string) string {
	if strings.ContainsAny(p, "^$*?()[]{}|+\\") {
		return "is a pattern (regular expression or glob)"
	}
	if !strings.HasPrefix(p, "/") {
		return "does not start with /"
	}
	if strings.ContainsAny(p, " \t#") {
		return "has whitespace or a fragment"
	}
	return ""
}

// proxyTarget reads a proxy value: a literal URL string, or a literal
// object whose target is one and which rewrites no path.
func proxyTarget(v *parse.Node) (string, string) {
	if v == nil {
		return "", "no value"
	}
	if t, ok := literalString(v); ok {
		return t, ""
	}
	if v.Kind != "object" {
		return "", "value is computed in code"
	}
	target, _, spread := property(v, "target")
	if spread {
		return "", "options have a spread"
	}
	for _, opt := range pathRewriting {
		if n, _, _ := property(v, opt); n != nil {
			return "", fmt.Sprintf("%s changes the path the provider sees", opt)
		}
	}
	t, ok := literalString(target)
	if !ok {
		return "", "target is not a literal string"
	}
	return t, ""
}

// proxyAddress records a proxy target as an address the member calls
// under prefix (empty: every path).
func (s *state) proxyAddress(j scriptJob, row string, line int, prefix, target string) {
	a, reason, isHTTP := readURL(target)
	if !isHTTP {
		reason = "target is not an http(s) URL"
	}
	if reason != "" {
		key := prefix
		if key == "" {
			key = "proxy"
		}
		s.unreadAt(j.member, j.path, line, fmt.Sprintf("%s: %s", key, reason))
		return
	}
	a.consumer, a.scope = j.member, scopeOf(j.member, j.path)
	if prefix != "" {
		a.prefixes = []string{prefix}
	}
	key := prefix
	if key == "" {
		key = "proxy"
	}
	a.evidence = s.evidence(j.member, row, j.path, line, key, target)
	s.addresses = append(s.addresses, a)
}

// readPackage reads row SG-R1: Create React App's `proxy` forwards every
// request the dev server cannot serve, so the entry has no selector. The
// field is only read when the package depends on react-scripts; elsewhere
// nothing uses it.
func (s *state) readPackage(m *config.Member, path string, data []byte) {
	var pkg struct {
		Proxy           json.RawMessage   `json:"proxy"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		if bytes.Contains(data, []byte(`"proxy"`)) {
			s.unreadAt(m, path, 0, fmt.Sprintf("not valid JSON: %v", err))
		}
		return
	}
	if pkg.Proxy == nil {
		return
	}
	line := lineOf(data, `"proxy"`)
	var target string
	if err := json.Unmarshal(pkg.Proxy, &target); err != nil {
		s.unreadAt(m, path, line, "proxy is not a string (the object form of Create React App 1 is not read)")
		return
	}
	_, dep := pkg.Dependencies["react-scripts"]
	_, dev := pkg.DevDependencies["react-scripts"]
	if !dep && !dev {
		s.unreadAt(m, path, line, "proxy without a react-scripts dependency (only Create React App reads it)")
		return
	}
	s.proxyAddress(scriptJob{member: m, path: path}, RowCRAProxy, line, "", target)
}

// lineOf is the 1-based line of the first occurrence of needle, or 0.
func lineOf(data []byte, needle string) int {
	i := bytes.Index(data, []byte(needle))
	if i < 0 {
		return 0
	}
	return bytes.Count(data[:i], []byte("\n")) + 1
}
