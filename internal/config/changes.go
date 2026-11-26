package config

import (
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// Changes holds the change journal limits (ADR 0056). The defaults equal
// the store's (store.DefaultJournalRuns and the others), which a test
// asserts; config does not import the store.
type Changes struct {
	// JournalRuns is how many journal runs are kept.
	JournalRuns int
	// JournalBudget bounds the journal's entry bytes as a factor of the
	// current graph's payload bytes.
	JournalBudget float64
	// MaxPins is how many pinned baselines a repository may hold.
	MaxPins int
	// MaxRefSnapshots is how many ref snapshots are kept, least recently
	// used dropped first.
	MaxRefSnapshots int
}

// DefaultChanges are the limits used when graphvein.yaml has no changes
// section or omits a key.
func DefaultChanges() Changes {
	return Changes{JournalRuns: 30, JournalBudget: 1.0, MaxPins: 5, MaxRefSnapshots: 4}
}

// Upper bounds keep a typo from making the journal unbounded in practice.
const (
	maxJournalRuns     = 1000
	maxJournalBudget   = 100.0
	maxPinsLimit       = 100
	maxRefSnapshotsCap = 100
)

func decodeChanges(n *yaml.Node) (Changes, error) {
	c := DefaultChanges()
	if n.Kind != yaml.MappingNode {
		return c, fmt.Errorf("changes must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return c, fmt.Errorf("line %d: changes keys must be strings", k.Line)
		}
		if seen[k.Value] {
			return c, fmt.Errorf("line %d: duplicate changes key %s", k.Line, k.Value)
		}
		seen[k.Value] = true
		var err error
		switch k.Value {
		case "journal_runs":
			c.JournalRuns, err = positiveInt(v, k.Value, maxJournalRuns)
		case "max_pins":
			c.MaxPins, err = positiveInt(v, k.Value, maxPinsLimit)
		case "max_ref_snapshots":
			// At least 2: a ref comparison A..B needs both snapshots kept
			// (Revision 3, R3-10).
			c.MaxRefSnapshots, err = intRange(v, k.Value, 2, maxRefSnapshotsCap)
		case "journal_budget":
			if v.Kind != yaml.ScalarNode || (v.Tag != "!!int" && v.Tag != "!!float") {
				err = fmt.Errorf("line %d: changes.journal_budget must be a number", v.Line)
				break
			}
			c.JournalBudget, err = strconv.ParseFloat(v.Value, 64)
			if err != nil || !(c.JournalBudget > 0) || c.JournalBudget > maxJournalBudget {
				err = fmt.Errorf("line %d: changes.journal_budget must be a number above 0 and at most %g", v.Line, maxJournalBudget)
			}
		default:
			err = fmt.Errorf("line %d: unknown changes key %s", k.Line, k.Value)
		}
		if err != nil {
			return c, err
		}
	}
	return c, nil
}

func positiveInt(v *yaml.Node, key string, max int) (int, error) {
	return intRange(v, key, 1, max)
}

// intRange reads an integer from min to max.
func intRange(v *yaml.Node, key string, min, max int) (int, error) {
	var n int
	if v.Kind != yaml.ScalarNode || v.Tag != "!!int" || v.Decode(&n) != nil || n < min || n > max {
		return 0, fmt.Errorf("line %d: changes.%s must be an integer from %d to %d", v.Line, key, min, max)
	}
	return n, nil
}
