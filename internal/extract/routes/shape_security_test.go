package routes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	securityShapeFixture  = "../../../testdata/fixtures/route-shape-security"
	securityShapeContract = "../../../docs/contract/claims/route-shape-security.md"
)

// TestSecurityShapeContractTable keeps the route security claim table
// (T029 SH-S) and its document 1:1: same rows, titles and forms in the
// same order, each form owned by one row, D last.
func TestSecurityShapeContractTable(t *testing.T) {
	data, err := os.ReadFile(securityShapeContract)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var docs []routes.SecurityShapeRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-S") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := routes.SecurityShapeRow{ID: strings.TrimSpace(cells[1]), Title: strings.TrimSpace(cells[2])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.Forms = append(r.Forms, f)
			}
		}
		docs = append(docs, r)
	}
	if len(docs) != len(routes.SecurityShapeRows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(routes.SecurityShapeRows))
	}
	for i, r := range routes.SecurityShapeRows {
		d := docs[i]
		if d.ID != r.ID || d.Title != r.Title || strings.Join(d.Forms, ",") != strings.Join(r.Forms, ",") {
			t.Errorf("row %d: document %s %q %v, code %s %q %v", i, d.ID, d.Title, d.Forms, r.ID, r.Title, r.Forms)
		}
	}
	if last := routes.SecurityShapeRows[len(routes.SecurityShapeRows)-1]; last.ID != "D" || len(routes.SecurityShapeRows) > 30 {
		t.Errorf("D must be last and the rows within SH-S1..SH-S29")
	}
}

// TestSecurityShapeFixtureRows indexes the route-shape-security fixture
// and checks that it reaches every form of every row, that every SH-S
// tag reached is a form of the table, and that every stored security
// item and scheme names an SH-S row.
func TestSecurityShapeFixtureRows(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(securityShapeFixture)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	tags := map[string]bool{}
	routes.SetShapeTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { routes.SetShapeTag(nil) })
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-23T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var stream bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	forms := map[string]bool{}
	for _, r := range routes.SecurityShapeRows {
		for _, f := range r.Forms {
			forms[r.ID+"/"+f] = true
			if !tags[r.ID+"/"+f] {
				t.Errorf("the fixture does not reach %s/%s", r.ID, f)
			}
		}
	}
	for tag := range tags {
		if strings.HasPrefix(tag, "SH-S") && !forms[tag] {
			t.Errorf("tag %s is not a form of the table", tag)
		}
	}
	rows := map[string]bool{}
	for _, r := range routes.SecurityShapeRows {
		rows[r.ID] = true
	}
	snapshot, err := s.Dump(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		v, ok := n.Attributes[routeshape.AttributeKey]
		if n.Kind != "route" || !ok {
			continue
		}
		shape, err := routeshape.FromAttribute(v)
		if err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		for _, it := range shape.Security.Items {
			if !rows[it.Provenance.Rule] {
				t.Errorf("%s: requirement rule %s is not an SH-S row", n.Identity, it.Provenance.Rule)
			}
		}
		for _, sc := range shape.Schemes {
			if !rows[sc.Provenance.Rule] {
				t.Errorf("%s: scheme rule %s is not an SH-S row", n.Identity, sc.Provenance.Rule)
			}
		}
	}
}
