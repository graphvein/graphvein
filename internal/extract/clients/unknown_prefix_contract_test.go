package clients

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	upClaimsPath     = "../../../docs/contract/claims/unknown-prefix-paths.md"
	upDocsPath       = "../../../docs/extract/clients.md"
	upMatcherTable   = "\n## Client matcher\n"
	upDefaultTable   = "\n## Default\n"
	upDocsTable      = "\n### Unknown-prefix tier claim contract (client matcher)\n"
	upAssumptionsTbl = "\n## Assumptions\n"
)

// upTable reads the rows of the first table after heading in path: the
// row ids in order and each row's full line.
func upTable(t *testing.T, path, heading string) ([]string, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, heading)
	if i < 0 {
		t.Fatalf("%s: no %q", path, strings.TrimSpace(heading))
	}
	var ids []string
	rows := map[string]string{}
	started := false
	for _, line := range strings.Split(text[i+len(heading):], "\n") {
		if !strings.HasPrefix(line, "|") {
			if started {
				break
			}
			continue
		}
		started = true
		if strings.HasPrefix(line, "| id |") || strings.HasPrefix(line, "|---") {
			continue
		}
		id := strings.TrimSpace(strings.Split(line, "|")[1])
		if _, dup := rows[id]; dup {
			t.Fatalf("%s: duplicate row %s", path, id)
		}
		ids = append(ids, id)
		rows[id] = line
	}
	return ids, rows
}

// TestUnknownPrefixMatcherContract keeps the matcher rows of the public
// unknown-prefix contract (and its default row D), their copy in
// docs/extract/clients.md and the code table UnknownPrefixMatcherRows 1:1
// (same rows, same order, same text), checks that every row has a tagged
// branch in the tier, every static tag names a row, and that the shown
// assumption texts are the public ones. TestUnknownPrefixLinks
// (internal/index) checks that the fixture reaches every row.
func TestUnknownPrefixMatcherContract(t *testing.T) {
	ids, public := upTable(t, upClaimsPath, upMatcherTable)
	defaults, defaultRows := upTable(t, upClaimsPath, upDefaultTable)
	if !reflect.DeepEqual(defaults, []string{"D"}) {
		t.Fatalf("default table rows %v, want [D]", defaults)
	}
	ids = append(ids, "D")
	public["D"] = defaultRows["D"]
	docsIDs, docs := upTable(t, upDocsPath, upDocsTable)
	if !reflect.DeepEqual(docsIDs, ids) {
		t.Errorf("docs rows %v, public rows %v", docsIDs, ids)
	}
	for _, id := range ids {
		if docs[id] != public[id] {
			t.Errorf("row %s differs:\npublic: %s\ndocs:   %s", id, public[id], docs[id])
		}
	}
	if !reflect.DeepEqual(UnknownPrefixMatcherRows, ids) {
		t.Errorf("code rows %v, public rows %v", UnknownPrefixMatcherRows, ids)
	}
	data, err := os.ReadFile("unknown_prefix.go")
	if err != nil {
		t.Fatal(err)
	}
	// upCandidates reports through its tag parameter (upTag, or a no-op
	// when row UP-22 probes the tier).
	tagged := map[string]bool{}
	for _, m := range regexp.MustCompile(`\b(?:upTag|tag)\("([^"]+)"\)`).FindAllStringSubmatch(string(data), -1) {
		tagged[m[1]] = true
		if !slices.Contains(UnknownPrefixMatcherRows, m[1]) {
			t.Errorf("tag %s is not a matcher row", m[0])
		}
	}
	for _, id := range ids {
		if !tagged[id] {
			t.Errorf("row %s has no tagged branch", id)
		}
	}
	_, assumptions := upTable(t, upClaimsPath, upAssumptionsTbl)
	for id, text := range UnknownPrefixAssumptions {
		if !strings.Contains(assumptions[id], "| "+text+" |") {
			t.Errorf("assumption %s text %q differs from the public row %q", id, text, assumptions[id])
		}
	}
	if len(assumptions) != len(UnknownPrefixAssumptions) {
		t.Errorf("public assumptions %d, code %d", len(assumptions), len(UnknownPrefixAssumptions))
	}
}

const (
	upCallTable     = "\n## Call side\n"
	upCallDocsTable = "\n### Unknown-prefix call side claim contract\n"
)

// TestUnknownPrefixCallContract keeps the call-side rows of the public
// unknown-prefix contract (and its default row D), their copy in
// docs/extract/clients.md and the code table UnknownPrefixCallRows 1:1
// (same rows, same order, same text), and checks that every row has a
// tagged branch in runtime_base.go or evaluate.go and every static tag
// names a row.
// TestUnknownPrefixHelpers (internal/index) checks that the fixture
// reaches every row.
func TestUnknownPrefixCallContract(t *testing.T) {
	ids, public := upTable(t, upClaimsPath, upCallTable)
	_, defaultRows := upTable(t, upClaimsPath, upDefaultTable)
	ids = append(ids, "D")
	public["D"] = defaultRows["D"]
	docsIDs, docs := upTable(t, upDocsPath, upCallDocsTable)
	if !reflect.DeepEqual(docsIDs, ids) {
		t.Errorf("docs rows %v, public rows %v", docsIDs, ids)
	}
	for _, id := range ids {
		if docs[id] != public[id] {
			t.Errorf("row %s differs:\npublic: %s\ndocs:   %s", id, public[id], docs[id])
		}
	}
	if !reflect.DeepEqual(UnknownPrefixCallRows, ids) {
		t.Errorf("code rows %v, public rows %v", UnknownPrefixCallRows, ids)
	}
	var source []byte
	for _, file := range []string{"runtime_base.go", "evaluate.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source = append(source, data...)
	}
	tagged := map[string]bool{}
	for _, m := range regexp.MustCompile(`upCallTag\("([^"]+)"\)`).FindAllStringSubmatch(string(source), -1) {
		tagged[m[1]] = true
		if !slices.Contains(UnknownPrefixCallRows, m[1]) {
			t.Errorf("tag %s is not a call-side row", m[0])
		}
	}
	for _, id := range ids {
		if !tagged[id] {
			t.Errorf("row %s has no tagged branch", id)
		}
	}
}
