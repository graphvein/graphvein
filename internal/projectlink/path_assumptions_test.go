package projectlink_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/projectlink"
)

// TestProjectLinkPathAssumptions checks that a project link to a route
// path entry resting on a path assumption (RO-V2 here; GR-V5 and RO-V1
// alike) stores the id in Assumes and ends its assumption with
// PathAssumptionNote, with the confidence of the
// row unchanged; a plain entry adds nothing.
func TestProjectLinkPathAssumptions(t *testing.T) {
	assumed := routeNode("route:server.ts#GET /z/b", "GET", "/z/b", "")
	assumed.Attributes["paths"].([]any)[0].(map[string]any)["assumptions"] = []any{"RO-V2"}
	plain := routeNode("route:server.ts#GET /plain", "GET", "/plain", "")
	for _, c := range []struct {
		template string
		assumes  []string
	}{
		{"/z/b", []string{"RO-V2"}},
		{"/plain", nil},
	} {
		t.Run(c.template, func(t *testing.T) {
			result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: []projectlink.Member{
				member("front", callNode("call:a", "GET", c.template, nil)),
				member("api", assumed, plain),
			}})
			if len(result.Set.Links) != 1 {
				t.Fatalf("links %+v, want one", result.Set.Links)
			}
			l := result.Set.Links[0]
			if !reflect.DeepEqual(l.Assumes, c.assumes) {
				t.Fatalf("assumes %v, want %v", l.Assumes, c.assumes)
			}
			if note := projectlink.PathAssumptionNote(c.assumes); (c.assumes != nil) != strings.HasSuffix(l.Assumption, "; "+note) {
				t.Fatalf("assumption %q", l.Assumption)
			}
			if !strings.HasPrefix(l.Assumption, projectlink.DefaultAssumption) {
				t.Fatalf("assumption %q", l.Assumption)
			}
		})
	}
}
