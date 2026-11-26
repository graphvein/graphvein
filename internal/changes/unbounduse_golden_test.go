package changes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

const prismaFixtureDir = "../../testdata/fixtures/table-access-prisma"

// TestUnboundMayAlsoUseGolden (T030 review, the R9 rule) indexes two
// revisions of the table-access-prisma fixture on both backends: the
// fixture as committed, then the same tree with Tag given a new column.
// The impact of the changed table and column lists, under "may also be
// used by", the entries naming Tag and the unbound entries that may reach
// it (unbound: true): the fixture's prisma sites with null candidates and
// the raw SQL (tool sql) sites of src/raw.ts, which the earlier same-tool
// rule of the MCP table tool missed. The golden is
// change-impact.prisma-unbound.golden.json.
func TestUnboundMayAlsoUseGolden(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		copyTree(t, prismaFixtureDir, root)
		indexInto(t, ctx, s, root, cache)
		d1 := dump(t, ctx, s, "fixture")
		schema := filepath.Join(root, "prisma", "schema.prisma")
		src, err := os.ReadFile(schema)
		if err != nil {
			t.Fatal(err)
		}
		edited := bytes.Replace(src, []byte("  name  String @unique\n  posts Post[]\n"), []byte("  name  String @unique\n  label String?\n  posts Post[]\n"), 1)
		if bytes.Equal(edited, src) {
			t.Fatal("the Tag model was not found in schema.prisma")
		}
		if err := os.WriteFile(schema, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		indexInto(t, ctx, s, root, cache)
		d2 := dump(t, ctx, s, "fixture")
		c := compare(t, ctx, s, "previous", root)
		checkRaw(t, "prisma-unbound", c, d1, d2)
		r := report(t, ctx, s, c, "table:prisma/schema.prisma#Tag", "column:prisma/schema.prisma#Tag.label")
		var unbound, sql bool
		for _, im := range r.Impacts {
			for _, u := range im.Impact.TableUses {
				for _, e := range u.MayAlsoBeUsedBy {
					unbound = unbound || e.Unbound
					sql = sql || e.Unbound && e.Tool == "sql"
				}
			}
		}
		if !unbound || !sql {
			t.Errorf("no unbound may-also-use entry (any %v, sql %v)", unbound, sql)
		}
		golden.Check(t, goldenPath("prisma-unbound"), normalise(t, r))
	})
}
