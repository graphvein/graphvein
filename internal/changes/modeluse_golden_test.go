package changes_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/golden"
)

const modelFixtureDir = "../../testdata/fixtures/change-impact-models"

// TestModelImpactGolden (T029 W2, rows MT-13..MT-18 of
// docs/contract/claims/route-model-types.md) indexes the two revisions of
// the change-impact-models fixture on both backends and checks the
// comparison and the impact of every changed model against
// change-impact.models.golden.json: a model changed in FastAPI (a nested
// model and a repository base), Go (a nested struct) and Express (an
// interface returned by a callee and a zod schema) reaches the routes
// whose shape uses it through relation I16, labelled with the role and
// the using items' provenance kinds; a route that gains a model is a C3
// item labelled models_changed.
func TestModelImpactGolden(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		copyTree(t, filepath.Join(modelFixtureDir, "rev1"), root)
		indexInto(t, ctx, s, root, cache)
		d1 := dump(t, ctx, s, "fixture")
		copyTree(t, filepath.Join(modelFixtureDir, "rev2"), root)
		indexInto(t, ctx, s, root, cache)
		d2 := dump(t, ctx, s, "fixture")
		c := compare(t, ctx, s, "previous", root)
		checkRaw(t, "models", c, d1, d2)
		seeds := []string{
			"class:py/app/models.py#Address",
			"class:py/app/models.py#UserBase",
			"struct:go/types.go#Money",
			"struct:go/types.go#Status",
			"interface:web/src/types.ts#Pet",
			"variable:web/src/schemas.ts#NewPet",
		}
		r := report(t, ctx, s, c, seeds...)
		// Every seed reaches at least one route through I16.
		for _, im := range r.Impacts {
			reached := false
			for _, p := range im.Impact.Paths {
				if p.Kind == "route" && p.Relation == "I16" && strings.HasPrefix(p.Wording, "through ") {
					reached = true
				}
			}
			if !reached {
				t.Errorf("%s reaches no route through I16", im.Seed)
			}
		}
		labelled := false
		for _, it := range c.AllItems() {
			for _, l := range it.Labels {
				labelled = labelled || l == changes.LabelModelsChanged
			}
		}
		if !labelled {
			t.Error("no route item is labelled models_changed")
		}
		golden.Check(t, goldenPath("models"), normalise(t, r))
	})
}
