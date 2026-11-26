package projectlink_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestDeclarationConflicts checks the "conflicts with declaration" flag
// (T022 B9) on the monorepo scenario: web/client.ts#userFortyTwoFromOrders is
// linked inside the repository to the users server's GET /t022/users/:id,
// while the web scope's entry covering its VITE_ORDERS base names
// monorepo/orders. It is the only conflict; the scenario without
// declarations has none, and targets is never asked for a member without
// scopes.
func TestDeclarationConflicts(t *testing.T) {
	ctx := context.Background()
	root := copyFixture(t)
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, m := range fixtureMembers {
		indexMember(t, s, filepath.Join(root, m), m)
	}
	targets := func(asked *[]string) func(string, []string) (map[string][]string, error) {
		return func(repository string, calls []string) (map[string][]string, error) {
			*asked = append(*asked, repository)
			edges, err := s.Edges(ctx, repository, calls, []string{"calls_route"}, store.Outgoing)
			if err != nil {
				return nil, err
			}
			out := map[string][]string{}
			for _, e := range edges {
				out[e.Source] = append(out[e.Source], e.Target)
			}
			return out, nil
		}
	}
	for _, tc := range []struct {
		scenario string
		want     int
	}{{"monorepo", 1}, {"monorepo-default", 0}, {"two", 0}} {
		t.Run(tc.scenario, func(t *testing.T) {
			project := loadScenario(t, root, tc.scenario)
			in, err := projectlink.Read(ctx, s, project, "sha256:"+tc.scenario)
			if err != nil {
				t.Fatal(err)
			}
			var asked []string
			got, err := projectlink.DeclarationConflicts(in, targets(&asked))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("conflicts: %+v", got)
			}
			for _, repo := range asked {
				if repo != "monorepo" {
					t.Errorf("targets asked for %s, which declares no scopes", repo)
				}
			}
			if tc.want == 0 {
				return
			}
			c := got[0]
			if !strings.HasPrefix(c.Call.Identity, "client_call:web/client.ts#userFortyTwoFromOrders.") || c.Call.Repository != "monorepo" {
				t.Errorf("call: %+v", c.Call)
			}
			if c.Consumer != "monorepo/web" || c.TargetScope != "users" || len(c.Declared) != 1 || c.Declared[0] != "`monorepo/web` consumes `monorepo/orders` (bases: VITE_ORDERS)" {
				t.Errorf("conflict: %+v", c)
			}
		})
	}
}
