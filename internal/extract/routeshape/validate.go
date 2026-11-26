package routeshape

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Violation is one broken model invariant: the row it breaks, where, and
// what is wrong.
type Violation struct {
	Row     string
	Path    string
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.Row, v.Path, v.Message)
}

// Validate checks every model invariant of rows SM-1..SM-29 and returns
// the violations in a stable order (nil when the shape is valid). An
// extractor calls it through ToAttribute before writing a shape, and the
// server calls it through FromAttribute before serving one, so a broken
// shape is never shown as knowledge.
func Validate(s *Shape) []Violation {
	v := &validator{s: s, models: map[string]Model{}}
	v.run()
	return v.out
}

type validator struct {
	s      *Shape
	models map[string]Model
	out    []Violation
}

func (v *validator) add(row, path, format string, args ...any) {
	v.out = append(v.out, Violation{Row: row, Path: path, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) run() {
	s := v.s
	if s.Version != ModelVersion {
		v.add("SM-1", "version", "version %d, want %d", s.Version, ModelVersion)
	}
	if s.Follow.DepthLimit < MinDepth || s.Follow.DepthLimit > MaxDepthLimit {
		v.add("SM-21", "follow.depth_limit", "%d is outside %d..%d", s.Follow.DepthLimit, MinDepth, MaxDepthLimit)
	}
	for i, m := range s.Models {
		p := fmt.Sprintf("models[%d]", i)
		if m.Name == "" {
			v.add("SM-20", p, "model without a name")
			continue
		}
		if _, dup := v.models[m.Name]; dup {
			v.add("SM-20", p, "duplicate model %q", m.Name)
		}
		v.models[m.Name] = m
	}
	for i, m := range s.Models {
		p := fmt.Sprintf("models[%d]", i)
		v.provenance(p+".provenance", m.Provenance)
		v.schema(p+".schema", &m.Schema, m.Provenance.Kind, 0)
	}
	params := []*ParamSection{&s.Path, &s.Query, &s.Header, &s.Cookie}
	for i, sec := range params {
		name := SectionNames[i]
		kinds := make([]Kind, len(sec.Items))
		for j, it := range sec.Items {
			kinds[j] = it.Provenance.Kind
		}
		v.section(name, sec.Section, len(sec.Items), kinds)
		seen := map[string]bool{}
		for j, it := range sec.Items {
			v.param(fmt.Sprintf("%s.items[%d]", name, j), name, it, seen)
		}
	}
	v.body()
	v.responses()
	v.security()
	v.follow()
}

// section checks the state rules SM-2..SM-5.
func (v *validator) section(name string, sec Section, items int, kinds []Kind) {
	if sec.Evidence != nil {
		v.provenance(name+".evidence", *sec.Evidence)
	}
	proving := sec.Evidence != nil && (sec.Evidence.Kind == Fact || sec.Evidence.Kind == Mapped)
	switch sec.State {
	case NotRead:
		if items > 0 || sec.Evidence != nil {
			v.add("SM-2", name, "not_read has no items and no evidence")
		}
	case ProvablyNone:
		if items > 0 {
			v.add("SM-3", name, "provably_none has no items")
		}
		if !proving {
			v.add("SM-3", name, "provably_none needs evidence of kind fact or mapped")
		}
	case Partial:
		if strings.TrimSpace(sec.Reason) == "" {
			v.add("SM-4", name, "partial needs a reason saying what may be missing")
		}
		if sec.Evidence != nil {
			v.add("SM-4", name, "partial has no evidence of completeness")
		}
	case Complete:
		if items == 0 {
			v.add("SM-5", name, "complete with no items: use provably_none")
		}
		if !proving {
			v.add("SM-5", name, "complete needs evidence of kind fact or mapped")
		}
		for _, k := range kinds {
			if k != Fact && k != Mapped {
				v.add("SM-5", name, "complete holds an item of kind %s", k)
				break
			}
		}
	default:
		v.add("SM-1", name+".state", "unknown state %q", sec.State)
	}
}

func proven(k Kind) bool { return k == Fact || k == Mapped }

// param checks rows SM-12, SM-13, SM-14 and SM-16.
func (v *validator) param(path, section string, p Param, seen map[string]bool) {
	v.provenance(path+".provenance", p.Provenance)
	kind := p.Provenance.Kind
	if p.Name == "" {
		v.add("SM-16", path, "parameter without a name")
	}
	key := p.Name
	if section == SectionHeader {
		key = strings.ToLower(key) // header names are case-insensitive
	}
	key += "\x00" + string(kind)
	if seen[key] {
		v.add("SM-16", path, "duplicate parameter %q of kind %s", p.Name, kind)
	}
	seen[key] = true
	switch p.Presence {
	case Required, Optional, PresenceUnknown:
	default:
		v.add("SM-16", path, "unknown presence %q", p.Presence)
	}
	switch p.Access {
	case Declared:
		if kind == ReadInHandler {
			v.add("SM-12", path, "a name read in code is never a declared parameter")
		}
	case ReadByHandler, ReadByDependency:
		row := "SM-12"
		if p.Access == ReadByDependency {
			row = "SM-13"
			if len(p.Provenance.Chain) == 0 {
				v.add(row, path, "read_by_dependency needs the chain to the dependency")
			}
		}
		if kind != ReadInHandler {
			v.add(row, path, "%s needs kind read_in_handler, not %s", p.Access, kind)
		}
		if p.Presence != PresenceUnknown {
			v.add(row, path, "a name read in code is never required or optional (presence unknown)")
		}
		if p.Schema != nil {
			v.add(row, path, "a name read in code is never typed (schema null)")
		}
	default:
		v.add("SM-16", path, "unknown access %q", p.Access)
	}
	if p.Presence == Required && !proven(kind) {
		v.add("SM-14", path, "required only from kind fact or mapped, not %s", kind)
	}
	if section == SectionPath && p.Access == Declared && proven(kind) && p.Presence != Required {
		v.add("SM-16", path, "a declared path parameter is required")
	}
	if p.Schema != nil {
		v.schema(path+".schema", p.Schema, kind, 0)
	}
}

func (v *validator) body() {
	s := v.s
	kinds := make([]Kind, len(s.Body.Items))
	for i, c := range s.Body.Items {
		kinds[i] = c.Provenance.Kind
	}
	v.section(SectionBody, s.Body.Section, len(s.Body.Items), kinds)
	seen := map[string]bool{}
	for i, c := range s.Body.Items {
		p := fmt.Sprintf("body.items[%d]", i)
		v.provenance(p+".provenance", c.Provenance)
		ct := "\x00"
		if c.ContentType != nil {
			ct = *c.ContentType
			if strings.TrimSpace(ct) == "" {
				v.add("SM-17", p, "empty content type: use null for not read")
			}
		}
		key := ct + "\x00" + string(c.Provenance.Kind)
		if seen[key] {
			v.add("SM-17", p, "duplicate body variant")
		}
		seen[key] = true
		if c.Schema != nil {
			v.schema(p+".schema", c.Schema, c.Provenance.Kind, 0)
			if c.Provenance.Kind == ReadInHandler {
				v.readSchema(p+".schema", c.Schema)
			}
		}
	}
}

// readSchema checks row SM-12 for a body built from names read in code:
// objects of fields whose presence and type are unknown.
func (v *validator) readSchema(path string, s *Schema) {
	if s.Type != TypeObject {
		if s.Type != TypeUnknown {
			v.add("SM-12", path, "names read in code are never typed (type %s)", s.Type)
		}
		return
	}
	for i := range s.Fields {
		f := &s.Fields[i]
		fp := fmt.Sprintf("%s.fields[%d]", path, i)
		if f.Presence != PresenceUnknown {
			v.add("SM-12", fp, "a name read in code is never required or optional")
		}
		v.readSchema(fp+".schema", &f.Schema)
	}
}

var statusPattern = regexp.MustCompile(`^([1-5][0-9][0-9]|[1-5]XX|default)$`)

func (v *validator) responses() {
	s := v.s
	kinds := make([]Kind, len(s.Responses.Items))
	for i, r := range s.Responses.Items {
		kinds[i] = r.Provenance.Kind
	}
	v.section(SectionResponses, s.Responses.Section, len(s.Responses.Items), kinds)
	seen := map[string]bool{}
	for i, r := range s.Responses.Items {
		p := fmt.Sprintf("responses.items[%d]", i)
		v.provenance(p+".provenance", r.Provenance)
		if !statusPattern.MatchString(r.Status) {
			v.add("SM-18", p, "status %q is not a code, a range like 4XX or default", r.Status)
		}
		ct := "\x00"
		if r.ContentType != nil {
			ct = *r.ContentType
			if strings.TrimSpace(ct) == "" {
				v.add("SM-18", p, "empty content type: use null for not read")
			}
		}
		key := r.Status + "\x00" + ct + "\x00" + string(r.Provenance.Kind)
		if seen[key] {
			v.add("SM-18", p, "duplicate response")
		}
		seen[key] = true
		if r.Schema != nil {
			v.schema(p+".schema", r.Schema, r.Provenance.Kind, 0)
		}
	}
}

func (v *validator) security() {
	s := v.s
	schemes := map[string]bool{}
	for i, sc := range s.Schemes {
		p := fmt.Sprintf("security_schemes[%d]", i)
		v.provenance(p+".provenance", sc.Provenance)
		if sc.Name == "" || schemes[sc.Name] {
			v.add("SM-19", p, "scheme name empty or duplicate (%q)", sc.Name)
		}
		schemes[sc.Name] = true
		set := func(field string, value *string, allowed bool) {
			if value == nil {
				return
			}
			if !allowed {
				v.add("SM-19", p+"."+field, "not a field of a %s scheme", sc.Type)
			} else if *value == "" {
				v.add("SM-19", p+"."+field, "empty: use null for not read")
			}
		}
		switch sc.Type {
		case SchemeHTTP, SchemeAPIKey, SchemeOAuth2, SchemeOpenIDConnect, SchemeUnknown:
		default:
			v.add("SM-19", p, "unknown scheme type %q", sc.Type)
		}
		set("scheme", sc.Scheme, sc.Type == SchemeHTTP)
		set("bearer_format", sc.BearerFormat, sc.Type == SchemeHTTP)
		set("in", sc.In, sc.Type == SchemeAPIKey)
		set("param_name", sc.ParamName, sc.Type == SchemeAPIKey)
		set("token_url", sc.TokenURL, sc.Type == SchemeOAuth2)
		set("openid_connect_url", sc.OpenIDConnectURL, sc.Type == SchemeOpenIDConnect)
		if sc.In != nil && *sc.In != "header" && *sc.In != "query" && *sc.In != "cookie" {
			v.add("SM-19", p+".in", "%q is not header, query or cookie", *sc.In)
		}
	}
	kinds := make([]Kind, len(s.Security.Items))
	for i, r := range s.Security.Items {
		kinds[i] = r.Provenance.Kind
	}
	v.section(SectionSecurity, s.Security.Section, len(s.Security.Items), kinds)
	for i, r := range s.Security.Items {
		p := fmt.Sprintf("security.items[%d]", i)
		v.provenance(p+".provenance", r.Provenance)
		if len(r.Schemes) == 0 && !proven(r.Provenance.Kind) {
			v.add("SM-19", p, "the anonymous alternative needs kind fact or mapped")
		}
		for j, use := range r.Schemes {
			if !schemes[use.Name] {
				v.add("SM-19", fmt.Sprintf("%s.schemes[%d]", p, j), "scheme %q is not in security_schemes", use.Name)
			}
		}
	}
}

// schema checks row SM-20 for one schema node and rows SM-14 and SM-15
// for the evidence level kind of the item or model it belongs to.
func (v *validator) schema(path string, s *Schema, kind Kind, depth int) {
	if depth > MaxSchemaDepth {
		v.add("SM-20", path, "nested deeper than %d: cut to unknown", MaxSchemaDepth)
		return
	}
	if s.Type != TypeObject && (len(s.Fields) > 0 || s.Additional != "") {
		v.add("SM-20", path, "fields or additional on a %s schema", s.Type)
	}
	if s.Type != TypeArray && s.Items != nil {
		v.add("SM-20", path, "items on a %s schema", s.Type)
	}
	if s.Type != TypeEnum && len(s.Enum) > 0 {
		v.add("SM-20", path, "enum values on a %s schema", s.Type)
	}
	if s.Type != TypeRef && s.Ref != "" {
		v.add("SM-20", path, "ref on a %s schema", s.Type)
	}
	if s.Type != TypeUnknown && s.Reason != "" {
		v.add("SM-20", path, "reason on a %s schema", s.Type)
	}
	switch s.Type {
	case TypeObject:
		switch s.Additional {
		case AdditionalAllowed, AdditionalForbidden, AdditionalUnknown:
		default:
			v.add("SM-20", path, "object needs additional allowed, forbidden or unknown")
		}
		names := map[string]bool{}
		for i := range s.Fields {
			f := &s.Fields[i]
			fp := fmt.Sprintf("%s.fields[%d]", path, i)
			if f.Name == "" || names[f.Name] {
				v.add("SM-20", fp, "field name empty or duplicate (%q)", f.Name)
			}
			names[f.Name] = true
			switch f.Presence {
			case Required, Optional, PresenceUnknown:
			default:
				v.add("SM-20", fp, "unknown presence %q", f.Presence)
			}
			if f.Presence == Required && !proven(kind) {
				v.add("SM-14", fp, "required only from kind fact or mapped, not %s", kind)
			}
			v.schema(fp+".schema", &f.Schema, kind, depth+1)
		}
	case TypeArray:
		if s.Items == nil {
			v.add("SM-20", path, "array without items (use an unknown items schema)")
		} else {
			v.schema(path+".items", s.Items, kind, depth+1)
		}
	case TypeEnum:
		if len(s.Enum) == 0 {
			v.add("SM-20", path, "enum without values")
		}
		for i, e := range s.Enum {
			switch e.(type) {
			case nil, string, bool, float64, int, int64:
			default:
				v.add("SM-20", fmt.Sprintf("%s.enum[%d]", path, i), "not a literal (%T)", e)
			}
		}
	case TypeRef:
		m, ok := v.models[s.Ref]
		if !ok {
			v.add("SM-20", path, "ref %q names no model", s.Ref)
		} else if proven(kind) && !proven(m.Provenance.Kind) {
			v.add("SM-15", path, "a %s item cannot take its schema from %s model %q", kind, m.Provenance.Kind, s.Ref)
		}
	case TypeUnknown:
		if strings.TrimSpace(s.Reason) == "" {
			v.add("SM-20", path, "unknown needs a reason")
		}
	case TypeString, TypeNumber, TypeInteger, TypeBoolean, TypeNull:
	default:
		v.add("SM-20", path, "unknown schema type %q", s.Type)
	}
}

// provenance checks row SM-11 (and the kind list SM-6..SM-10).
func (v *validator) provenance(path string, p Provenance) {
	switch p.Kind {
	case Fact, ReadInHandler, Intended, Returned, Mapped:
	default:
		v.add("SM-11", path, "unknown kind %q", p.Kind)
	}
	if p.Rule == "" {
		v.add("SM-11", path, "no rule")
	}
	if p.File == "" || p.Line < 1 {
		v.add("SM-11", path, "no source file and line")
	}
	for i, st := range p.Chain {
		if !validStep(st) {
			v.add("SM-11", fmt.Sprintf("%s.chain[%d]", path, i), "step needs file, line and function")
		}
	}
	if len(p.Chain) > v.s.Follow.DepthLimit {
		v.add("SM-21", path+".chain", "chain of %d calls is longer than the depth limit %d", len(p.Chain), v.s.Follow.DepthLimit)
	}
	if p.Kind == Mapped {
		if !(p.Confidence > 0 && p.Confidence <= 1) {
			v.add("SM-11", path, "mapped confidence %v is outside (0, 1]", p.Confidence)
		}
	} else if p.Confidence != 1 {
		v.add("SM-11", path, "confidence %v: everything read from code has confidence 1", p.Confidence)
	}
}

func validStep(s Step) bool {
	return s.File != "" && s.Line >= 1 && s.Function != ""
}

// follow checks the chain records (rows SM-21..SM-29).
func (v *validator) follow() {
	limit := v.s.Follow.DepthLimit
	sections := map[string]bool{}
	for _, n := range SectionNames {
		sections[n] = true
	}
	for i, c := range v.s.Follow.Chains {
		p := fmt.Sprintf("follow.chains[%d]", i)
		if !sections[c.Section] {
			v.add("SM-29", p, "unknown section %q", c.Section)
		}
		if !validStep(c.Start) {
			v.add("SM-29", p+".start", "start needs file, line and function")
		}
		if c.Depth < 0 || c.Depth > limit || len(c.Steps) != c.Depth {
			v.add("SM-21", p, "depth %d with %d steps and limit %d", c.Depth, len(c.Steps), limit)
		}
		for j, st := range c.Steps {
			if !validStep(st) {
				v.add("SM-29", fmt.Sprintf("%s.steps[%d]", p, j), "step needs file, line and function")
			}
		}
		row, ok := stopRows[c.Stop]
		if !ok {
			v.add("SM-29", p, "unknown stop %q", c.Stop)
			continue
		}
		needsAt := c.Stop != StopEnded && c.Stop != StopResolverError
		if needsAt && (c.At == nil || !validStep(*c.At)) {
			v.add(row, p, "stop %s needs the site it stopped at", c.Stop)
		}
		if !needsAt && c.At != nil {
			v.add(row, p, "stop %s has no site", c.Stop)
		}
		if c.Stop == StopDepthCap && c.Depth != limit {
			v.add(row, p, "depth_cap at depth %d, limit %d", c.Depth, limit)
		}
	}
}

// Error joins violations into one error (nil for none).
func Error(vs []Violation) error {
	if len(vs) == 0 {
		return nil
	}
	errs := make([]error, len(vs))
	for i, x := range vs {
		errs[i] = errors.New(x.String())
	}
	return errors.Join(errs...)
}
