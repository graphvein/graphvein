package routeshape

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
)

const contractDoc = "../../../docs/contract/claims/route-shape.md"

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
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var rows []docRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SM-") && !strings.HasPrefix(line, "| D |") {
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

// TestContractTable keeps routeshape.Rows and the document 1:1 (ids,
// order, forms) and checks that every constant of the model is the form
// of exactly one row.
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
	if Rows[len(Rows)-1].ID != "D" || Rows[len(Rows)-2].ID != "SM-29" {
		t.Error("rows must end with SM-29 and D")
	}
	owner := map[string]string{}
	for _, r := range Rows {
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	var forms []string
	add := func(group string, values ...string) {
		for _, v := range values {
			forms = append(forms, group+":"+v)
		}
	}
	add("attribute", AttributeKey)
	add("version", "1")
	add("state", string(NotRead), string(ProvablyNone), string(Partial), string(Complete))
	add("kind", string(Fact), string(ReadInHandler), string(Intended), string(Returned), string(Mapped))
	add("access", string(Declared), string(ReadByHandler), string(ReadByDependency))
	add("presence", string(Required), string(Optional), string(PresenceUnknown))
	add("section", SectionNames...)
	add("scheme", string(SchemeHTTP), string(SchemeAPIKey), string(SchemeOAuth2), string(SchemeOpenIDConnect), string(SchemeUnknown))
	add("type", string(TypeObject), string(TypeArray), string(TypeString), string(TypeNumber), string(TypeInteger), string(TypeBoolean), string(TypeNull), string(TypeEnum), string(TypeRef), string(TypeUnknown))
	add("additional", string(AdditionalAllowed), string(AdditionalForbidden), string(AdditionalUnknown))
	add("config", "index.shape_depth")
	add("flag", "--shape-depth")
	for _, s := range Stops {
		add("stop", string(s))
		if stopRows[s] != owner["stop:"+string(s)] {
			t.Errorf("stop %s: validator row %s, table row %s", s, stopRows[s], owner["stop:"+string(s)])
		}
	}
	if len(stopRows) != len(Stops) {
		t.Errorf("%d stop rows for %d stops", len(stopRows), len(Stops))
	}
	for _, f := range forms {
		if owner[f] == "" {
			t.Errorf("form %s has no row", f)
		}
		delete(owner, f)
	}
	for f, row := range owner {
		t.Errorf("row %s lists %s, which the model does not have", row, f)
	}
}

// TestDepthDefaultsAgree checks the config package's copies of the depth
// bounds (config does not import this package).
func TestDepthDefaultsAgree(t *testing.T) {
	if config.DefaultShapeDepth != DefaultDepth || config.MinShapeDepth != MinDepth || config.MaxShapeDepth != MaxDepthLimit {
		t.Errorf("config %d %d..%d, routeshape %d %d..%d", config.DefaultShapeDepth, config.MinShapeDepth, config.MaxShapeDepth, DefaultDepth, MinDepth, MaxDepthLimit)
	}
	if config.Defaults().Index.ShapeDepth != DefaultDepth {
		t.Error("config.Defaults has another shape depth")
	}
}
