package typescript

import (
	"fmt"
	"strings"
	"testing"
)

// passFlags renders the flags of the pass events of the first shape
// function: callee Top Awaited Test TestAt.
func passFlags(t *testing.T, source string) []string {
	t.Helper()
	f, _ := parseModels(t, "a.ts", source)
	if f.Shape == nil || len(f.Shape.Functions) == 0 {
		t.Fatal("no shape function")
	}
	var out []string
	for _, e := range f.Shape.Functions[0].Events {
		if e.Kind != "pass" || e.Callee == nil {
			continue
		}
		name := e.Callee.Name
		if e.Callee.Object != nil {
			name = e.Callee.Object.Name + "." + name
		}
		out = append(out, fmt.Sprintf("%s top=%t await=%t test=%s at=%d", name, e.Top, e.Awaited, e.Test, e.TestAt))
	}
	return out
}

// TestShapeTopLevelCalls checks the Top and Awaited flags (rows SH-E12,
// SH-E19 of docs/contract/claims/route-shape-express.md): only a call that
// is a whole top-level statement or initializer is Top.
func TestShapeTopLevelCalls(t *testing.T) {
	cases := []struct{ source, want string }{
		{`(req, res) => { Item.parse(req.body); }`, "Item.parse top=true await=false"},
		{`async (req, res) => { const v = await Item.parseAsync(req.body); }`, "Item.parseAsync top=true await=true"},
		{`(req, res) => { Item.parseAsync(req.body).catch(() => 1); }`, "Item.parseAsync top=false await=false"},
		{`(req, res) => { if (x) { Item.parse(req.body); } }`, "Item.parse top=false await=false"},
		{`(req, res) => { x || Item.parse(req.body); }`, "Item.parse top=false await=false"},
		{`(req, res) => { for (const k of y) { Item.parse(req.body); } }`, "Item.parse top=false await=false"},
		{`(req, res) => { try { Item.parse(req.body); } catch (e) {} }`, "Item.parse top=false await=false"},
		{`(req, res) => { [1].forEach(() => Item.parse(req.body)); }`, "Item.parse top=false await=false"},
		{`(req, res) => Item.parse(req.body)`, "Item.parse top=true await=false"},
		{`async (req, res) => { await (check(req.body)); }`, "check top=true await=true"},
	}
	for _, c := range cases {
		got := passFlags(t, c.source)
		if len(got) != 1 || !strings.HasPrefix(got[0], c.want+" ") {
			t.Errorf("%s:\n got %v\nwant %s", c.source, got, c.want)
		}
	}
}

// TestShapeTestedResults checks the Test flag (rows SH-E13, SH-E14,
// SH-E19): a top-level if whose condition is the failure test (or an ||
// with it) and whose consequence always returns or throws.
func TestShapeTestedResults(t *testing.T) {
	cases := []struct{ source, want string }{
		{`(req, res) => { const r = S.safeParse(req.body); if (!r.success) { return res.end(); } }`, "test=success"},
		{`(req, res) => { const r = S.safeParse(req.body); if (r.error) throw r.error; }`, "test=error"},
		{`(req, res) => { const { error } = S.validate(req.body); if (x || error) { return 1; } }`, "test=error"},
		{`(req, res) => { const { error: e } = S.validate(req.body); if (e) { return 1; } }`, "test=error"},
		{`(req, res) => { const { success } = S.safeParse(req.body); if (!success) { return 1; } }`, "test=success"},
		{`(req, res) => { const r = S.safeParse(req.body); if (r.error && strict) { return 1; } }`, "test="},
		{`(req, res) => { const r = S.safeParse(req.body); if (r.success) { return 1; } }`, "test="},
		{`(req, res) => { const r = S.safeParse(req.body); [1].forEach(() => { if (!r.success) return; }); }`, "test="},
		{`(req, res) => { const r = S.safeParse(req.body); if (!r.success) { log(); } }`, "test="},
		{`(req, res) => { const r = S.safeParse(req.body); if (!r.success) { return 1; } else { x(); } }`, "test=success"},
		{`(req, res) => { if (y) { const r = S.safeParse(req.body); if (!r.success) { return 1; } } }`, "test="},
		{`(req, res) => { let r = S.safeParse(req.body); if (!r.success) { return 1; } }`, "test="},
		{`(req, res) => { const r = S.safeParse(req.body); g(() => { const r = 1; }); if (!r.success) { return 1; } }`, "test="},
	}
	for _, c := range cases {
		got := passFlags(t, c.source)
		if len(got) != 1 || !strings.Contains(got[0], " "+c.want+" ") {
			t.Errorf("%s:\n got %v\nwant %s", c.source, got, c.want)
		}
	}
	// TestAt counts the events before the if statement.
	got := passFlags(t, `(req, res) => { const r = S.safeParse(req.body); res.set('a', 'b'); if (!r.success) { return 1; } }`)
	if len(got) != 1 || !strings.HasSuffix(got[0], "at=2") {
		t.Errorf("TestAt: %v", got)
	}
}

// TestShapeAsync records async functions.
func TestShapeAsync(t *testing.T) {
	f, _ := parseModels(t, "a.ts", `async function check(b) { S.parse(b); }
const h = async (req) => { S.parse(req.body); };
function sync(b) { S.parse(b); }`)
	var got []bool
	for _, fn := range f.Shape.Functions {
		got = append(got, fn.Async)
	}
	if fmt.Sprint(got) != "[true true false]" {
		t.Errorf("async flags %v", got)
	}
}

// TestShapeArms records the top-level statement and if branch of events.
func TestShapeArms(t *testing.T) {
	f, _ := parseModels(t, "a.ts", `(req, res) => {
  if (a) { return res.end(); }
  if (b) { res.end(); } else { res.json(2); }
  res.json(1);
}`)
	var got []string
	for _, e := range f.Shape.Functions[0].Events {
		if e.Kind == "return" {
			// Return events (T029 SH-N) are checked by TestShapeNext.
			continue
		}
		got = append(got, fmt.Sprintf("%d/%d/%t", e.Stmt, e.Arm, e.ArmExits))
	}
	if fmt.Sprint(got) != "[1/1/true 2/1/false 2/2/false 3/0/false]" {
		t.Errorf("arms %v", got)
	}
}

// TestShapeReturnFn records the function a factory returns.
func TestShapeReturnFn(t *testing.T) {
	f, _ := parseModels(t, "a.ts", `const auth = (p) => {
  const mw = (req, res, next) => { if (!req.user) { res.status(403).end(); } else { next(); } };
  return mw;
};
const inline = (p) => (req, res, next) => next();
function two(p) { if (p) { return (req, res) => res.end(); } return (req, res) => res.json(1); }`)
	var got []bool
	for _, fn := range f.Shape.Functions {
		if fn.Params != nil && fn.Params[0].Name == "p" {
			got = append(got, fn.ReturnFn > 0)
		}
	}
	if fmt.Sprint(got) != "[true true false]" {
		t.Errorf("return functions %v", got)
	}
}

// TestShapeNext records the Next.js forms (T029 SH-N): Fetch API body
// readers and new URL(...) as parameter paths, next/headers calls as
// parameter -2, next/navigation calls, and return statements (only those
// building a response in a helper without other events).
func TestShapeNext(t *testing.T) {
	f, _ := parseModels(t, "a.ts", `export async function POST(request, { params }) {
  const body = await request.json();
  const { searchParams } = new URL(request.url);
  searchParams.get('q');
  (await headers()).get('x-a');
  const jar = await cookies();
  S.parse(await request.clone().json());
  if (!body.ok) { notFound(); }
  const run = () => { return 1; };
  return NextResponse.json({ a: body.a }, { status: 201 });
}
function plain(x) { return x + 1; }
function reply() { return Response.json({ ok: true }); }`)
	var got []string
	for _, fn := range f.Shape.Functions {
		for _, e := range fn.Events {
			got = append(got, fmt.Sprintf("%d:%s:%d:%s", fn.Line, e.Kind, e.Param, strings.Join(e.Path, ".")))
		}
	}
	want := []string{
		"1:keys:1:", "1:read:0:json()", "1:keys:0:url.URL()", "1:call:0:url.URL().searchParams",
		"1:call:-2:headers()", "1:read:-2:cookies()", "1:pass:0:json()", "1:read:0:json().ok",
		"1:call:-2:notFound", "1:read:0:json().a", "1:return:-1:", "12:read:0:", "12:return:-1:", "13:return:-1:",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("events\n got %v\nwant %v", got, want)
	}
	ret := f.Shape.Functions[0].Events[len(f.Shape.Functions[0].Events)-1]
	if len(ret.Methods) != 2 || ret.Methods[1].Name != "args" || len(ret.Methods[1].Args) != 2 || ret.Methods[1].Args[1].Props["status"].Name != "201" {
		t.Errorf("return event %+v", ret)
	}
}

// TestShapeNextReview covers the SH-N review forms: an un-awaited
// next/headers call is its own root, and a next/navigation call in a
// nested function is marked Nested.
func TestShapeNextReview(t *testing.T) {
	f, _ := parseModels(t, "a.ts", `export async function GET() {
  headers().get('x-a');
  const h = headers();
  h.get('x-b');
  (await cookies()).get('c');
  const later = () => notFound();
  redirect('/x');
  return Response.json({ ok: 1 });
}`)
	var got []string
	for _, e := range f.Shape.Functions[0].Events {
		got = append(got, fmt.Sprintf("%s:%d:%s:%v", e.Kind, e.Param, strings.Join(e.Path, "."), e.Nested))
	}
	want := []string{
		"call:-2:headers()sync:false", "read:-2:headers()sync:false", "call:-2:headers()sync:false",
		"call:-2:cookies():false", "call:-2:notFound:true", "call:-2:redirect:false", "return:-1::false",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("events\n got %v\nwant %v", got, want)
	}
}
