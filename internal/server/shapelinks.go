package server

import (
	"sort"
	"strconv"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/query"
)

// This file holds the shape_links of GET /api/v0/api/endpoint (T029, ADR
// 0081 "Editor links on shape chips"): the editor link of every file:line
// a provenance chip of the route's request and response shape shows, so
// the web UI opens chips in the editor through links the server built,
// never through a URL it builds itself.

// shapeLinkKey is the key of one position in shape_links: "file:line".
func shapeLinkKey(file string, line int) string {
	return file + ":" + strconv.Itoa(line)
}

// shapeChipPositions lists the file:line positions the shape chips of a
// route show, in key order without duplicates: the route's registration
// (the Explorer's handler chip and the fallback provenance of a name a
// security dependency reads), the first step of each stored FastAPI
// security dependency's chain (where it is declared), and every
// provenance of the stored shape (items, section evidence, security
// schemes and models). Chain steps are not chips and are left out.
func shapeChipPositions(route contract.Node, shape *routeshape.Shape) []shapePosition {
	seen := map[string]shapePosition{}
	add := func(file string, line int) {
		if file == "" || file == "." || line < 1 {
			return
		}
		seen[shapeLinkKey(file, line)] = shapePosition{file: file, line: line}
	}
	if route.Span != nil {
		add(route.File, route.Span.StartLine)
	}
	for _, dep := range objects(route.Attributes["security_dependencies"]) {
		steps := objects(dep["chain"])
		if len(steps) == 0 {
			continue
		}
		file, _ := steps[0]["file"].(string)
		span, _ := steps[0]["span"].(map[string]any)
		line, ok := intValue(span["start_line"])
		if ok {
			add(file, int(line))
		}
	}
	if shape != nil {
		for _, p := range shapeProvenances(shape) {
			add(p.File, p.Line)
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]shapePosition, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

// shapePosition is one file and line a chip shows.
type shapePosition struct {
	file string
	line int
}

// shapeProvenances lists every provenance of a shape that a chip shows.
func shapeProvenances(s *routeshape.Shape) []routeshape.Provenance {
	var out []routeshape.Provenance
	for _, sec := range []*routeshape.Section{&s.Path.Section, &s.Query.Section, &s.Header.Section, &s.Cookie.Section, &s.Body.Section, &s.Responses.Section, &s.Security.Section} {
		if sec.Evidence != nil {
			out = append(out, *sec.Evidence)
		}
	}
	for _, p := range []*routeshape.ParamSection{&s.Path, &s.Query, &s.Header, &s.Cookie} {
		for _, it := range p.Items {
			out = append(out, it.Provenance)
		}
	}
	for _, it := range s.Body.Items {
		out = append(out, it.Provenance)
	}
	for _, it := range s.Responses.Items {
		out = append(out, it.Provenance)
	}
	for _, it := range s.Security.Items {
		out = append(out, it.Provenance)
	}
	for _, it := range s.Schemes {
		out = append(out, it.Provenance)
	}
	for _, it := range s.Models {
		out = append(out, it.Provenance)
	}
	return out
}

// shapeLinks builds the editor link of every chip position with the same
// presenter.link the schema reasons use (the line, no column). It returns
// nil when no position has a link, such as when editor links are
// unavailable, so the field is then absent.
func shapeLinks(p *presenter, route contract.Node, shape *routeshape.Shape) map[string]*query.IDELink {
	var out map[string]*query.IDELink
	for _, pos := range shapeChipPositions(route, shape) {
		link := p.link(pos.file, contract.Span{StartLine: pos.line, EndLine: pos.line})
		if link == nil {
			continue
		}
		if out == nil {
			out = map[string]*query.IDELink{}
		}
		out[shapeLinkKey(pos.file, pos.line)] = link
	}
	return out
}
