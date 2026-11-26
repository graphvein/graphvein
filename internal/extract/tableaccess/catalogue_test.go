package tableaccess

import (
	"os"
	"strings"
	"testing"
)

// TestAssumptionTexts checks the served assumption texts against the
// document's "Assumptions" section, one entry per id of Assumptions, in
// its order.
func TestAssumptionTexts(t *testing.T) {
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Assumptions\n")
	end := strings.Index(text, "\n## Claim contract\n")
	if start < 0 || end < start {
		t.Fatal("the document has no Assumptions section before its claim contract")
	}
	section := strings.Join(strings.Fields(text[start:end]), " ")
	if len(AssumptionTexts) != len(Assumptions) {
		t.Fatalf("%d assumption texts, %d assumptions", len(AssumptionTexts), len(Assumptions))
	}
	for i, a := range AssumptionTexts {
		if a.ID != Assumptions[i] {
			t.Errorf("text %d is %s, want %s", i, a.ID, Assumptions[i])
		}
		if a.Title == "" || !strings.Contains(section, "- **"+a.ID+"** "+a.Text) {
			t.Errorf("%s: %q is not the document's entry", a.ID, a.Text)
		}
	}
	if got := strings.Count(section, "- **TA-V"); got != len(Assumptions) {
		t.Errorf("the document lists %d TA-V entries, the package %d", got, len(Assumptions))
	}
}
