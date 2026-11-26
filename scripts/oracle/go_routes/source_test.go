package goroutes

// The source oracle of task T023. The chi/gin
// oracle (oracle_test.go) rebuilds the registration trees from
// Graphvein's own composition graph, so it checks composition and
// dispatch given Graphvein's recognition. This oracle checks recognition
// too: it compiles the go-routes fixture's source module
// (testdata/fixtures/go-routes/source, Graphvein's own test code, never
// corpus code) with the real net/http, chi and gin at the pinned versions,
// builds each case's real handler by calling the case function, and
// dispatches
//   - every path Graphvein claims for a route: the request must reach the
//     route's own handler (a disagreement otherwise), so wrong roots,
//     incomplete sibling sets, unproven wrapper and closure proofs and
//     wrong handlers all show;
//   - for each claimed path, its other slash form when the route does not
//     claim that form too (TOP-1 of 2026-11-18): the request must not reach
//     the route (a disagreement otherwise);
//   - the own pattern of every route Graphvein gives no path: the result
//     (reached or not) is reported, never a disagreement, since a refusal
//     claims nothing.
// Each handler writes a tag in the X-Route header: tg.T("Case/label") and
// tg.G("Case/label") write "Case/label"; a named handler writes its own
// name (usersHandler, Pets.List).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"example.com/gvsource/cases"
)

// sourceRoute is one route of source-routes.json (Graphvein's
// TestGoRoutesSourceExport).
type sourceRoute struct {
	Identity          string `json:"identity"`
	Owner             string `json:"owner"`
	Framework         string `json:"framework"`
	Method            string `json:"method"`
	Pattern           string `json:"pattern"`
	Host              string `json:"host"`
	Handler           string `json:"handler"`
	HandlerExpression string `json:"handler_expression"`
	Paths             []struct {
		Template      string `json:"template"`
		TrailingSlash bool   `json:"trailing_slash"`
	} `json:"paths"`
}

// sourceCases builds every case's real handler. R11 serves through
// cases.Listen, which is replaced to capture the handler.
func sourceCases() map[string]func() http.Handler {
	return map[string]func() http.Handler{
		"R1": cases.R1, "R2": cases.R2, "R6": cases.R6, "R9": cases.R9, "R10": cases.R10, "R12": cases.R12,
		"R11": func() http.Handler {
			var got http.Handler
			cases.Listen = func(_ string, h http.Handler) error { got = h; return nil }
			cases.R11()
			return got
		},
		"H1": cases.H1, "H2": cases.H2, "H3": cases.H3, "H4": cases.H4, "H5": cases.H5, "H6": cases.H6,
		"H7": cases.H7, "H8": cases.H8, "H9": cases.H9, "H10": cases.H10,
		"P1": cases.P1, "P2": cases.P2, "P3": cases.P3, "P4": cases.P4, "P5": cases.P5, "P6": cases.P6,
		"P12": cases.P12, "P13": cases.P13, "P14": cases.P14, "P15": cases.P15, "P16": cases.P16,
		"P17": cases.P17, "P18": cases.P18, "P19": cases.P19, "P20": cases.P20, "P21": cases.P21, "P22": cases.P22, "P23": cases.P23,
		"PW1": cases.PW1, "PW2": cases.PW2, "PW3": cases.PW3, "PW4": cases.PW4, "PW5": cases.PW5,
		"PW7": cases.PW7, "PW8": cases.PW8, "PC1": cases.PC1,
		"R13": cases.R13, "R14": cases.R14, "H14": cases.H14,
		"V1": cases.V1, "V2": cases.V2, "V3": cases.V3, "V4": cases.V4, "V5": cases.V5, "V6": cases.V6, "V7": cases.V7, "V8": cases.V8, "V9": cases.V9,
		"U1": func() http.Handler { return cases.U1().(http.Handler) },
		"U2": func() http.Handler { return cases.U2().(*http.Server).Handler },
	}
}

var tagCall = regexp.MustCompile(`^tg\.[TG]\("([^"]+)"\)$`)

// routeTag is the tag a route's handler writes and the case that
// registers it: from a tg.T/tg.G call, or a named handler's name with the
// case its registering function names. ok is false for a handler the
// oracle cannot tag (an inline closure, StripPrefix of a parameter).
func routeTag(r sourceRoute) (tag, caseName string, ok bool) {
	if m := tagCall.FindStringSubmatch(r.HandlerExpression); m != nil {
		return m[1], strings.SplitN(m[1], "/", 2)[0], true
	}
	if r.Handler != "" {
		name := r.Handler[strings.LastIndex(r.Handler, "#")+1:]
		owner := r.Owner[strings.LastIndex(r.Owner, "#")+1:]
		return name, owner, true
	}
	return "", "", false
}

// samplePath fills a template: a parameter becomes 7 (a value chi's
// regexp parameters of the fixture accept), a catch-all two segments.
func samplePath(template string, trailing bool) string {
	parts := strings.Split(template, "/")
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "{*"):
			parts[i] = "gv7/gv8"
		case strings.HasPrefix(p, "{"):
			parts[i] = "7"
		}
	}
	out := strings.Join(parts, "/")
	if trailing && !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

// ownPath is a refused route's own pattern as a request path (its method
// and host removed, wildcards filled).
func ownPath(r sourceRoute) string {
	p := r.Pattern
	if i := strings.IndexByte(p, ' '); i >= 0 && r.Framework == "net/http" {
		p = p[i+1:]
	}
	if r.Framework == "net/http" && !strings.HasPrefix(p, "/") {
		p = p[strings.IndexByte(p, '/'):]
	}
	p = strings.ReplaceAll(p, "{$}", "")
	re := regexp.MustCompile(`\{[^}]*\}|:[A-Za-z_]+|\*[A-Za-z_]*`)
	p = re.ReplaceAllStringFunc(p, func(s string) string {
		if strings.HasPrefix(s, "*") || strings.HasSuffix(s, "...}") {
			return "gv7/gv8"
		}
		return "7"
	})
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// sourceDispatch sends one request and returns the X-Route header.
func sourceDispatch(h http.Handler, method, host, path string) (tag string) {
	defer func() {
		if recover() != nil {
			tag = "panic"
		}
	}()
	if method == "ANY" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, "http://example.org"+path, nil)
	if host != "" {
		req.Host = host
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Header().Get("X-Route")
}

// TestGoRoutesSourceOracle runs the source oracle on
// GRAPHVEIN_GO_ROUTES_SOURCE/source-routes.json.
func TestGoRoutesSourceOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_SOURCE")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_SOURCE is not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, "source-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var routes []sourceRoute
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	built := map[string]http.Handler{}
	for name, f := range sourceCases() {
		built[name] = f()
	}
	var claims, agree, refused, confirmed, reached, untagged, slashChecks int
	var disagreements, refusedReached []string
	for _, r := range routes {
		tag, caseName, ok := routeTag(r)
		h := built[caseName]
		if !ok || h == nil {
			untagged++
			continue
		}
		if len(r.Paths) == 0 {
			refused++
			got := sourceDispatch(h, r.Method, r.Host, ownPath(r))
			if got == tag {
				reached++
				refusedReached = append(refusedReached, r.Identity+" at "+ownPath(r))
			} else {
				confirmed++
			}
			continue
		}
		claimed := map[string]bool{}
		for _, p := range r.Paths {
			claimed[fmt.Sprintf("%s|%v", p.Template, p.TrailingSlash)] = true
		}
		for _, p := range r.Paths {
			claims++
			path := samplePath(p.Template, p.TrailingSlash)
			if got := sourceDispatch(h, r.Method, r.Host, path); got != tag {
				disagreements = append(disagreements, r.Identity+": "+r.Method+" "+path+" answered by "+strings.TrimSpace(got+" ")+", want "+tag)
				continue
			}
			agree++
			// The other slash form (TOP-1 of 2026-11-18): a form the route
			// does not claim must not reach it (gin redirects, chi and
			// net/http answer 404 or another route). Root and catch-all
			// templates have no other form.
			if p.Template == "/" || strings.Contains(p.Template, "{*") || claimed[fmt.Sprintf("%s|%v", p.Template, !p.TrailingSlash)] {
				continue
			}
			slashChecks++
			other := samplePath(p.Template, !p.TrailingSlash)
			if got := sourceDispatch(h, r.Method, r.Host, other); got == tag {
				disagreements = append(disagreements, r.Identity+": "+r.Method+" "+other+" (the unclaimed slash form) answered by the route")
			}
		}
	}
	sort.Strings(disagreements)
	sort.Strings(refusedReached)
	t.Logf("source oracle: %d claimed paths, %d agree; %d unclaimed slash forms checked; %d refused routes: %d own patterns not reached by the route (refusal confirmed), %d reached (refusal conservative); %d routes untagged", claims, agree, slashChecks, refused, confirmed, reached, untagged)
	for _, r := range refusedReached {
		t.Logf("refusal conservative: %s", r)
	}
	for _, d := range disagreements {
		t.Errorf("disagreement: %s", d)
	}
}
