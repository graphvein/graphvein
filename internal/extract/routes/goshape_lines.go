package routes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// ShapeLines renders a route shape as compact, deterministic lines for
// golden files and corpus reports: one line per section with its state
// and reason, one per item with its provenance, one per chain record.
func ShapeLines(identity string, s *routeshape.Shape) []string {
	out := []string{identity}
	prov := func(p routeshape.Provenance) string {
		chain := make([]string, len(p.Chain))
		for i, c := range p.Chain {
			chain[i] = fmt.Sprintf("%s@%d", c.Function, c.Line)
		}
		text := fmt.Sprintf("%s %s %s:%d", p.Kind, p.Rule, p.File, p.Line)
		if len(chain) > 0 {
			text += " via " + strings.Join(chain, ">")
		}
		return text
	}
	schema := func(sc *routeshape.Schema) string {
		if sc == nil {
			return "-"
		}
		data, _ := json.Marshal(sc)
		return string(data)
	}
	section := func(name string, sec routeshape.Section) {
		line := fmt.Sprintf("  %s: %s", name, sec.State)
		if sec.Reason != "" {
			line += " (" + sec.Reason + ")"
		}
		out = append(out, line)
	}
	params := []struct {
		name string
		sec  routeshape.ParamSection
	}{{"path", s.Path}, {"query", s.Query}, {"header", s.Header}, {"cookie", s.Cookie}}
	for _, p := range params {
		section(p.name, p.sec.Section)
		for _, it := range p.sec.Items {
			out = append(out, fmt.Sprintf("    %s %s %s %s | %s", it.Name, it.Access, it.Presence, schema(it.Schema), prov(it.Provenance)))
		}
	}
	str := func(p *string) string {
		if p == nil {
			return "-"
		}
		return *p
	}
	section("body", s.Body.Section)
	for _, it := range s.Body.Items {
		out = append(out, fmt.Sprintf("    %s %s | %s", str(it.ContentType), schema(it.Schema), prov(it.Provenance)))
	}
	section("responses", s.Responses.Section)
	for _, it := range s.Responses.Items {
		out = append(out, fmt.Sprintf("    %s %s %s | %s", it.Status, str(it.ContentType), schema(it.Schema), prov(it.Provenance)))
	}
	section("security", s.Security.Section)
	for _, c := range s.Follow.Chains {
		steps := make([]string, len(c.Steps))
		for i, st := range c.Steps {
			steps[i] = fmt.Sprintf("%s@%d", st.Function, st.Line)
		}
		at := ""
		if c.At != nil {
			at = fmt.Sprintf(" at %s:%d %s", c.At.File, c.At.Line, c.At.Function)
		}
		detail := ""
		if c.Detail != "" {
			detail = " (" + c.Detail + ")"
		}
		out = append(out, fmt.Sprintf("  chain %s from %s depth %d [%s] %s%s%s", c.Section, c.Start.Function, c.Depth, strings.Join(steps, ">"), c.Stop, at, detail))
	}
	return out
}
