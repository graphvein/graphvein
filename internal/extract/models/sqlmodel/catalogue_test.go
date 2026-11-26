package sqlmodel

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestCatalogue checks the model catalogue: its rows are modelClaimRows'
// rows with the same claim function and the docs table's claim cell, and
// each assumption in force has the bold title and first sentence of its
// entry in docs/lang/python-models.md (a folded one a sentence of it).
func TestCatalogue(t *testing.T) {
	docs := tableRows(t, "../../../../docs/extract/schema-models.md")
	seen := map[string]bool{}
	for _, e := range Contract() {
		seen[e.ID] = true
		fn, ok := modelClaimRows[e.ID]
		if !ok {
			t.Errorf("catalogue row %s is not in modelClaimRows", e.ID)
		}
		want := fn
		if fn == "" {
			want = "none"
		}
		if e.Branch != want {
			t.Errorf("%s: branch %q, modelClaimRows %q", e.ID, e.Branch, want)
		}
		cells := strings.Split(docs[e.ID], "|")
		if len(cells) < 4 || strings.TrimSpace(cells[3]) != e.Summary {
			t.Errorf("%s: summary %q, docs %q", e.ID, e.Summary, docs[e.ID])
		}
	}
	for id := range modelClaimRows {
		if !seen[id] {
			t.Errorf("row %s is not in the catalogue", id)
		}
	}
	data, err := os.ReadFile("../../../../docs/lang/python-models.md")
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	cur := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := regexp.MustCompile(`^- \*\*(V\d) `).FindStringSubmatch(line); m != nil {
			cur = m[1]
			entries[cur] = line
		} else if cur != "" && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			entries[cur] += " " + strings.TrimSpace(line)
		} else {
			cur = ""
		}
	}
	plain := strings.NewReplacer("**", "", "`", "")
	entry := regexp.MustCompile(`^- \*\*V\d (?:\(Rev \d\) )?([^*]+?)\.\*\* (.*)$`)
	for _, a := range Assumptions() {
		m := entry.FindStringSubmatch(entries[a.ID])
		if m == nil {
			t.Errorf("%s: no entry in docs/lang/python-models.md", a.ID)
			continue
		}
		if m[1] != a.Title {
			t.Errorf("%s: title %q, docs %q", a.ID, a.Title, m[1])
		}
		text := plain.Replace(m[2])
		if a.FoldedInto != "" {
			if !strings.Contains(text, a.Text) {
				t.Errorf("%s: folded text %q is not in its entry", a.ID, a.Text)
			}
			continue
		}
		first := text
		if i := strings.Index(text, ". "); i >= 0 {
			first = text[:i+1]
		}
		if first != a.Text {
			t.Errorf("%s: text %q, docs %q", a.ID, a.Text, first)
		}
	}
}
