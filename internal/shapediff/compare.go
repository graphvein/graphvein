package shapediff

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// Directions of a difference, and the words the link details show
// (rows SD-8, SD-9). They never say "unused", "broken" or "error": the
// difference is what the code shows, not a verdict.
const (
	SentNotRead = "sent_not_read"
	ReadNotSent = "read_not_sent"
)

// Texts are the words of each direction.
var Texts = map[string]string{
	SentNotRead: "sent by the frontend, not read by the handler",
	ReadNotSent: "read by the handler, not sent by this call",
}

// Result is the comparison of one link. A nil Result means nothing was
// compared, which the link details show as nothing at all.
type Result struct {
	// Compared names the sections compared ("body", "query").
	Compared []string `json:"compared"`
	// Differences are sorted by section, field and direction; empty when
	// the compared sections agree (row SD-10).
	Differences []Difference `json:"differences"`
}

// Difference is one key one side has and the other does not.
type Difference struct {
	Row     string `json:"row"`
	Section string `json:"section"`
	// Field is the key's path in the body or the query name ("user.bio").
	Field     string `json:"field"`
	Direction string `json:"direction"`
	Text      string `json:"text"`
	// Call and Route are where each side is known.
	Call  Side `json:"call"`
	Route Side `json:"route"`
}

// Side is one side's provenance: a file and line, the row that read it,
// and how it is known (literal, typed, fact).
type Side struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Rule string `json:"rule"`
	Kind string `json:"kind"`
}

// Compare compares what a call sends with the route's shape. It returns
// nil unless at least one section is fully read on both sides.
func Compare(shape *routeshape.Shape, sends *Sends) *Result {
	if shape == nil || sends == nil {
		return nil
	}
	out := &Result{Compared: []string{}, Differences: []Difference{}}
	if sends.Body != nil {
		if obj, prov, ok := bodyObject(shape, sends.Body.ContentType); ok {
			out.Compared = append(out.Compared, routeshape.SectionBody)
			c := comparer{shape: shape, part: sends.Body, route: prov, out: out}
			c.object(obj, sends.Body.Fields, sends.Body.Complete, sends.Body.File, sends.Body.Line, "", 0)
		}
	}
	if sends.Query != nil {
		if params, prov, ok := queryParams(shape); ok {
			out.Compared = append(out.Compared, routeshape.SectionQuery)
			compareQuery(params, prov, sends.Query, out)
		}
	}
	if len(out.Compared) == 0 {
		return nil
	}
	sort.SliceStable(out.Differences, func(i, j int) bool {
		a, b := out.Differences[i], out.Differences[j]
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Direction < b.Direction
	})
	return out
}

// fullyRead reports a section that is complete, carries no caveat, and
// whose followed chains never reached the depth limit (rows SD-6, SD-7).
func fullyRead(shape *routeshape.Shape, s routeshape.Section, section string) bool {
	if s.State != routeshape.Complete || s.Reason != "" {
		return false
	}
	for _, c := range shape.Follow.Chains {
		if c.Section == section && c.Stop == routeshape.StopDepthCap {
			return false
		}
	}
	return true
}

// bodyObject returns the route's request body object for the media type
// the call sends (row SD-6): one fact item of that type whose schema is
// an object, or a reference to a fact model that is one.
func bodyObject(shape *routeshape.Shape, contentType string) (routeshape.Schema, routeshape.Provenance, bool) {
	if !fullyRead(shape, shape.Body.Section, routeshape.SectionBody) {
		return routeshape.Schema{}, routeshape.Provenance{}, false
	}
	var found []routeshape.Content
	for _, item := range shape.Body.Items {
		if item.ContentType != nil && mediaType(*item.ContentType) == mediaType(contentType) {
			found = append(found, item)
		}
	}
	if len(found) != 1 || found[0].Provenance.Kind != routeshape.Fact || found[0].Schema == nil {
		return routeshape.Schema{}, routeshape.Provenance{}, false
	}
	return objectOf(shape, *found[0].Schema, found[0].Provenance)
}

// mediaType is a content type without parameters, lower case.
func mediaType(t string) string {
	t, _, _ = strings.Cut(t, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// objectOf resolves a schema to an object of fact provenance: the schema
// itself, or the model a reference names (whose provenance is then the
// model's). Anything else (an array, a union, a model of another kind, an
// object that keeps unlisted keys) is not compared.
func objectOf(shape *routeshape.Shape, s routeshape.Schema, prov routeshape.Provenance) (routeshape.Schema, routeshape.Provenance, bool) {
	for hop := 0; s.Type == routeshape.TypeRef && hop < 4; hop++ {
		var model *routeshape.Model
		for i := range shape.Models {
			if shape.Models[i].Name == s.Ref {
				model = &shape.Models[i]
			}
		}
		if model == nil || model.Provenance.Kind != routeshape.Fact {
			return routeshape.Schema{}, prov, false
		}
		s, prov = model.Schema, model.Provenance
	}
	if s.Type != routeshape.TypeObject || s.Additional == routeshape.AdditionalAllowed {
		// An object that keeps keys it does not list may hand them to the
		// handler: a key it does not list may still be read.
		return routeshape.Schema{}, prov, false
	}
	return s, prov, true
}

type comparer struct {
	shape *routeshape.Shape
	part  *Part
	route routeshape.Provenance
	out   *Result
}

func (c *comparer) add(row, field, direction string, call Side, route routeshape.Provenance) {
	c.out.Differences = append(c.out.Differences, Difference{
		Row: row, Section: routeshape.SectionBody, Field: field, Direction: direction, Text: Texts[direction],
		Call: call, Route: Side{File: route.File, Line: route.Line, Rule: route.Rule, Kind: string(route.Kind)},
	})
}

// object compares one object level: every key sent that the route's
// object does not list (row SD-8), and, when every key sent is known,
// every key the route requires that is not sent (row SD-9). Keys both
// sides have are compared one level deeper when both are objects.
func (c *comparer) object(obj routeshape.Schema, sent []Field, complete bool, file string, line int, prefix string, depth int) {
	if depth > maxDepth {
		return
	}
	listed := map[string]routeshape.Field{}
	for _, f := range obj.Fields {
		listed[f.Name] = f
	}
	namesKnown := namesProven(obj.Fields)
	names := map[string]bool{}
	for _, f := range sent {
		names[f.Name] = true
		rf, ok := listed[f.Name]
		if !ok {
			if !namesKnown {
				continue
			}
			c.add("SD-8", prefix+f.Name, SentNotRead, Side{File: f.File, Line: f.Line, Rule: c.part.Rule, Kind: c.part.Form}, c.route)
			continue
		}
		if f.Nested == nil {
			continue
		}
		inner := rf.Schema
		if inner.Type == routeshape.TypeRef || inner.Type == routeshape.TypeObject {
			child, prov, ok := objectOf(c.shape, inner, c.route)
			if ok {
				saved := c.route
				c.route = prov
				c.object(child, f.Nested.Fields, f.Nested.Complete, f.File, f.Line, prefix+f.Name+".", depth+1)
				c.route = saved
			}
		}
	}
	if !complete {
		return
	}
	for _, f := range obj.Fields {
		if mustSend(f.Presence, &f.Schema, c.route) && !names[f.Name] {
			c.add("SD-9", prefix+f.Name, ReadNotSent, Side{File: file, Line: line, Rule: c.part.Rule, Kind: c.part.Form}, c.route)
		}
	}
}

// namesProven reports whether the keys an object accepts are exactly the
// names it lists (row SD-8). A field whose presence is unknown may be
// read under another name (a serde alias accepts it, also with
// deny_unknown_fields) or not at all, so a key the object does not list
// may still be read: no key of that object is reported as not read.
func namesProven(fields []routeshape.Field) bool {
	for _, f := range fields {
		if f.Presence == routeshape.PresenceUnknown {
			return false
		}
	}
	return true
}

// paramNamesProven is namesProven for query parameters.
func paramNamesProven(params []routeshape.Param) bool {
	for _, p := range params {
		if p.Presence == routeshape.PresenceUnknown {
			return false
		}
	}
	return true
}

// nullableRequiredRules are the rules whose required claim on a field
// that accepts null is proven: SH-F22 claims it only when the manifest
// proves Pydantic 2 (Pydantic 1 gives such a field a None default).
var nullableRequiredRules = map[string]bool{"SH-F22": true}

// mustSend reports a key the route requires (row SD-9): a fact-required
// key that cannot be null, or one whose rule proves the requirement for
// a key that can.
func mustSend(presence routeshape.Presence, schema *routeshape.Schema, prov routeshape.Provenance) bool {
	if presence != routeshape.Required || prov.Kind != routeshape.Fact {
		return false
	}
	if schema == nil || schema.Nullable || schema.Type == routeshape.TypeNull {
		return nullableRequiredRules[prov.Rule]
	}
	return true
}

// queryParams returns the route's query parameters when every one is a
// declared fact, or none when the code proves there is none (row SD-7).
func queryParams(shape *routeshape.Shape) ([]routeshape.Param, *routeshape.Provenance, bool) {
	q := shape.Query.Section
	if q.State == routeshape.ProvablyNone && q.Reason == "" && q.Evidence != nil {
		return nil, q.Evidence, true
	}
	if !fullyRead(shape, q, routeshape.SectionQuery) || q.Evidence == nil {
		return nil, nil, false
	}
	for _, p := range shape.Query.Items {
		if p.Provenance.Kind != routeshape.Fact || p.Access != routeshape.Declared {
			return nil, nil, false
		}
	}
	return shape.Query.Items, shape.Query.Evidence, true
}

func compareQuery(params []routeshape.Param, evidence *routeshape.Provenance, sent *Part, out *Result) {
	byName := map[string]routeshape.Param{}
	for _, p := range params {
		byName[p.Name] = p
	}
	route := routeshape.Provenance{}
	if evidence != nil {
		route = *evidence
	}
	namesKnown := paramNamesProven(params)
	names := map[string]bool{}
	for _, f := range sent.Fields {
		names[f.Name] = true
		if _, ok := byName[f.Name]; ok || !namesKnown {
			continue
		}
		out.Differences = append(out.Differences, Difference{
			Row: "SD-8", Section: routeshape.SectionQuery, Field: f.Name, Direction: SentNotRead, Text: Texts[SentNotRead],
			Call:  Side{File: f.File, Line: f.Line, Rule: sent.Rule, Kind: sent.Form},
			Route: Side{File: route.File, Line: route.Line, Rule: route.Rule, Kind: string(route.Kind)},
		})
	}
	if !sent.Complete {
		return
	}
	for _, p := range params {
		if mustSend(p.Presence, p.Schema, p.Provenance) && !names[p.Name] {
			out.Differences = append(out.Differences, Difference{
				Row: "SD-9", Section: routeshape.SectionQuery, Field: p.Name, Direction: ReadNotSent, Text: Texts[ReadNotSent],
				Call:  Side{File: sent.File, Line: sent.Line, Rule: sent.Rule, Kind: sent.Form},
				Route: Side{File: p.Provenance.File, Line: p.Provenance.Line, Rule: p.Provenance.Rule, Kind: string(p.Provenance.Kind)},
			})
		}
	}
}
