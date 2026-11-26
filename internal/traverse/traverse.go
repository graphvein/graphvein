// Package traverse is the breadth-first graph walk shared by change impact
// (ADR 0056, internal/changes) and the architecture chain (ADR 0079,
// internal/server): a relation table says which stored edges a walk
// follows, and the walk applies the same depth, member and fan-out caps to
// both, reporting every cap that binds. Each caller keeps its own relation
// table and output; this package only decides which candidate edges reach
// a new node at which step.
package traverse

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// Traversal caps (ADR 0056 defaults, reused by the chain, ADR 0079).
const (
	// MaxDepth is the deepest step a walk may take. It was 6 until T042
	// raised it to the project walk's 10: a route-to-frontend chain through
	// three helper functions needs 7 steps. The member and fan-out caps
	// below are unchanged, so a deeper walk still stops at the same size.
	MaxDepth = 10
	// MaxReached is the most nodes a walk may reach, seeds excluded.
	MaxReached = 2000
	// MaxFanOut is the most new nodes one node may lead to in one step.
	MaxFanOut = 200
)

// ChainDefaultDepth is the architecture chain's depth when a request names
// none (ADR 0079). It stayed 6 when MaxDepth rose to 10 (T042), so a chain
// costs what it did unless someone asks for more. Change impact's default
// is changes.DefaultDepth.
const ChainDefaultDepth = 6

// Names of the caps that can bind, as reported to readers.
const (
	CapDepth   = "depth"
	CapReached = "reached_nodes"
	CapFanOut  = "fan_out"
)

// CapOrder is the stable order in which bound caps are listed.
var CapOrder = []string{CapDepth, CapReached, CapFanOut}

// Rule is one followed relation of a relation table: from a node whose
// kind is in From (nil: any kind), the edges of kind Edge in direction Dir,
// to a node whose kind is in To (nil: any kind). ID names the claim
// contract row the rule implements.
type Rule struct {
	ID   string
	From []string
	Edge string
	Dir  store.Direction
	To   []string
}

// Match returns the first rule of rules that follows edge e from the node
// at its fromKind end in direction dir, or nil. skip, when not nil, refuses
// a rule for this edge (change impact follows imports one hop only).
func Match(rules []Rule, fromKind string, e contract.Edge, dir store.Direction, skip func(*Rule) bool) *Rule {
	other := Kind(e.Source)
	if dir == store.Outgoing {
		other = Kind(e.Target)
	}
	for i := range rules {
		r := &rules[i]
		if r.Edge != e.Kind || r.Dir != dir {
			continue
		}
		if r.From != nil && !contains(r.From, fromKind) {
			continue
		}
		if r.To != nil && !contains(r.To, other) {
			continue
		}
		if skip != nil && skip(r) {
			continue
		}
		return r
	}
	return nil
}

// Kind is the kind part of a node identity ("" when it has none).
func Kind(id string) string {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[:i]
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Candidate is one edge a frontier node may follow: From is the frontier
// node, To the node it leads to, Rule the relation that follows it. Data
// carries whatever the caller attached to the candidate.
type Candidate struct {
	From string
	To   string
	Edge contract.Edge
	Dir  store.Direction
	Rule *Rule
	Data any
}

// Bound is one cap that bound at one step: Beyond counts the distinct
// nodes the cap kept out of that step (for the depth cap, the nodes one
// step past the last).
type Bound struct {
	Cap    string `json:"cap"`
	Step   int    `json:"step"`
	Beyond int    `json:"beyond"`
}

// Walk is one breadth-first traversal. Its state (reached count, bound
// caps) carries over between Run calls, so a caller may run it from
// several seed sets that share the caps (change impact walks present and
// removed seeds in different graphs).
type Walk struct {
	// Depth is the deepest step, 1 to MaxDepth.
	Depth int
	// Visited reports whether a node is already a seed or reached.
	Visited func(id string) bool
	// Candidates lists the edges the frontier may follow at step d, in a
	// stable order. probe is true when the walk only asks whether the
	// frontier past the depth cap could continue: the call must then leave
	// the caller's state as it was.
	Candidates func(frontier []string, d int, probe bool) ([]Candidate, error)
	// Seen, when not nil, sees every candidate of a step in order, before
	// the walk decides whether it reaches a new node.
	Seen func(c Candidate)
	// Reach records that c reaches c.To at step d; afterwards Visited(c.To)
	// must be true.
	Reach func(c Candidate, d int)

	reached int
	caps    map[string]bool
	bounds  []Bound
}

// Run walks from seeds (already visited) up to Depth steps.
func (w *Walk) Run(seeds []string) error {
	return w.RunLevels(map[int][]string{0: seeds})
}

// RunLevels continues the walk from nodes already visited at different
// distances: levels maps a distance to the nodes placed there, and each
// step's frontier is the nodes the previous step reached plus those placed
// at its distance. Change impact uses it at project scope (T022 B7) to
// walk a member's graph from project link sources at the link's distance;
// Run is the case of seeds at distance 0.
func (w *Walk) RunLevels(levels map[int][]string) error {
	if w.caps == nil {
		w.caps = map[string]bool{}
	}
	if len(levels) == 0 {
		return nil
	}
	first, last := -1, -1
	for d := range levels {
		if first < 0 || d < first {
			first = d
		}
		if d > last {
			last = d
		}
	}
	frontier := append([]string{}, levels[first]...)
	for d := first; len(frontier) > 0 || d < last; d++ {
		var next []string
		if len(frontier) > 0 {
			if d+1 > w.Depth {
				beyond, err := w.beyond(frontier, d+1)
				if err != nil {
					// The cap may hide nodes; say it binds rather than not.
					w.bind(CapDepth, d+1, 0)
					return nil
				}
				if beyond > 0 {
					w.bind(CapDepth, d+1, beyond)
				}
				return nil
			}
			var err error
			if next, err = w.step(frontier, d+1); err != nil {
				return err
			}
		}
		frontier = append(next, levels[d+1]...)
	}
	return nil
}

// Admit counts one node the caller reached outside a step (a project link
// source, T022 B7) against the reached-nodes cap: false, with the cap
// bound at step d, when the cap leaves no room.
func (w *Walk) Admit(d int) bool {
	if w.reached >= MaxReached {
		w.Bind(CapReached, d, 1)
		return false
	}
	w.reached++
	return true
}

// Bind records that a cap bound at step d outside a step of the walk (a
// project link past the depth or fan-out cap), keeping beyond nodes out.
func (w *Walk) Bind(cap string, step, beyond int) {
	if w.caps == nil {
		w.caps = map[string]bool{}
	}
	w.bind(cap, step, beyond)
}

// beyond counts the distinct unvisited nodes the frontier could reach one
// step past the depth cap.
func (w *Walk) beyond(frontier []string, d int) (int, error) {
	cands, err := w.Candidates(frontier, d, true)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, c := range cands {
		if !w.Visited(c.To) {
			seen[c.To] = true
		}
	}
	return len(seen), nil
}

func (w *Walk) step(frontier []string, d int) ([]string, error) {
	cands, err := w.Candidates(frontier, d, false)
	if err != nil {
		return nil, err
	}
	fan := map[string]int{}
	fanned, rest := map[string]bool{}, map[string]bool{}
	var next []string
	for i, c := range cands {
		if w.Seen != nil {
			w.Seen(c)
		}
		if w.Visited(c.To) {
			continue
		}
		if fan[c.From] >= MaxFanOut {
			fanned[c.To] = true
			continue
		}
		if w.reached >= MaxReached {
			for _, r := range cands[i:] {
				if !w.Visited(r.To) {
					rest[r.To] = true
				}
			}
			break
		}
		fan[c.From]++
		w.Reach(c, d)
		w.reached++
		next = append(next, c.To)
	}
	// A node kept out by fan-out may still be reached through another
	// frontier node in the same step.
	n := 0
	for id := range fanned {
		if !w.Visited(id) {
			n++
		}
	}
	if len(fanned) > 0 {
		w.bind(CapFanOut, d, n)
	}
	if len(rest) > 0 {
		w.bind(CapReached, d, len(rest))
	}
	return next, nil
}

func (w *Walk) bind(cap string, step, beyond int) {
	w.caps[cap] = true
	w.bounds = append(w.bounds, Bound{Cap: cap, Step: step, Beyond: beyond})
}

// Caps lists the caps that bound, in CapOrder.
func (w *Walk) Caps() []string {
	out := []string{}
	for _, c := range CapOrder {
		if w.caps[c] {
			out = append(out, c)
		}
	}
	return out
}

// Bounds lists every binding cap per step, in the order they bound.
func (w *Walk) Bounds() []Bound {
	return append([]Bound{}, w.bounds...)
}

// BoundText is the sentence a reader sees for a bound cap outside the
// chain (which words its own as members): it names the cap, where it
// bound and how many nodes it kept out, so a cut never reads as "nothing
// beyond". A depth bound sits one step past the last step walked.
func BoundText(b Bound) string {
	switch b.Cap {
	case CapDepth:
		return fmt.Sprintf("depth cap reached (%d): %d beyond step %d", b.Step-1, b.Beyond, b.Step-1)
	case CapFanOut:
		return fmt.Sprintf("fan-out cap (%d new nodes from one node) bound at step %d: %d not followed", MaxFanOut, b.Step, b.Beyond)
	case CapReached:
		return fmt.Sprintf("reached-node cap (%d) bound at step %d: %d beyond", MaxReached, b.Step, b.Beyond)
	}
	return fmt.Sprintf("cap %s bound at step %d: %d beyond", b.Cap, b.Step, b.Beyond)
}
