package routes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// The security section of a route shape for Express, Axum, gin, chi and
// Next.js (task T029, docs/contract/claims/route-shape-security.md,
// rows SH-S1..SH-S24). Each framework reads the auth it can prove runs for
// the route (a middleware of a closed library list, a typed extractor, a
// layer) into secSources; secCollector turns them into the shared model.
// A form no row reads states no scheme: custom auth stays as written in
// the route's middleware attributes.

// secSource is one auth middleware, extractor or layer that runs for the
// route: the credential forms it accepts (alternatives, each a set of
// schemes sent together) and whether it refuses a request without them.
type secSource struct {
	// rule is the row that reads the form; label names it in reasons.
	rule, label string
	// kind is fact when the source refuses requests without a credential
	// before the handler runs, intended when it may let them through.
	kind routeshape.Kind
	alts [][]routeshape.Scheme
	file string
	line int
	// reasons make the section partial (another form it accepts that the
	// model cannot express, an option not read).
	reasons []string
}

// secCollector gathers a route's sources and the reasons its section is
// not complete.
type secCollector struct {
	sources []secSource
	reasons []string
	// complete is set by the framework when every middleware that runs
	// before the handler was read (rows SH-S12, SH-S22).
	complete bool
}

// maxSecRequirements bounds the alternatives one route's sources combine
// into; beyond it no requirement is stated (row SH-S1).
const maxSecRequirements = 16

func (c *secCollector) reason(r string) {
	for _, old := range c.reasons {
		if old == r {
			return
		}
	}
	c.reasons = append(c.reasons, r)
}

// apply writes the security section and schemes (row SH-S1). With no
// source the section stays not_read: auth Graphvein does not read is no
// evidence of none.
func (c *secCollector) apply(sh *routeshape.Shape) {
	if len(c.sources) == 0 {
		return
	}
	for _, s := range c.sources {
		for _, r := range s.reasons {
			c.reason(r)
		}
	}
	// Every source must pass: one requirement per combination of their
	// alternatives (AND across sources, OR within one).
	combos := [][]routeshape.Scheme{{}}
	kind := routeshape.Fact
	for _, s := range c.sources {
		if s.kind != routeshape.Fact {
			kind = routeshape.Intended
		}
		var next [][]routeshape.Scheme
		for _, base := range combos {
			for _, alt := range s.alts {
				next = append(next, append(append([]routeshape.Scheme(nil), base...), alt...))
			}
		}
		combos = next
		if len(combos) > maxSecRequirements {
			break
		}
	}
	defined := map[string]bool{}
	for _, sc := range sh.Schemes {
		defined[sc.Name] = true
	}
	if len(combos) > maxSecRequirements {
		tagShape("SH-S1/cap")
		c.reason(fmt.Sprintf("more than %d combinations of credentials: no requirement is stated", maxSecRequirements))
		combos = nil
	}
	first := c.sources[0]
	prov := routeshape.Provenance{Kind: kind, Rule: first.rule, File: first.file, Line: first.line, Chain: []routeshape.Step{}, Confidence: 1}
	if len(c.sources) > 1 {
		tagShape("SH-S1/and")
		prov.Rule = "SH-S1"
	}
	if kind == routeshape.Intended {
		tagShape("SH-S1/intended")
	}
	for _, combo := range combos {
		req := routeshape.Requirement{Schemes: []routeshape.SchemeUse{}, Provenance: prov}
		used := map[string]bool{}
		for _, sc := range combo {
			if used[sc.Name] {
				continue
			}
			used[sc.Name] = true
			if !defined[sc.Name] {
				defined[sc.Name] = true
				sh.Schemes = append(sh.Schemes, sc)
			}
			req.Schemes = append(req.Schemes, routeshape.SchemeUse{Name: sc.Name, Scopes: []string{}})
		}
		if len(req.Schemes) == 0 {
			continue
		}
		sh.Security.Items = append(sh.Security.Items, req)
	}
	if len(sh.Security.Items) > 1 {
		tagShape("SH-S1/alternatives")
	}
	detailed := true
	for _, sc := range sh.Schemes {
		if sc.Type == routeshape.SchemeUnknown || sc.Type == routeshape.SchemeAPIKey && sc.ParamName == nil {
			detailed = false
		}
	}
	if c.complete && detailed && len(c.reasons) == 0 && kind == routeshape.Fact && len(sh.Security.Items) > 0 {
		tagShape("SH-S1/complete")
		ev := sh.Security.Items[0].Provenance
		sh.Security.State, sh.Security.Evidence = routeshape.Complete, &ev
		return
	}
	tagShape("SH-S1/partial")
	sh.Security.State = routeshape.Partial
	reasons := c.reasons
	if len(reasons) == 0 {
		reasons = []string{"not every middleware that runs before the handler is read"}
	}
	sh.Security.Reason = strings.Join(reasons, "; ")
}

// Scheme constructors (row SH-S2). Names follow the scheme's fields, so
// one name is one definition everywhere in the generated document.

func bearerScheme(rule, file string, line int) routeshape.Scheme {
	tagShape("SH-S2/http")
	return routeshape.Scheme{Name: "bearerAuth", Type: routeshape.SchemeHTTP, Scheme: strPtr("bearer"), Provenance: secProv(rule, file, line)}
}

func basicScheme(rule, file string, line int) routeshape.Scheme {
	tagShape("SH-S2/http")
	return routeshape.Scheme{Name: "basicAuth", Type: routeshape.SchemeHTTP, Scheme: strPtr("basic"), Provenance: secProv(rule, file, line)}
}

var schemeNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// apiKeyScheme is a key sent in a header, query parameter or cookie; a
// nil name is a key whose name is not read.
func apiKeyScheme(in string, name *string, rule, file string, line int) routeshape.Scheme {
	tagShape("SH-S2/apiKey")
	n := "apiKey_" + in
	if name != nil {
		n += "_" + schemeNameUnsafe.ReplaceAllString(*name, "_")
	}
	return routeshape.Scheme{Name: n, Type: routeshape.SchemeAPIKey, In: strPtr(in), ParamName: name, Provenance: secProv(rule, file, line)}
}

// unknownScheme is auth whose credential form is not read: the scheme
// is named after its library and has no detail.
func unknownScheme(name, rule, file string, line int) routeshape.Scheme {
	tagShape("SH-S2/unknown")
	return routeshape.Scheme{Name: schemeNameUnsafe.ReplaceAllString(name, "_"), Type: routeshape.SchemeUnknown, Provenance: secProv(rule, file, line)}
}

func secProv(rule, file string, line int) routeshape.Provenance {
	return routeshape.Provenance{Kind: routeshape.Fact, Rule: rule, File: file, Line: line, Chain: []routeshape.Step{}, Confidence: 1}
}

// one is a single alternative of one scheme.
func one(s routeshape.Scheme) [][]routeshape.Scheme { return [][]routeshape.Scheme{{s}} }
