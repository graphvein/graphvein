package query

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestMatchOf covers the target rule of T030 review: an identity, a
// name and a suffix at a name boundary resolve; a suffix inside a name is
// a candidate only.
func TestMatchOf(t *testing.T) {
	list := contract.Node{Identity: "function:server/app.ts#listItems", Name: "listItems", QualifiedName: "listItems"}
	save := contract.Node{Identity: "method:m.py#User.save", Name: "save", QualifiedName: "User.save"}
	route := contract.Node{Identity: "route:app.ts#GET%20%2Ft016d-items", Name: "GET /t016d-items", QualifiedName: "GET /t016d-items"}
	rust := contract.Node{Identity: "function:src/lib.rs#db::open", Name: "open", QualifiedName: "db::open"}
	for _, tc := range []struct {
		n    contract.Node
		term string
		want string
	}{
		{list, "function:server/app.ts#listItems", MatchIdentity},
		{list, "listItems", MatchName},
		{list, "tItems", MatchPartial},
		{list, "Items", MatchPartial},
		{list, "s", MatchPartial},
		{list, "nothing", ""},
		{save, "save", MatchName},
		{save, "User.save", MatchName},
		{save, ".save", MatchQualifiedSuffix},
		{save, "ser.save", MatchPartial},
		{route, "/t016d-items", MatchQualifiedSuffix},
		{route, "items", MatchQualifiedSuffix},
		{route, "tems", MatchPartial},
		{rust, "db::open", MatchName},
		{rust, "pen", MatchPartial},
		{list, "", ""},
	} {
		if got := MatchOf(tc.n, tc.term); got != tc.want {
			t.Errorf("MatchOf(%s, %q) = %q, want %q", tc.n.Identity, tc.term, got, tc.want)
		}
	}
	resolving, partial := Resolve("save", []contract.Node{save, list})
	if len(resolving) != 1 || resolving[0].Identity != save.Identity || len(partial) != 0 {
		t.Errorf("Resolve(save) = %v, %v", resolving, partial)
	}
	resolving, partial = Resolve("Items", []contract.Node{list})
	if len(resolving) != 0 || len(partial) != 1 {
		t.Errorf("Resolve(Items) = %v, %v", resolving, partial)
	}
}
