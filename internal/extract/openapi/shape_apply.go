package openapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// safeMap reads one operation's mapped shape; a failure records a
// diagnostic and maps nothing, never stopping the import (AGENTS.md: a
// failure in one file never stops others).
func (p *pass) safeMap(d *document, item, op location) (out *specShape) {
	defer func() {
		if v := recover(); v != nil {
			p.quiet = false
			p.diag(location{doc: op.doc, pointer: op.pointer}, "openapi_shape_failed", fmt.Sprintf("reading the operation's shape: %v", v))
			out = nil
		}
	}()
	return p.mapOperation(d, item, op)
}

// applyShapes adds the mapped items of each linked operation to the shape
// attribute of the route it documents (SH-O1..SH-O3). A route documented
// by several operations gets none of them (SH-O2): which one describes it
// is not proven.
func (p *pass) applyShapes() {
	if p.options.ShapeDepth == 0 || len(p.mapped) == 0 {
		return
	}
	byRoute := map[string][]contract.Edge{}
	for _, e := range p.edges {
		if e.Kind == "documents_route" {
			byRoute[e.Target] = append(byRoute[e.Target], e)
		}
	}
	routes := map[string]contract.Node{}
	for _, n := range p.options.Routes {
		if n.Kind == "route" && n.Attributes != nil {
			routes[n.Identity] = n
		}
	}
	targets := make([]string, 0, len(byRoute))
	for t := range byRoute {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, target := range targets {
		n, ok := routes[target]
		if !ok {
			continue
		}
		edges := byRoute[target]
		if len(edges) > 1 {
			rowOf("link:several")
			continue
		}
		frag := p.mapped[edges[0].Source]
		if frag == nil {
			continue
		}
		rowOf("link:documents_route")
		if err := p.applyShape(n, frag, edges[0]); err != nil {
			p.diagnostics = append(p.diagnostics, contract.Diagnostic{Type: "diagnostic", File: n.File, Severity: "info", Code: "openapi_shape_failed",
				Message: fmt.Sprintf("%s: mapped shape from %s not stored: %v", n.Identity, edges[0].Source, err)})
		}
	}
}

// applyShape merges one operation's mapped items into the route's shape.
// The route's own items, states and models are never changed (SH-O3); a
// code fact the spec contradicts gets a "spec differs" note (SH-O18).
func (p *pass) applyShape(n contract.Node, frag *specShape, edge contract.Edge) error {
	var s *routeshape.Shape
	if value, ok := n.Attributes[routeshape.AttributeKey]; ok {
		var err error
		if s, err = routeshape.FromAttribute(value); err != nil {
			return fmt.Errorf("reading the route's shape: %w", err)
		}
	} else {
		s = routeshape.New(p.options.ShapeDepth)
	}
	f := withConfidence(frag, edge.Confidence)
	for _, m := range s.Models {
		for _, fm := range f.models {
			if m.Name == fm.Name {
				return fmt.Errorf("model %s exists in the route's shape", m.Name)
			}
		}
	}
	s.Models = append(s.Models, f.models...)
	renameSchemes(s, f)
	types := typeIndex(s)

	params := []*routeshape.ParamSection{&s.Path, &s.Query, &s.Header, &s.Cookie}
	for i, sec := range params {
		name := routeshape.SectionNames[i]
		items := f.params[name]
		var notes []string
		if sec.State != routeshape.NotRead {
			notes = paramDiffers(name, sec, items, types)
		}
		if mergeState(&sec.Section, len(items) > 0, notes, f.notes[name]) {
			sec.Items = append(sec.Items, items...)
		}
	}
	var notes []string
	if s.Body.State != routeshape.NotRead {
		notes = bodyDiffers(&s.Body, f.body, types)
	}
	if mergeState(&s.Body.Section, len(f.body) > 0, notes, joinNotes(f.bodyNote, f.notes[routeshape.SectionBody])) {
		s.Body.Items = append(s.Body.Items, f.body...)
	}
	notes = nil
	if s.Responses.State != routeshape.NotRead {
		notes = responseDiffers(&s.Responses, f.responses, types)
	}
	if mergeState(&s.Responses.Section, len(f.responses) > 0, notes, f.notes[routeshape.SectionResponses]) {
		s.Responses.Items = append(s.Responses.Items, f.responses...)
	}
	notes = nil
	if s.Security.State != routeshape.NotRead {
		notes = securityDiffers(&s.Security, f.security)
	}
	if mergeState(&s.Security.Section, len(f.security) > 0, notes, "") {
		s.Security.Items = append(s.Security.Items, f.security...)
		s.Schemes = append(s.Schemes, f.schemes...)
	}
	value, err := routeshape.ToAttribute(s)
	if err != nil {
		return err
	}
	n.Attributes[routeshape.AttributeKey] = value
	return nil
}

// mergeState applies SH-O3 to one section and reports whether the mapped
// items are added: a section the code did not read holds them as
// spec-only; a read section keeps its state; provably_none keeps its
// state and gets a note instead (it cannot hold items).
func mergeState(sec *routeshape.Section, has bool, notes []string, extra string) bool {
	if !has && extra == "" && len(notes) == 0 {
		return false
	}
	if sec.State == routeshape.NotRead {
		if !has {
			return false
		}
		rowOf("state:spec_only")
		sec.State = routeshape.Partial
		sec.Reason = joinNotes(routeshape.SpecOnlyReason, extra)
		return true
	}
	if sec.State == routeshape.ProvablyNone {
		rowOf("state:provably_none_kept")
		sec.Reason = joinNotes(sec.Reason, notesText(notes))
		return false
	}
	rowOf("state:code_kept")
	sec.Reason = joinNotes(sec.Reason, notesText(notes), extra)
	return has
}

func notesText(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "spec differs: " + strings.Join(notes, "; ")
}

func joinNotes(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}

// withConfidence copies a mapped fragment with the confidence of the link
// it is applied through (SH-O1, SM-11).
func withConfidence(frag *specShape, c float64) *specShape {
	out := &specShape{params: map[string][]routeshape.Param{}, bodyNote: frag.bodyNote, notes: frag.notes}
	for k, items := range frag.params {
		cp := append([]routeshape.Param(nil), items...)
		for i := range cp {
			cp[i].Provenance.Confidence = c
		}
		out.params[k] = cp
	}
	out.body = append([]routeshape.Content(nil), frag.body...)
	for i := range out.body {
		out.body[i].Provenance.Confidence = c
	}
	out.responses = append([]routeshape.Response(nil), frag.responses...)
	for i := range out.responses {
		out.responses[i].Provenance.Confidence = c
	}
	out.security = make([]routeshape.Requirement, len(frag.security))
	for i, r := range frag.security {
		r.Schemes = append([]routeshape.SchemeUse(nil), r.Schemes...)
		r.Provenance.Confidence = c
		out.security[i] = r
	}
	out.schemes = append([]routeshape.Scheme(nil), frag.schemes...)
	for i := range out.schemes {
		out.schemes[i].Provenance.Confidence = c
	}
	out.models = append([]routeshape.Model(nil), frag.models...)
	for i := range out.models {
		out.models[i].Provenance.Confidence = c
	}
	return out
}

// renameSchemes gives a mapped scheme whose name the route's code already
// uses the name "<name> (spec)" (SH-O17), so both stay side by side.
func renameSchemes(s *routeshape.Shape, f *specShape) {
	used := map[string]bool{}
	for _, sc := range s.Schemes {
		used[sc.Name] = true
	}
	names := map[string]string{}
	for i := range f.schemes {
		old := f.schemes[i].Name
		if !used[old] {
			continue
		}
		rowOf("scheme:renamed")
		name := old + " (spec)"
		for used[name] {
			name += "'"
		}
		used[name] = true
		names[old] = name
		f.schemes[i].Name = name
	}
	for i := range f.security {
		for j := range f.security[i].Schemes {
			if n, ok := names[f.security[i].Schemes[j].Name]; ok {
				f.security[i].Schemes[j].Name = n
			}
		}
	}
}

// typeIndex resolves a schema's top-level type through model references,
// for the type comparisons of SH-O18.
func typeIndex(s *routeshape.Shape) func(*routeshape.Schema) routeshape.SchemaType {
	models := map[string]routeshape.Schema{}
	for _, m := range s.Models {
		models[m.Name] = m.Schema
	}
	return func(sc *routeshape.Schema) routeshape.SchemaType {
		for i := 0; sc != nil && i < 16; i++ {
			if sc.Type != routeshape.TypeRef {
				if sc.Type == routeshape.TypeUnknown {
					return ""
				}
				return sc.Type
			}
			m, ok := models[sc.Ref]
			if !ok {
				return ""
			}
			sc = &m
		}
		return ""
	}
}

// typesDiffer reports two known top-level types that contradict each
// other (integer within number is no contradiction).
func typesDiffer(a, b routeshape.SchemaType) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	numeric := func(t routeshape.SchemaType) bool { return t == routeshape.TypeInteger || t == routeshape.TypeNumber }
	if numeric(a) && numeric(b) {
		return false
	}
	// An enum's literals are compared by nothing here: a string enum in
	// one and a string in the other agree.
	if a == routeshape.TypeEnum || b == routeshape.TypeEnum {
		return false
	}
	return true
}

// paramDiffers compares a read parameter section with the spec's
// parameters of that location (SH-O18).
func paramDiffers(section string, sec *routeshape.ParamSection, spec []routeshape.Param, types func(*routeshape.Schema) routeshape.SchemaType) []string {
	key := func(n string) string {
		if section == routeshape.SectionHeader {
			return strings.ToLower(n)
		}
		return n
	}
	facts := map[string]routeshape.Param{}
	var factOrder []string
	for _, it := range sec.Items {
		if it.Provenance.Kind == routeshape.Fact && it.Access == routeshape.Declared {
			if _, dup := facts[key(it.Name)]; !dup {
				factOrder = append(factOrder, key(it.Name))
			}
			facts[key(it.Name)] = it
		}
	}
	var notes []string
	specNames := map[string]bool{}
	for _, m := range spec {
		specNames[key(m.Name)] = true
		f, ok := facts[key(m.Name)]
		if !ok {
			continue
		}
		known := func(p routeshape.Presence) bool { return p == routeshape.Required || p == routeshape.Optional }
		if known(f.Presence) && known(m.Presence) && f.Presence != m.Presence {
			rowOf("differs:presence")
			notes = append(notes, fmt.Sprintf("%s parameter %s is %s in the code, %s in the spec", section, m.Name, f.Presence, m.Presence))
		}
		// A parameter is text on the wire: code that reads it as a
		// string accepts whatever the spec's type allows.
		if a, b := types(f.Schema), types(m.Schema); a != routeshape.TypeString && typesDiffer(a, b) {
			rowOf("differs:type")
			notes = append(notes, fmt.Sprintf("%s parameter %s is %s in the code, %s in the spec", section, m.Name, a, b))
		}
	}
	// Path parameters are matched by position when the route is linked;
	// their names are not observable on the wire and are not compared.
	proven := sec.Evidence != nil && sec.Evidence.Kind == routeshape.Fact && (sec.State == routeshape.Complete || sec.State == routeshape.ProvablyNone)
	if proven && section != routeshape.SectionPath {
		var extra, missing []string
		for _, m := range spec {
			if _, ok := facts[key(m.Name)]; !ok {
				extra = append(extra, m.Name)
			}
		}
		for _, k := range factOrder {
			if !specNames[k] {
				missing = append(missing, facts[k].Name)
			}
		}
		if len(extra) > 0 {
			rowOf("differs:names")
			notes = append(notes, fmt.Sprintf("the spec declares %s parameters %s that the code does not", section, strings.Join(extra, ", ")))
		}
		if len(missing) > 0 {
			rowOf("differs:names")
			notes = append(notes, fmt.Sprintf("the code declares %s parameters %s that the spec does not", section, strings.Join(missing, ", ")))
		}
	}
	return notes
}

func contentType(ct *string) string {
	if ct == nil {
		return ""
	}
	return *ct
}

// mediaKey is a media type compared by type and subtype only, case
// insensitively and without parameters (SH-O18): "application/json;
// charset=utf-8" and "Application/JSON" name the same body.
func mediaKey(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// mediaRange reports a media range ("*/*", "application/*") rather than
// one media type.
func mediaRange(key string) bool { return strings.Contains(key, "*") }

// mediaCovers reports whether a media range or type covers a media type
// (both as mediaKey returns them).
func mediaCovers(pattern, key string) bool {
	if pattern == key || pattern == "*/*" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		return strings.HasPrefix(key, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// bodyDiffers compares a read body section with the spec's (SH-O18).
// Media types are compared by mediaKey; a media range on either side
// covers the types it names and is never a difference itself.
func bodyDiffers(sec *routeshape.BodySection, spec []routeshape.Content, types func(*routeshape.Schema) routeshape.SchemaType) []string {
	var notes []string
	proven := sec.Evidence != nil && sec.Evidence.Kind == routeshape.Fact
	if sec.State == routeshape.ProvablyNone && proven && len(spec) > 0 {
		rowOf("differs:body")
		return []string{"the spec declares a request body; the code proves none"}
	}
	facts := map[string]routeshape.Content{}
	var factKeys []string
	for _, it := range sec.Items {
		if it.Provenance.Kind == routeshape.Fact && it.ContentType != nil {
			k := mediaKey(*it.ContentType)
			if _, dup := facts[k]; !dup {
				factKeys = append(factKeys, k)
			}
			facts[k] = it
		}
	}
	var specKeys []string
	specNames := map[string]string{}
	for _, m := range spec {
		if m.ContentType == nil {
			continue
		}
		k := mediaKey(*m.ContentType)
		if _, dup := specNames[k]; !dup {
			specKeys = append(specKeys, k)
			specNames[k] = *m.ContentType
		}
		if mediaRange(k) {
			continue
		}
		if f, ok := facts[k]; ok {
			if a, b := types(f.Schema), types(m.Schema); typesDiffer(a, b) {
				rowOf("differs:type")
				notes = append(notes, fmt.Sprintf("the %s request body is %s in the code, %s in the spec", *m.ContentType, a, b))
			}
		}
	}
	covered := func(key string, by []string) bool {
		for _, p := range by {
			if mediaCovers(p, key) || mediaCovers(key, p) {
				return true
			}
		}
		return false
	}
	if sec.State == routeshape.Complete && proven && len(specKeys) > 0 {
		var missing []string
		for _, k := range factKeys {
			if !covered(k, specKeys) {
				missing = append(missing, *facts[k].ContentType)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			rowOf("differs:body")
			notes = append(notes, "the code reads request bodies of type "+strings.Join(missing, ", ")+" that the spec does not declare")
		}
		var extra []string
		for _, k := range specKeys {
			if !mediaRange(k) && !covered(k, factKeys) {
				extra = append(extra, specNames[k])
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			rowOf("differs:body")
			notes = append(notes, "the spec declares request bodies of type "+strings.Join(extra, ", ")+" that the code does not read")
		}
	}
	return notes
}

// responseDiffers compares the code's fact responses with the spec's
// (SH-O18): a success status the spec does not document, and a body type
// that contradicts the spec's for the same status and content type.
func responseDiffers(sec *routeshape.ResponseSection, spec []routeshape.Response, types func(*routeshape.Schema) routeshape.SchemaType) []string {
	if len(spec) == 0 {
		return nil
	}
	documented := map[string]bool{}
	bySite := map[string]routeshape.Response{}
	for _, m := range spec {
		documented[m.Status] = true
		bySite[m.Status+"\x00"+mediaKey(contentType(m.ContentType))] = m
	}
	var notes []string
	seen := map[string]bool{}
	for _, it := range sec.Items {
		if it.Provenance.Kind != routeshape.Fact {
			continue
		}
		st := it.Status
		if strings.HasPrefix(st, "2") && len(st) == 3 && !documented[st] && !documented["2XX"] && !documented["default"] && !seen[st] {
			seen[st] = true
			rowOf("differs:status")
			notes = append(notes, "the code answers "+st+", which the spec does not document")
		}
		if it.ContentType == nil {
			continue
		}
		k := mediaKey(*it.ContentType)
		if mediaRange(k) {
			continue
		}
		if m, ok := bySite[st+"\x00"+k]; ok {
			if a, b := types(it.Schema), types(m.Schema); typesDiffer(a, b) {
				rowOf("differs:type")
				notes = append(notes, fmt.Sprintf("the %s %s response is %s in the code, %s in the spec", st, *it.ContentType, a, b))
			}
		}
	}
	return notes
}

// securityDiffers compares a proven security section with the spec's
// requirements (SH-O18): public in one, authenticated in the other.
func securityDiffers(sec *routeshape.SecuritySection, spec []routeshape.Requirement) []string {
	if len(spec) == 0 {
		return nil
	}
	if sec.State != routeshape.Partial && (sec.Evidence == nil || sec.Evidence.Kind != routeshape.Fact) {
		return nil
	}
	anonymous := func(reqs []routeshape.Requirement) (any, all bool) {
		all = len(reqs) > 0
		for _, r := range reqs {
			if r.Provenance.Kind != routeshape.Fact && r.Provenance.Kind != routeshape.Mapped {
				continue
			}
			if len(r.Schemes) == 0 {
				any = true
			} else {
				all = false
			}
		}
		return any, all
	}
	specAny, specAll := anonymous(spec)
	switch sec.State {
	case routeshape.ProvablyNone:
		if !specAny {
			rowOf("differs:security")
			return []string{"the spec requires authentication; the code proves none is required"}
		}
	case routeshape.Complete, routeshape.Partial:
		// A partial section may lack requirements (middleware is not
		// read), which only adds authentication: code facts that all
		// require it still contradict a spec that requires none.
		var facts []routeshape.Requirement
		for _, r := range sec.Items {
			if r.Provenance.Kind == routeshape.Fact {
				facts = append(facts, r)
			}
		}
		codeAny, _ := anonymous(facts)
		if len(facts) > 0 && !codeAny && specAll {
			rowOf("differs:security")
			return []string{"the spec requires no authentication; the code requires it"}
		}
		if sec.State == routeshape.Complete && codeAny && !specAny {
			rowOf("differs:security")
			return []string{"the spec requires authentication; the code also allows anonymous requests"}
		}
	}
	return nil
}

// shapeDiag records a diagnostic found while reading mapped shapes, once
// per message: the reader runs quiet for references the import already
// reported, and one schema can be read by many operations.
func (p *pass) shapeDiag(loc location, code, message string) {
	key := loc.doc.file + "#" + loc.pointer + "\x00" + code + "\x00" + message
	if p.shapeDiags[key] {
		return
	}
	if p.shapeDiags == nil {
		p.shapeDiags = map[string]bool{}
	}
	p.shapeDiags[key] = true
	quiet := p.quiet
	p.quiet = false
	p.diag(loc, code, message)
	p.quiet = quiet
}
