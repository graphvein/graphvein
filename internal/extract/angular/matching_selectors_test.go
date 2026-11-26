package angular

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/template"
)

func TestSelectorSubset(t *testing.T) {
	for _, text := range []string{"app-child", "[a]", "button[a=VALUE], app-x[x='a,b']", "[a=b=c]", "[x=\"\"]", "[a.b][*x]", "button\f,\v\u00a0[x]", "button,\ufeff[x]"} {
		if _, ok := parseSelector(text); !ok {
			t.Errorf("modeled selector rejected: %s", text)
		}
	}
	for _, text := range []string{"", " app-x", "app-x ", "button [x]", "div span", ".classy", "#id", ":not([x])", "*", "APP-X", "[x$]", "[x\\y]", "[x]tail", "[x],", ",[x]", "[x=\"a'b\"]", "button\u0085,[x]"} {
		if _, ok := parseSelector(text); ok {
			t.Errorf("unmodeled selector accepted: %s", text)
		}
	}
}

func TestMatchingEvidenceAndRecovery(t *testing.T) {
	span := func(a, b int) contract.Span {
		return contract.Span{StartLine: 1, EndLine: 1, StartColumn: a, EndColumn: b}
	}
	n := matchingNode{name: "app-x", span: span(0, 45), nameSpan: span(1, 6), attrs: map[string]matchingAttribute{"x": {value: "value", span: span(8, 9), valueSpan: span(8, 17)}}}
	for _, tc := range []struct {
		selector           string
		errors             []contract.Span
		matched, uncertain bool
		evidence           []contract.Span
	}{
		{"app-x[x]", nil, true, false, []contract.Span{span(1, 6), span(8, 9)}},
		{"[x=VALUE]", nil, true, false, []contract.Span{span(8, 17)}},
		{"[x]", []contract.Span{span(12, 13)}, true, false, []contract.Span{span(8, 9)}},
		{"[x=value]", []contract.Span{span(12, 13)}, false, true, []contract.Span{}},
		{"app-x", []contract.Span{span(2, 3)}, false, true, []contract.Span{}},
		{"[missing]", nil, false, false, []contract.Span{}},
	} {
		selectors, ok := parseSelector(tc.selector)
		if !ok {
			t.Fatal(tc.selector)
		}
		got, matched, uncertain := matchCandidate(n, selectors, tc.errors)
		if matched != tc.matched || uncertain != tc.uncertain || !reflect.DeepEqual(got, tc.evidence) {
			t.Errorf("%s: %v %t %t", tc.selector, got, matched, uncertain)
		}
	}
	if !onlyCommentsAndWhitespace(" \n<!-- a -->\t<!-- b -->") || onlyCommentsAndWhitespace("<!-- incomplete") || onlyCommentsAndWhitespace("<p/>") {
		t.Error("block adjacency")
	}
	if !onlyCommentsAndWhitespace("\u00a0<!-- comment -->\ufeff") || onlyCommentsAndWhitespace("\u0085") {
		t.Error("block adjacency must use Angular whitespace")
	}
	// A missing point at a child's half-open end belongs to the parent, not
	// to that child. It must keep the parent's nonmatch uncertain.
	f := template.Facts{Elements: []template.Element{{Location: template.Location{Span: span(10, 20)}}}, ErrorSpans: []contract.Span{span(20, 20)}}
	if !nodeRecovery(f, n) {
		t.Error("child end hid a parent recovery point")
	}
	n.attrs["x"] = matchingAttribute{value: "i", span: span(8, 9)}
	selectors, _ := parseSelector(`[x="İ"]`)
	if _, matched, _ := matchCandidate(n, selectors, nil); matched {
		t.Error("dotted capital I selector must not match ASCII i")
	}
}
