package typescript

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// extractInline parses one inline TypeScript source and extracts its facts.
func extractInline(t *testing.T, source string) Facts {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	return Extract("a.ts", []byte(source), results[0].Root)
}

// clientCall returns the cached client call whose source text starts with prefix.
func clientCall(t *testing.T, f Facts, prefix string) facts.ClientCall {
	t.Helper()
	for _, c := range f.Routes.Clients {
		if strings.HasPrefix(c.Expression.Text, prefix) {
			return c
		}
	}
	t.Fatalf("no client call starting with %q", prefix)
	return facts.ClientCall{}
}

// substitutions returns the template_substitution arguments of a tagged call.
func substitutions(e facts.RouteExpr) []facts.RouteExpr {
	var out []facts.RouteExpr
	for _, a := range e.Args {
		if a.Kind == "template_substitution" {
			out = append(out, a)
		}
	}
	return out
}

// TestClientCallObjectSpread checks that an object argument with a spread
// element is marked, at any depth, and that objects without one are not
// (T018h H-B2, ADR 0060).
func TestClientCallObjectSpread(t *testing.T) {
	f := extractInline(t, `const criteria = { where: { id: 1 } };
async function list(prisma) {
  await prisma.user.findMany({ ...criteria, include: { posts: true } });
  await prisma.user.findMany({ where: { ...criteria.where }, include: { posts: true } });
  await prisma.user.findMany({ include: { posts: true } });
}
`)
	spread := clientCall(t, f, "prisma.user.findMany({ ...criteria").Expression.Args[0]
	if spread.Kind != "object" || !spread.Spread {
		t.Fatalf("top-level spread not marked: %+v", spread)
	}
	if spread.Properties["include"].Spread {
		t.Fatal("object without a spread marked")
	}
	nested := clientCall(t, f, "prisma.user.findMany({ where").Expression.Args[0]
	if nested.Spread || !nested.Properties["where"].Spread {
		t.Fatalf("nested spread marked at the wrong level: %+v", nested)
	}
	plain := clientCall(t, f, "prisma.user.findMany({ include").Expression.Args[0]
	if plain.Spread || plain.Properties["include"].Spread {
		t.Fatalf("plain object marked: %+v", plain)
	}
}

// TestBindingObjectSpread checks the route reduction, which module-level
// const values go through.
func TestBindingObjectSpread(t *testing.T) {
	f := extractInline(t, `const criteria = { where: { id: 1 } };
const base = { ...criteria, take: 2 };
`)
	bindings := f.Routes.Scopes[0].Bindings
	if !bindings["base"].Value.Spread {
		t.Fatalf("binding spread not marked: %+v", bindings["base"].Value)
	}
	if bindings["criteria"].Value.Spread || bindings["criteria"].Value.Properties["where"].Spread {
		t.Fatalf("binding without spread marked: %+v", bindings["criteria"].Value)
	}
	if _, ok := bindings["base"].Value.Properties["take"]; !ok {
		t.Fatal("pairs next to a spread lost")
	}
}

// TestTaggedTemplateSubstitutions checks that each substitution of a tagged
// template keeps its reduced inner expression in Object, comments skipped,
// while untagged templates keep their Parts form unchanged.
func TestTaggedTemplateSubstitutions(t *testing.T) {
	f := extractInline(t, "async function run(prisma, db, t, id) {\n"+
		"  await prisma.$queryRaw`SELECT * FROM ${t} WHERE id = ${ /* key */ id}`;\n"+
		"  db.execute(sql`select ${users.id} from ${users} where ${eq(users.id, id)}`);\n"+
		"  fetch(`/api/${id}`);\n"+
		"}\n")

	raw := substitutions(clientCall(t, f, "prisma.$queryRaw").Expression)
	if len(raw) != 2 {
		t.Fatalf("queryRaw substitutions: %+v", raw)
	}
	for i, want := range []string{"t", "id"} {
		o := raw[i].Object
		if o == nil || o.Kind != "identifier" || o.Name != want {
			t.Fatalf("substitution %d: %+v", i, raw[i])
		}
	}

	execute := clientCall(t, f, "db.execute").Expression
	tagged := execute.Args[0]
	if tagged.Kind != "call_expression" || tagged.Object == nil || tagged.Object.Name != "sql" {
		t.Fatalf("sql tag: %+v", tagged)
	}
	subs := substitutions(tagged)
	if len(subs) != 3 {
		t.Fatalf("sql substitutions: %+v", subs)
	}
	if o := subs[0].Object; o == nil || o.Kind != "member_expression" || o.Name != "id" || o.Object == nil || o.Object.Name != "users" {
		t.Fatalf("member substitution: %+v", subs[0])
	}
	if o := subs[1].Object; o == nil || o.Kind != "identifier" || o.Name != "users" {
		t.Fatalf("identifier substitution: %+v", subs[1])
	}
	if o := subs[2].Object; o == nil || o.Kind != "call_expression" || len(o.Args) != 2 || o.Object == nil || o.Object.Name != "eq" {
		t.Fatalf("call substitution: %+v", subs[2])
	}

	// The tagged sql call is also cached as a client call of its own.
	own := substitutions(clientCall(t, f, "sql`").Expression)
	if len(own) != 3 || own[1].Object == nil || own[1].Object.Name != "users" {
		t.Fatalf("own sql call: %+v", own)
	}

	untagged := clientCall(t, f, "fetch(").Expression.Args[0]
	if untagged.Kind != "template_string" || untagged.Object != nil || len(untagged.Parts) != 2 || untagged.Parts[1].Name != "id" {
		t.Fatalf("untagged template changed: %+v", untagged)
	}
}

// TestRouteCallTaggedSubstitution checks the route reduction (RouteCall
// arguments, kept only for route verbs and use) for a tagged template argument.
func TestRouteCallTaggedSubstitution(t *testing.T) {
	f := extractInline(t, "app.get(sql`select from ${table}`);\n")
	for _, c := range f.Routes.Calls {
		if c.Method != "get" {
			continue
		}
		subs := substitutions(c.Args[0])
		if len(subs) != 1 || subs[0].Object == nil || subs[0].Object.Name != "table" {
			t.Fatalf("route call substitution: %+v", c.Args[0])
		}
		return
	}
	t.Fatal("no route call for app.get")
}

// TestSpreadOmittedFromJSON checks that the marker is additive: an object
// without a spread serialises exactly as before.
func TestSpreadOmittedFromJSON(t *testing.T) {
	b, err := json.Marshal(facts.RouteExpr{Kind: "object"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Spread") {
		t.Fatalf("Spread serialised when false: %s", b)
	}
	b, err = json.Marshal(facts.RouteExpr{Kind: "object", Spread: true})
	if err != nil || !strings.Contains(string(b), `"Spread":true`) {
		t.Fatalf("Spread missing when true: %s %v", b, err)
	}
}
