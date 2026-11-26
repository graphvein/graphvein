package routes

import (
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// TestPythonCallText: a router constructor hop is named by its whole call
// rebuilt from the facts (which keep no text for the call), so its
// registration hash covers every argument (T029 FU-2 review).
func TestPythonCallText(t *testing.T) {
	ctor := facts.RouteExpr{Kind: "call", Object: &facts.RouteExpr{Kind: "identifier", Text: "APIRouter"},
		Args: []facts.RouteExpr{{Text: "x"}},
		Keywords: []facts.RouteKeyword{
			{Name: "prefix", Value: facts.RouteExpr{Text: `os.environ["OWN"]`}},
			{Name: "dependencies", Value: facts.RouteExpr{Text: "[Depends(auth)]"}},
			{Value: facts.RouteExpr{Text: "opts"}},
		}}
	want := `APIRouter(x,prefix=os.environ["OWN"],dependencies=[Depends(auth)],**opts)`
	if got := pythonCallText(ctor); got != want {
		t.Errorf("pythonCallText = %q, want %q", got, want)
	}
	if got := pythonCallText(facts.RouteExpr{Kind: "identifier", Text: "r"}); got != "r" {
		t.Errorf("a non-call keeps its text: %q", got)
	}
	if got := pythonCallText(facts.RouteExpr{Kind: "call"}); got != "" {
		t.Errorf("a call without a callee: %q", got)
	}
}
