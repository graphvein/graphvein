package openapi

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

type document struct {
	file            string
	source          []byte
	root            *value
	base            *url.URL
	format, version string
	minor           int
	supported       bool
	// lines holds the offset of every line start, built on first use by
	// the mapped shape reader (line).
	lines []int
}
type location struct {
	doc     *document
	value   *value
	pointer string
}
type pass struct {
	options     LinkOptions
	sources     map[string][]byte
	docs        map[string]*document
	failed      map[string]bool
	self        map[string][]*document
	nodes       []contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
	// quiet suppresses diagnostics while the mapped shape reader resolves
	// references the import already reported (SH-O).
	quiet bool
	// mapped holds the mapped shape of each linked operation, by identity.
	mapped map[string]*specShape
	// shapeDiags holds the diagnostics the mapped shape reader recorded,
	// so each is written once.
	shapeDiags map[string]bool
}

var yamlPrefilter = regexp.MustCompile(`(?m)^(?:openapi|swagger|"openapi"|"swagger"|'openapi'|'swagger')[ \t]*:`)
var versionShape = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)
var versionPattern = regexp.MustCompile(`^3\.([0-9]+)(?:\.[0-9]+)*$`)

// Link builds current declarations using only scanner-visible, normalized bytes.
// References and configuration are deliberately recomputed on every pass.
func Link(sources map[string][]byte, rules, tests testscope.Rules, options ...LinkOptions) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	p := &pass{sources: sources, docs: map[string]*document{}, failed: map[string]bool{}, self: map[string][]*document{}}
	if len(options) > 0 {
		p.options = options[0]
	}
	files := make([]string, 0, len(sources))
	for file := range sources {
		files = append(files, file)
	}
	sort.Strings(files)
	var confirmed []*document
	for _, file := range files {
		ext := strings.ToLower(path.Ext(file))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}
		included, excluded := rules.Match(file)
		if excluded {
			continue
		}
		name := strings.ToLower(path.Base(file))
		named := strings.Contains(name, "openapi") || strings.Contains(name, "swagger")
		source := sources[file]
		prefilter := false
		if ext == ".json" {
			prefilter = bytes.Contains(source, []byte(`"openapi"`)) || bytes.Contains(source, []byte(`"swagger"`))
		} else {
			prefilter = yamlPrefilter.Match(source)
		}
		if !named && !included && !prefilter {
			continue
		}
		d, err := p.document(file)
		if err != nil {
			if named || included || (ext != ".json" && prefilter) {
				p.diag(location{doc: &document{file: file}}, "openapi_parse_error", err.Error())
			}
			continue
		}
		v, exists := d.root.field("openapi")
		d.format = "openapi"
		if !exists {
			v, exists = d.root.field("swagger")
			d.format = "swagger"
		}
		d.version = v.value.versionText()
		if !exists || d.root.kind != 'm' || !versionShape.MatchString(d.version) {
			if included {
				message := "version value is not a version-shaped scalar"
				if !exists {
					message = "missing version value"
				}
				p.diag(location{d, d.root, ""}, "openapi_not_a_spec", message)
			}
			continue
		}
		d.supported = true
		if d.format == "swagger" {
			if d.version != "2.0" {
				p.diag(location{d, v.value, "/swagger"}, "openapi_unsupported_version", "unsupported version "+d.version)
				continue
			}
		} else {
			parts := versionPattern.FindStringSubmatch(d.version)
			if parts == nil {
				p.diag(location{d, v.value, "/openapi"}, "openapi_unsupported_version", "unsupported version "+d.version)
				continue
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil {
				n = 3
			}
			d.minor = n
			if n > 2 {
				d.supported = false
				p.diag(location{d, v.value, "/openapi"}, "openapi_unsupported_version", "version "+d.version+" uses 3.2 rules")
			}
		}
		if d.format == "openapi" && d.minor >= 2 {
			if self := d.root.get("$self").text(); self != "" {
				base, err := resolveURI(d.base, self)
				if err != nil {
					p.diag(location{d, d.root.get("$self"), "/$self"}, "openapi_ref_unresolved", "invalid $self "+self+": "+err.Error())
				} else {
					base.Fragment = ""
					base.RawFragment = ""
					d.base = base
					if !localURI(base) {
						p.self[base.String()] = append(p.self[base.String()], d)
					}
				}
			}
		}
		confirmed = append(confirmed, d)
	}
	for _, d := range confirmed {
		start := len(p.nodes)
		p.emit(d, tests)
		p.coverage(d.file, start)
	}
	p.applyShapes()
	return p.nodes, p.edges, p.diagnostics
}

func (p *pass) document(file string) (*document, error) {
	if d := p.docs[file]; d != nil {
		return d, nil
	}
	if p.failed[file] {
		return nil, fmt.Errorf("invalid referenced document %s", file)
	}
	source, ok := p.sources[file]
	if !ok {
		return nil, fmt.Errorf("file is not scanner-visible: %s", file)
	}
	root, err := parseSource(source, strings.EqualFold(path.Ext(file), ".json"))
	if err != nil {
		p.failed[file] = true
		return nil, err
	}
	d := &document{file: file, source: source, root: root, base: &url.URL{Scheme: "graphvein", Host: "repository", Path: "/" + file}}
	p.docs[file] = d
	return d, nil
}
func localURI(u *url.URL) bool { return u.Scheme == "graphvein" && u.Host == "repository" }
func resolveURI(base *url.URL, text string) (*url.URL, error) {
	u, err := url.Parse(text)
	if err != nil {
		return nil, err
	}
	if localURI(base) && u.Scheme == "" && u.Host == "" {
		joined := path.Join(path.Dir(strings.TrimPrefix(base.Path, "/")), u.Path)
		if strings.HasPrefix(u.Path, "/") || joined == ".." || strings.HasPrefix(joined, "../") {
			return nil, fmt.Errorf("URI leaves repository")
		}
	}
	return base.ResolveReference(u), nil
}
func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }
func (p *pass) diag(loc location, code, message string) {
	if p.quiet {
		return
	}
	severity := "info"
	if code == "openapi_parse_error" || code == "openapi_unsupported_version" {
		severity = "warning"
	}
	region := ""
	if loc.doc.root != nil {
		if e, ok := pointerEntry(loc.doc.root, loc.pointer); ok {
			s := span(loc.doc.source, e.start, e.end)
			region = fmt.Sprintf(" [%d:%d-%d:%d]", s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
		}
	}
	p.diagnostics = append(p.diagnostics, contract.Diagnostic{Type: "diagnostic", File: loc.doc.file, Severity: severity, Code: code, Message: fmt.Sprintf("%s#%s%s: %s", loc.doc.file, loc.pointer, region, message)})
}
func pointerParts(pointer string) ([]string, bool) {
	if pointer == "" {
		return nil, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	parts := strings.Split(pointer[1:], "/")
	for i, s := range parts {
		for j := 0; j < len(s); j++ {
			if s[j] == '~' {
				if j+1 == len(s) || s[j+1] != '0' && s[j+1] != '1' {
					return nil, false
				}
				j++
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return parts, true
}
func pointerEntry(root *value, pointer string) (entry, bool) {
	parts, ok := pointerParts(pointer)
	if !ok || len(parts) == 0 {
		return entry{}, false
	}
	v := root
	var e entry
	for _, part := range parts {
		if v == nil {
			return entry{}, false
		}
		if v.kind == 's' {
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v.items) || strconv.Itoa(i) != part {
				return entry{}, false
			}
			v = v.items[i]
			e = entry{value: v}
		} else {
			e, ok = v.field(part)
			if !ok {
				return entry{}, false
			}
			v = e.value
		}
	}
	return e, true
}
func lookup(root *value, pointer string) (*value, bool) {
	if pointer == "" {
		return root, true
	}
	e, ok := pointerEntry(root, pointer)
	return e.value, ok
}
func (p *pass) resolve(loc location) location { return p.follow(loc, map[string]bool{}) }
func (p *pass) follow(loc location, seen map[string]bool) location {
	ref, exists := loc.value.field("$ref")
	if !exists {
		return loc
	}
	text := ref.value.text()
	failure := func(reason string) location {
		p.diag(location{loc.doc, ref.value, loc.pointer + "/$ref"}, "openapi_ref_unresolved", fmt.Sprintf("reference %q: %s", text, reason))
		return location{doc: loc.doc, pointer: loc.pointer}
	}
	if text == "" {
		return failure("expected a nonempty URI")
	}
	u, err := resolveURI(loc.doc.base, text)
	if err != nil {
		return failure(err.Error())
	}
	pointer := u.Fragment
	u.Fragment = ""
	u.RawFragment = ""
	var target *document
	if localURI(u) {
		target, err = p.document(strings.TrimPrefix(u.Path, "/"))
		if err != nil {
			return failure(err.Error())
		}
	} else {
		docs := p.self[u.String()]
		if len(docs) == 0 {
			return failure("absolute URI is not claimed by any scanned document's $self")
		}
		if len(docs) > 1 {
			names := []string{}
			for _, d := range docs {
				names = append(names, d.file)
			}
			return failure("absolute URI is claimed by multiple scanned documents' $self; claimants: " + strings.Join(names, ", "))
		}
		target = docs[0]
	}
	key := target.file + "#" + pointer
	if seen[key] {
		return failure("reference cycle")
	}
	seen[key] = true
	v, ok := lookup(target.root, pointer)
	if !ok {
		return failure("invalid JSON pointer " + pointer)
	}
	return p.follow(location{target, v, pointer}, seen)
}

func (p *pass) add(d *document, kind, name string, level, start, end int, attrs map[string]any, owner string) string {
	s := span(d.source, start, end)
	id := facts.ID(kind, d.file, name)
	p.nodes = append(p.nodes, contract.Node{Type: "node", Identity: id, Kind: kind, Level: level, Name: name, QualifiedName: name, File: d.file, Span: &s, ContentHash: contract.ContentHash(d.source[start:end]), Attributes: attrs})
	p.edges = append(p.edges, facts.Edge("contains", owner, id, d.file, s))
	return id
}
func (p *pass) emit(d *document, tests testscope.Rules) {
	root := d.root
	info := root.get("info")
	attrs := map[string]any{"format": d.format, "spec_version": d.version, "version_supported": d.supported, "title": info.get("title").nullable(), "api_version": info.get("version").nullable(), "self": nil}
	if d.format == "openapi" {
		attrs["servers"] = serversAttribute(root.get("servers"))
		if d.minor >= 2 {
			attrs["self"] = root.get("$self").nullable()
		}
	} else {
		attrs["servers"] = map[string]any{"host": root.get("host").nullable(), "base_path": root.get("basePath").nullable(), "schemes": stringsList(root.get("schemes"))}
	}
	scope := testscope.Classify(d.file, tests)
	if scope == testscope.Test {
		attrs["scope"] = "test"
	}
	spec := p.add(d, "api_spec", "spec", 2, 0, len(d.source), attrs, facts.ID("file", d.file, d.file))
	for _, pe := range root.get("paths").fields() {
		pointer := "/paths/" + escape(pe.key)
		if strings.HasPrefix(pe.key, "x-") {
			continue
		}
		if !strings.HasPrefix(pe.key, "/") {
			p.diag(location{d, pe.value, pointer}, "openapi_invalid_path", "path must start with /")
			continue
		}
		loc := p.resolve(location{d, pe.value, pointer})
		if loc.value == nil {
			continue
		}
		_, referenced := pe.value.field("$ref")
		for _, oe := range loc.value.fields() {
			fixed := strings.Contains("|get|put|post|delete|options|head|patch|", "|"+oe.key+"|") || oe.key == "trace" && d.format != "swagger"
			if oe.key == "query" {
				fixed = d.format != "swagger" && d.minor >= 2
			}
			if fixed {
				p.operation(d, spec, pe, oe, strings.ToUpper(oe.key), loc, referenced, scope)
				continue
			}
			if oe.key == "query" || oe.key == "additionalOperations" {
				if d.format == "swagger" || d.minor < 2 {
					p.diag(location{loc.doc, oe.value, loc.pointer + "/" + oe.key}, "openapi_invalid_operation", oe.key+" requires OpenAPI 3.2")
					continue
				}
				for _, extra := range oe.value.fields() {
					if strings.Contains("|GET|PUT|POST|DELETE|OPTIONS|HEAD|PATCH|TRACE|QUERY|", "|"+extra.key+"|") {
						p.diag(location{loc.doc, extra.value, loc.pointer + "/additionalOperations/" + escape(extra.key)}, "openapi_invalid_operation", "fixed method in additionalOperations: "+extra.key)
						continue
					}
					p.operation(d, spec, pe, extra, extra.key, location{loc.doc, loc.value, loc.pointer + "/additionalOperations"}, referenced, scope)
				}
			}
		}
	}
}

func stringsList(v *value) []string {
	out := []string{}
	for _, x := range v.list() {
		if s, ok := x.scalar.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func sortedKeys(v *value) []string {
	out := []string{}
	for _, e := range v.fields() {
		if !strings.HasPrefix(e.key, "x-") {
			out = append(out, e.key)
		}
	}
	sort.Strings(out)
	return out
}
func (p *pass) operation(d *document, spec string, pe, oe entry, method string, pathLoc location, referenced bool, scope testscope.Scope) {
	if oe.value == nil || oe.value.kind != 'm' {
		p.diag(location{pathLoc.doc, oe.value, pathLoc.pointer + "/" + escape(oe.key)}, "openapi_invalid_operation", "operation must be a mapping")
		return
	}
	op := oe.value
	opLoc := location{pathLoc.doc, op, pathLoc.pointer + "/" + escape(oe.key)}
	attrs := map[string]any{"method": method, "path": pe.key, "operation_id": op.get("operationId").nullable(), "summary": op.get("summary").nullable(), "tags": stringsList(op.get("tags")), "deprecated": op.get("deprecated").boolean()}
	// Additional operation metadata still inherits the actual path item.
	item := pathLoc.value
	// Nullable names remain distinct from every string, including "<nil>".
	type parameterKey struct{ name, location any }
	parameters := map[parameterKey]map[string]any{}
	body := false
	for _, owner := range []location{{pathLoc.doc, item, strings.TrimSuffix(pathLoc.pointer, "/additionalOperations")}, opLoc} {
		for i, parameter := range owner.value.get("parameters").list() {
			loc := p.resolve(location{owner.doc, parameter, fmt.Sprintf("%s/parameters/%d", owner.pointer, i)})
			if loc.value == nil {
				continue
			}
			v := loc.value
			name, in := v.get("name").nullable(), v.get("in").nullable()
			key := parameterKey{name, in}
			parameters[key] = map[string]any{"name": name, "in": in, "required": v.get("required").boolean()}
			if in == "body" || in == "formData" {
				body = true
			}
		}
	}
	params := []map[string]any{}
	for _, v := range parameters {
		params = append(params, v)
	}
	sort.Slice(params, func(i, j int) bool {
		a, b := params[i], params[j]
		if fmt.Sprint(a["in"]) != fmt.Sprint(b["in"]) {
			return fmt.Sprint(a["in"]) < fmt.Sprint(b["in"])
		}
		if a["name"] == nil {
			return b["name"] != nil
		}
		if b["name"] == nil {
			return false
		}
		return a["name"].(string) < b["name"].(string)
	})
	attrs["parameters"] = params
	content := []string{}
	if d.format == "swagger" {
		if body {
			consumes, exists := op.field("consumes")
			if !exists {
				consumes.value = d.root.get("consumes")
			}
			content = stringsList(consumes.value)
			sort.Strings(content)
		}
	} else if v, exists := op.field("requestBody"); exists {
		loc := p.resolve(location{opLoc.doc, v.value, opLoc.pointer + "/requestBody"})
		content = sortedKeys(loc.value.get("content"))
	}
	attrs["request_content_types"] = content
	responses := op.get("responses")
	attrs["response_codes"] = sortedKeys(responses)
	for _, r := range responses.fields() {
		if !strings.HasPrefix(r.key, "x-") {
			p.resolve(location{opLoc.doc, r.value, opLoc.pointer + "/responses/" + escape(r.key)})
		}
	}
	security, exists := op.field("security")
	if !exists {
		security.value = d.root.get("security")
	}
	schemes := map[string]bool{}
	for _, req := range security.value.list() {
		for _, e := range req.fields() {
			schemes[e.key] = true
		}
	}
	names := []string{}
	for s := range schemes {
		names = append(names, s)
	}
	sort.Strings(names)
	attrs["security"] = names
	paths, count := operationPaths(d, item, op, pe.key)
	attrs["paths"] = paths
	attrs["path_count"] = count
	start, end := oe.start, oe.end
	if referenced {
		start, end = pe.start, pe.end
		attrs["ref_file"] = pathLoc.doc.file
		attrs["ref_pointer"] = strings.TrimSuffix(pathLoc.pointer, "/additionalOperations")
	}
	if scope == testscope.Test {
		attrs["scope"] = "test"
	}
	id := p.add(d, "api_operation", method+" "+pe.key, 4, start, end, attrs, spec)
	edges := len(p.edges)
	p.linkOperation(p.nodes[len(p.nodes)-1])
	if p.options.ShapeDepth != 0 && len(p.edges) > edges {
		if p.mapped == nil {
			p.mapped = map[string]*specShape{}
		}
		itemLoc := location{pathLoc.doc, item, strings.TrimSuffix(pathLoc.pointer, "/additionalOperations")}
		p.mapped[id] = p.safeMap(d, itemLoc, opLoc)
	}
}
