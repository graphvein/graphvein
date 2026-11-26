package typescript

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// argTexts returns the source texts of a call's reduced arguments.
func argTexts(e facts.RouteExpr) []string {
	out := []string{}
	for _, a := range e.Args {
		out = append(out, a.Text)
	}
	return out
}

// TestClientCallChains checks that every call of a chain keeps its own
// text, arguments and URL scope. The calls of a chain all start at the
// same byte; the cache used to key the rebuilt expressions by start byte
// only, so each one got the innermost call's arguments (T018h Revision 4,
// ADR 0060 payload correction).
func TestClientCallChains(t *testing.T) {
	f := extractInline(t, `async function chains(db, axios, prisma, u) {
  await db.select().from(users).leftJoin(posts, on);
  axios.get(u).then((r) => r.data);
  await prisma.user.findUnique({ where: { id: 1 } }).posts({ take: 2 });
}
`)
	cases := []struct {
		text string
		args []string
	}{
		{"db.select().from(users).leftJoin(posts, on)", []string{"posts", "on"}},
		{"db.select().from(users)", []string{"users"}},
		{"db.select()", []string{}},
		{"axios.get(u).then((r) => r.data)", []string{"(r) => r.data"}},
		{"axios.get(u)", []string{"u"}},
		{"prisma.user.findUnique({ where: { id: 1 } }).posts({ take: 2 })", []string{"{ take: 2 }"}},
		{"prisma.user.findUnique({ where: { id: 1 } })", []string{"{ where: { id: 1 } }"}},
	}
	byText := map[string]facts.ClientCall{}
	for _, c := range f.Routes.Clients {
		byText[c.Expression.Text] = c
	}
	for _, tc := range cases {
		c, ok := byText[tc.text]
		if !ok {
			t.Errorf("no cached call with text %q", tc.text)
			continue
		}
		if got := argTexts(c.Expression); !reflect.DeepEqual(got, tc.args) {
			t.Errorf("%s: arguments %q, want %q", tc.text, got, tc.args)
		}
		if c.URLScope == 0 {
			t.Errorf("%s: URL scope 0, want the function's scope", tc.text)
		}
	}
	// The callee chain still holds the inner calls with their own arguments.
	outer := byText["db.select().from(users).leftJoin(posts, on)"].Expression
	if from := outer.Object.Object; from.Kind != "call_expression" || !reflect.DeepEqual(argTexts(*from), []string{"users"}) {
		t.Errorf("inner from call: %+v", from)
	}
}
