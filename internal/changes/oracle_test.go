package changes_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestChangeClassificationOracle is step 5 of the T019 oracle: on the
// stores TestChangeJournalOracle (internal/indexrun) left in
// GRAPHVEIN_CHANGE_ORACLE_SCRATCH, it classifies the journal, pin and ref
// comparisons of every pair and checks the invariants (Violations,
// ImpactViolations on the traversal of every seed). Run it after that
// test, with the same environment:
//
//	GRAPHVEIN_CHANGE_ORACLE=1 GRAPHVEIN_CHANGE_ORACLE_PAIRS=<file> GRAPHVEIN_CHANGE_ORACLE_SCRATCH=<dir> \
//	  go test ./internal/indexrun -run TestChangeJournalOracle
//	... go test ./internal/changes -run TestChangeClassificationOracle
func TestChangeClassificationOracle(t *testing.T) {
	if os.Getenv("GRAPHVEIN_CHANGE_ORACLE") != "1" {
		t.Skip("set GRAPHVEIN_CHANGE_ORACLE=1, GRAPHVEIN_CHANGE_ORACLE_PAIRS and GRAPHVEIN_CHANGE_ORACLE_SCRATCH to run step 5")
	}
	pairs, err := os.Open(os.Getenv("GRAPHVEIN_CHANGE_ORACLE_PAIRS"))
	if err != nil {
		t.Fatal(err)
	}
	defer pairs.Close()
	scratch := os.Getenv("GRAPHVEIN_CHANGE_ORACLE_SCRATCH")
	type report struct {
		ID         string                    `json:"id"`
		Base       string                    `json:"base"`
		Rows       map[string]int            `json:"items_per_row"`
		Changes    map[string]map[string]int `json:"changes_per_row"`
		Reasons    map[string]int            `json:"link_reasons"`
		Seeds      int                       `json:"seeds"`
		Paths      int                       `json:"paths"`
		Relations  map[string]int            `json:"steps_per_relation"`
		Violations []string                  `json:"violations"`
	}
	var reports []report
	lines := bufio.NewScanner(pairs)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("pairs line %q", line)
		}
		id, clone, a, b := f[0], f[1], f[2], f[3]
		t.Run(id, func(t *testing.T) {
			ctx := context.Background()
			s, err := sqlite.OpenRead(ctx, filepath.Join(scratch, id, "s.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, raw := range []string{"previous", "pin:A", "ref:" + a + ".." + b} {
				base, _ := changes.ParseBase(raw)
				c, err := changes.Compare(ctx, s, id, base, changes.Options{Root: clone, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
				if err != nil {
					t.Fatal(raw, err)
				}
				r := report{ID: id, Base: raw, Rows: map[string]int{}, Changes: map[string]map[string]int{}, Reasons: map[string]int{}, Relations: map[string]int{}, Violations: c.Violations()}
				for _, it := range c.AllItems() {
					r.Rows[it.Row]++
					if r.Changes[it.Row] == nil {
						r.Changes[it.Row] = map[string]int{}
					}
					r.Changes[it.Row][it.Change]++
					if it.Reason != nil && strings.HasPrefix(it.Change, "link_") {
						r.Reasons[it.Change+":"+*it.Reason]++
					}
					imp, err := changes.Impact(ctx, s, c, changes.ImpactRequest{ID: it.ID()})
					if err == changes.ErrNotSeed {
						continue
					}
					if err != nil {
						t.Fatal(it.ID(), err)
					}
					r.Seeds++
					r.Paths += len(imp.Paths)
					for _, p := range imp.Paths {
						r.Relations[p.Relation]++
					}
					r.Violations = append(r.Violations, changes.ImpactViolations(imp)...)
				}
				sort.Strings(r.Violations)
				if len(r.Violations) > 0 {
					t.Errorf("%s %s: %d classification violations, first %s", id, raw, len(r.Violations), r.Violations[0])
				}
				reports = append(reports, r)
			}
		})
	}
	data, _ := json.MarshalIndent(reports, "", "  ")
	t.Logf("step 5:\n%s", data)
	if path := os.Getenv("GRAPHVEIN_CHANGE_ORACLE_REPORT"); path != "" {
		if err := os.WriteFile(path+".step5.json", append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
