package schema

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// docEntries reads the bullet entries of a Markdown file whose first line
// matches start (one capture: the id), joining continuation lines.
func docEntries(t *testing.T, path string, start *regexp.Regexp) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	cur := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := start.FindStringSubmatch(line); m != nil {
			cur = m[1]
			out[cur] = line
			continue
		}
		if cur != "" && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			out[cur] += " " + strings.TrimSpace(line)
			continue
		}
		cur = ""
	}
	return out
}

// plainText drops Markdown emphasis and code marks.
func plainText(s string) string {
	return strings.NewReplacer("**", "", "`", "").Replace(s)
}

// firstSentence is s up to its first ". ".
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// TestCatalogueTexts checks the catalogue against docs/extract/schema.md:
// each assumption in force has the entry's bold title and first sentence,
// E7 the sentence that folds it into E8, and every id of
// contractAssumptions is a catalogue entry.
func TestCatalogueTexts(t *testing.T) {
	entries := docEntries(t, "../../../docs/extract/schema.md", regexp.MustCompile(`^- \*\*(E\d) `))
	entry := regexp.MustCompile(`^- \*\*E\d ([^*]+)\*\*(?: \([^)]*\))?: (.*)$`)
	seen := map[string]bool{}
	for _, a := range Assumptions() {
		seen[a.ID] = true
		if a.FoldedInto != "" {
			if !strings.Contains(plainText(entries[a.FoldedInto]), "("+a.Text[:len(a.Text)-1]+".)") {
				t.Errorf("%s: folded text %q is not in the docs", a.ID, a.Text)
			}
			continue
		}
		m := entry.FindStringSubmatch(entries[a.ID])
		if m == nil {
			t.Errorf("%s: no entry in docs/extract/schema.md", a.ID)
			continue
		}
		if !strings.EqualFold(m[1], a.Title) {
			t.Errorf("%s: title %q, docs %q", a.ID, a.Title, m[1])
		}
		if want := firstSentence(plainText(m[2])); want != a.Text {
			t.Errorf("%s: text %q, docs %q", a.ID, a.Text, want)
		}
	}
	for id := range entries {
		if !seen[id] {
			t.Errorf("%s is in the docs but not in the catalogue", id)
		}
	}
	listed := map[string]bool{}
	for _, e := range Contract() {
		listed[e.ID] = true
		for _, a := range e.Assumptions {
			if !seen[a] {
				t.Errorf("%s rests on %s, which the catalogue lacks", e.ID, a)
			}
		}
	}
	for id := range contractAssumptions {
		if !listed[id] {
			t.Errorf("contractAssumptions has %s, which is no row or precondition", id)
		}
	}
}
