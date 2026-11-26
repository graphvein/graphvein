package openapi

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

const contractDoc = "../../docs/contract/claims/openapi-generation.md"

type docRow struct {
	id    string
	forms []string
}

// docRows reads the "## Claim contract" table of the contract document.
func docRows(t *testing.T) []docRow {
	t.Helper()
	data, err := os.ReadFile(contractDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	end := strings.Index(text, "\n## Path normalisation")
	if start < 0 || end < start {
		t.Fatal("no Claim contract section")
	}
	var rows []docRow
	for _, line := range strings.Split(text[start:end], "\n") {
		if !strings.HasPrefix(line, "| OG-") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := docRow{id: strings.TrimSpace(cells[1])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.forms = append(r.forms, f)
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// TestContractTable keeps Rows and the document 1:1 (ids, order, forms)
// and checks that every value the generator writes in a listed group is
// the form of exactly one row.
func TestContractTable(t *testing.T) {
	docs := docRows(t)
	if len(docs) != len(Rows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(Rows))
	}
	for i, r := range Rows {
		if docs[i].id != r.ID || !reflect.DeepEqual(docs[i].forms, r.Forms) {
			t.Errorf("row %d: document %s %v, code %s %v", i, docs[i].id, docs[i].forms, r.ID, r.Forms)
		}
	}
	if Rows[len(Rows)-1].ID != "D" {
		t.Error("rows must end with D")
	}
	owner := map[string]string{}
	ids := map[string]bool{}
	for _, r := range Rows {
		ids[r.ID] = true
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	var want []string
	add := func(group string, values ...string) {
		for _, v := range values {
			want = append(want, group+":"+v)
		}
	}
	add("openapi", Version)
	add("info.version", "unversioned")
	add("refused", ReasonResolution, ReasonInvalidTemplate, ReasonDuplicateParam, ReasonWildcard, ReasonOptional, ReasonHost, ReasonMethod)
	add("conflict", ConflictEquivalent, ConflictDuplicate, ConflictScheme)
	add("method", append([]string{"ANY"}, methodOrder...)...)
	add("convertor", "path")
	for f := range firstMatch {
		add("first_match", f)
	}
	for class := range httpSchemes {
		add("class", class)
	}
	add("not_read", notReadFields...)
	add("auth", authKeys...)
	add("resolution", "literal", "constant", "settings_default", "unresolved", SetByYou)
	add("unresolved", UnresolvedNoPath, UnresolvedBase)
	add("unknown", PartBasePath, PartMount)
	add("format", FormatJSON, FormatYAML)
	add("state", string(routeshape.NotRead), string(routeshape.ProvablyNone), string(routeshape.Partial), string(routeshape.Complete))
	add("kind", string(routeshape.Fact), string(routeshape.ReadInHandler), string(routeshape.Intended), string(routeshape.Returned), string(routeshape.Mapped))
	add("access", string(routeshape.Declared), string(routeshape.ReadByHandler), string(routeshape.ReadByDependency))
	add("presence", string(routeshape.Required), string(routeshape.Optional), string(routeshape.PresenceUnknown))
	add("section", routeshape.SectionNames...)
	for h := range reservedHeaders {
		add("header", h)
	}
	add("type", string(routeshape.TypeObject), string(routeshape.TypeArray), string(routeshape.TypeString), string(routeshape.TypeNumber), string(routeshape.TypeInteger),
		string(routeshape.TypeBoolean), string(routeshape.TypeNull), string(routeshape.TypeEnum), string(routeshape.TypeRef), string(routeshape.TypeUnknown))
	add("additional", string(routeshape.AdditionalAllowed), string(routeshape.AdditionalForbidden), string(routeshape.AdditionalUnknown))
	add("nullable", "true")
	add("scheme", string(routeshape.SchemeHTTP), string(routeshape.SchemeAPIKey), string(routeshape.SchemeOpenIDConnect), string(routeshape.SchemeOAuth2), string(routeshape.SchemeUnknown))
	add("stop", stopNames()...)
	add("key", config.PathValueRoutePrefix, config.PathValueNextBasePrefix, config.PathValueMountPrefix)
	add("shadowing", "not_checked")
	add("user_value", UserValueUsed, UserValueUnused)
	for _, f := range want {
		if owner[f] == "" {
			t.Errorf("form %s has no row", f)
		}
		delete(owner, f)
	}
	for f, row := range owner {
		t.Errorf("row %s lists %s, which the generator does not have", row, f)
	}
	// Every row the code names (not_standard items, comments) exists.
	for _, file := range []string{"generate.go", "shape.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`"(OG-\d+)"`).FindAllStringSubmatch(string(data), -1) {
			if !ids[m[1]] {
				t.Errorf("%s names row %s, which the table does not have", file, m[1])
			}
		}
	}
}
