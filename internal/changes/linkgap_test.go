package changes

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// linkStream is a two-file stream: a client call in src/call.ts linked to
// a route in src/route.ts by calls_route. status is the call's status;
// routeGap adds an error diagnostic on src/route.ts (a knowledge gap).
func linkStream(status string, routeGap bool) string {
	route := "route:src/route.ts#GET%20%2Fx"
	call := "client_call:src/call.ts#load.GET%20%2Fx"
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	lines := []string{
		`{"type":"file","path":"src/call.ts","language":"typescript","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("a", 64) + `"}`,
		`{"type":"file","path":"src/route.ts","language":"typescript","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("b", 64) + `"}`,
		`{"type":"node","identity":"` + route + `","kind":"route","level":4,"name":"GET /x","qualified_name":"GET /x","file":"src/route.ts","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("c", 64) + `","attributes":{"framework":"express","method":"GET","pattern":"/x","handler":null,"handler_inline":false,"middleware":[],"paths":[{"template":"/x","conditional":false,"resolution":"literal"}]}}`,
		`{"type":"node","identity":"` + call + `","kind":"client_call","level":4,"name":"GET /x","qualified_name":"load.GET /x","file":"src/call.ts","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("d", 64) + `","attributes":{"method":"GET","status":"` + status + `"}}`,
		`{"type":"edge","kind":"calls_route","source":"` + call + `","target":"` + route + `","provenance":"inferred","confidence":0.9,"evidence":[{"file":"src/call.ts","span":` + span + `}]}`,
	}
	diagnostics := 0
	if routeGap {
		lines = append(lines, `{"type":"diagnostic","file":"src/route.ts","severity":"error","code":"parse_failed","message":"parsing src/route.ts: fixture failure"}`)
		diagnostics = 1
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"linkgap","generated_at":"2026-11-14T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":2,"node":2,"edge":1,"diagnostic":%d}}`, len(lines), diagnostics)
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestCallResolutionInGapTarget covers R3-8: a C4 call's status change
// whose linked route lies in a gap file is unknown_now, like its C5 link;
// with the route's file known the same change is a plain changed field.
func TestCallResolutionInGapTarget(t *testing.T) {
	for _, gap := range []bool{true, false} {
		ctx := context.Background()
		s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, stream := range []string{linkStream("linked", false), linkStream("ambiguous", gap)} {
			if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
				t.Fatal(err)
			}
		}
		c, err := Compare(ctx, s, "linkgap", Base{Kind: BasePrevious}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var call *Item
		for i := range c.items {
			if c.items[i].Row == "C4" {
				call = &c.items[i]
			}
		}
		// The item itself may be unknown_now when retention kept only the
		// newest run (base gaps_unknown); the field shows the rule.
		want := Changed
		if gap {
			want = UnknownNow
		}
		if call == nil || len(call.Fields) != 1 || call.Fields[0].Name != "status" || call.Fields[0].Change != want {
			t.Errorf("route gap %v: call item = %+v, want %s on status", gap, call, want)
		}
		s.Close()
	}
}

// columnStream is one migration-defined column; with withDefault its
// default is claimed too.
func columnStream(withDefault bool) string {
	hash, claims, attrs := "1", `[{"fact":"exists"},{"fact":"type"}]`, `"type":"integer"`
	if withDefault {
		hash, claims, attrs = "2", `[{"fact":"exists"},{"fact":"type"},{"fact":"default"}]`, `"type":"integer","default":"0"`
	}
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	lines := []string{
		`{"type":"file","path":"db/m.sql","language":"sql","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat(hash, 64) + `"}`,
		`{"type":"node","identity":"column:db/m.sql#t.c","kind":"column","level":4,"name":"c","qualified_name":"t.c","file":"db/m.sql","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("c", 64) + `","attributes":{"claims":` + claims + `,` + attrs + `}}`,
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"column","generated_at":"2026-11-14T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":1,"node":1,"edge":0,"diagnostic":0}}`, len(lines))
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestDefaultClaimedOnOneSide covers R3-10: a default first claimed on the
// head is known_now, never a changed default.
func TestDefaultClaimedOnOneSide(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, stream := range []string{columnStream(false), columnStream(true)} {
		if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Compare(ctx, s, "column", Base{Kind: BasePrevious}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var col *Item
	for i := range c.items {
		if c.items[i].Row == "C9" {
			col = &c.items[i]
		}
	}
	if col == nil || len(col.Fields) != 1 || col.Fields[0].Name != "default" || col.Fields[0].Change != KnownNow || col.Change == Changed {
		t.Fatalf("column item = %+v, want a known_now default", col)
	}
}

// callStream is a two-file stream: function caller in src/b.ts calls
// function callee in src/a.ts (a fact edge). calleeHash varies the
// callee's body; callerGap adds an error diagnostic on src/b.ts.
func callStream(calleeHash string, callerGap bool) string {
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	callee := "function:src/a.ts#callee"
	caller := "function:src/b.ts#caller"
	lines := []string{
		`{"type":"file","path":"src/a.ts","language":"typescript","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat(calleeHash, 64) + `"}`,
		`{"type":"file","path":"src/b.ts","language":"typescript","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("b", 64) + `"}`,
		`{"type":"node","identity":"` + callee + `","kind":"function","level":4,"name":"callee","qualified_name":"callee","file":"src/a.ts","span":` + span + `,"content_hash":"sha256:` + strings.Repeat(calleeHash, 64) + `","attributes":{}}`,
		`{"type":"node","identity":"` + caller + `","kind":"function","level":4,"name":"caller","qualified_name":"caller","file":"src/b.ts","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("d", 64) + `","attributes":{}}`,
		`{"type":"edge","kind":"calls","source":"` + caller + `","target":"` + callee + `","provenance":"fact","confidence":1,"evidence":[{"file":"src/b.ts","span":` + span + `}]}`,
	}
	diagnostics := 0
	if callerGap {
		lines = append(lines, `{"type":"diagnostic","file":"src/b.ts","severity":"error","code":"parse_failed","message":"parsing src/b.ts: fixture failure"}`)
		diagnostics = 1
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"callgap","generated_at":"2026-11-14T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":2,"node":2,"edge":1,"diagnostic":%d}}`, len(lines), diagnostics)
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestPathThroughGapNode covers R3-10: a path over fact steps that reaches
// a node in a gap file is inferred ("may depend on"), and ImpactViolations
// rejects it as fact; with the caller's file known the same path is a fact.
func TestPathThroughGapNode(t *testing.T) {
	for _, gap := range []bool{true, false} {
		ctx := context.Background()
		s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, stream := range []string{callStream("a", false), callStream("e", gap)} {
			if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
				t.Fatal(err)
			}
		}
		c, err := Compare(ctx, s, "callgap", Base{Kind: BasePrevious}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		imp, err := Impact(ctx, s, c, ImpactRequest{ID: "function:src/a.ts#callee"})
		if err != nil {
			t.Fatalf("gap %v: %v", gap, err)
		}
		if len(imp.Paths) != 1 || imp.Paths[0].Node != "function:src/b.ts#caller" {
			t.Fatalf("gap %v: paths %+v", gap, imp.Paths)
		}
		p := imp.Paths[0]
		want, claim := "fact", "depends on"
		if gap {
			want, claim = "inferred", "may depend on"
		}
		if p.Provenance != want || p.Claim != claim {
			t.Errorf("gap %v: provenance %s claim %q, want %s %q", gap, p.Provenance, p.Claim, want, claim)
		}
		if gap && (len(imp.Unknowns.GapNodes) != 1 || imp.Unknowns.GapNodes[0] != p.Node) {
			t.Errorf("gap nodes %v", imp.Unknowns.GapNodes)
		}
		if v := ImpactViolations(imp); len(v) != 0 {
			t.Errorf("gap %v: violations %v", gap, v)
		}
		if gap {
			// The rule as a check: the same path claimed as a fact is a violation.
			bad := *imp
			bad.Paths = []Path{p}
			bad.Paths[0].Provenance, bad.Paths[0].Claim = "fact", "depends on"
			if v := ImpactViolations(&bad); len(v) != 1 {
				t.Errorf("fact path through a gap node: violations %v", v)
			}
		}
		s.Close()
	}
}
