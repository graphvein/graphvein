package traverse

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// tree is a graph given as children lists; walk runs a Walk over it.
func walk(t *testing.T, children map[string][]string, depth int) (*Walk, map[string]int) {
	t.Helper()
	at := map[string]int{"root": 0}
	rule := &Rule{ID: "R", Edge: "calls", Dir: store.Outgoing}
	w := &Walk{Depth: depth, Visited: func(id string) bool { _, ok := at[id]; return ok }}
	w.Candidates = func(frontier []string, d int, probe bool) ([]Candidate, error) {
		var out []Candidate
		for _, f := range frontier {
			for _, c := range children[f] {
				out = append(out, Candidate{From: f, To: c, Rule: rule})
			}
		}
		return out, nil
	}
	w.Reach = func(c Candidate, d int) { at[c.To] = d }
	if err := w.Run([]string{"root"}); err != nil {
		t.Fatal(err)
	}
	return w, at
}

func names(prefix string, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, prefix+strconv.Itoa(i))
	}
	return out
}

// TestWalkCaps checks each cap and the count of nodes it keeps out.
func TestWalkCaps(t *testing.T) {
	// Depth: a chain a -> b -> c -> d with depth 2 keeps c out (beyond 1).
	w, at := walk(t, map[string][]string{"root": {"a"}, "a": {"b"}, "b": {"c"}}, 2)
	if !reflect.DeepEqual(w.Bounds(), []Bound{{CapDepth, 3, 1}}) || len(at) != 3 || !reflect.DeepEqual(w.Caps(), []string{CapDepth}) {
		t.Errorf("depth: bounds %v, reached %v", w.Bounds(), at)
	}
	// A depth cap that hides nothing new does not bind.
	w, _ = walk(t, map[string][]string{"root": {"a"}, "a": {"root"}}, 1)
	if len(w.Bounds()) != 0 {
		t.Errorf("depth without new nodes: %v", w.Bounds())
	}
	// Fan-out: 203 children, one of them also reached through a sibling.
	kids := names("k", MaxFanOut+3)
	w, at = walk(t, map[string][]string{"root": append(kids, "x"), "x": {}}, 2)
	if !reflect.DeepEqual(w.Bounds(), []Bound{{CapFanOut, 1, 4}}) || len(at) != MaxFanOut+1 {
		t.Errorf("fan-out: bounds %v, reached %d", w.Bounds(), len(at))
	}
	// Reached: 12 parents with 199 children each pass the 2,000 cap.
	children := map[string][]string{"root": names("p", 12)}
	for i := 0; i < 12; i++ {
		children["p"+strconv.Itoa(i)] = names("p"+strconv.Itoa(i)+"c", 199)
	}
	w, at = walk(t, children, 3)
	if len(at)-1 != MaxReached || !reflect.DeepEqual(w.Caps(), []string{CapReached}) {
		t.Fatalf("reached: %d, caps %v", len(at)-1, w.Caps())
	}
	if b := w.Bounds(); len(b) != 1 || b[0].Step != 2 || b[0].Beyond != 12+12*199-MaxReached {
		t.Errorf("reached: bounds %v", b)
	}
}

// TestMatch checks the relation table matching.
func TestMatch(t *testing.T) {
	rules := []Rule{
		{ID: "A", From: []string{"function"}, Edge: "calls", Dir: store.Incoming, To: []string{"function"}},
		{ID: "B", Edge: "calls", Dir: store.Incoming},
	}
	e := contract.Edge{Kind: "calls", Source: "class:a.py#C", Target: "function:a.py#f"}
	if r := Match(rules, "function", e, store.Incoming, nil); r == nil || r.ID != "B" {
		t.Errorf("class caller: %v", r)
	}
	e.Source = "function:a.py#g"
	if r := Match(rules, "function", e, store.Incoming, nil); r == nil || r.ID != "A" {
		t.Errorf("function caller: %v", r)
	}
	if r := Match(rules, "function", e, store.Incoming, func(r *Rule) bool { return true }); r != nil {
		t.Errorf("skipped: %v", r)
	}
	if r := Match(rules, "function", e, store.Outgoing, nil); r != nil {
		t.Errorf("outgoing: %v", r)
	}
}
