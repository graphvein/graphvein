package index

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCountsUnresolvedCalls keeps CountsUnresolvedCalls in step with the
// readers that report unresolved calls: every language package that calls
// facts.Unresolved, and no other (ADR 0089, row CR-24).
func TestCountsUnresolvedCalls(t *testing.T) {
	languages := map[string][]string{
		"typescript": {"typescript", "tsx", "javascript"},
		"golang":     {"go"},
		"python":     {"python"},
		"rust":       {"rust"},
		"java":       {"java"},
		"csharp":     {"c_sharp"},
		"dart":       {"dart"},
		"template":   {"html"},
		"besteffort": nil,
		"facts":      nil,
	}
	dirs, err := os.ReadDir("../lang")
	if err != nil {
		t.Fatal(err)
	}
	var counting []string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		names, ok := languages[d.Name()]
		if !ok {
			t.Fatalf("internal/lang/%s is not mapped to languages in this test", d.Name())
		}
		files, err := filepath.Glob(filepath.Join("../lang", d.Name(), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		calls := false
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "facts.Unresolved(") {
				calls = true
			}
		}
		if calls {
			counting = append(counting, names...)
		}
	}
	sort.Strings(counting)
	var got []string
	for _, l := range []string{"typescript", "tsx", "javascript", "go", "python", "rust", "java", "c_sharp", "dart", "html", "css", "json", "unknown"} {
		if CountsUnresolvedCalls(l) {
			got = append(got, l)
		}
		if CountsUnresolvedCalls(l) && !Reads(l) {
			t.Errorf("%s counts unresolved calls but has no reader", l)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(counting, ",") {
		t.Fatalf("CountsUnresolvedCalls = %v, readers calling facts.Unresolved = %v", got, counting)
	}
}
