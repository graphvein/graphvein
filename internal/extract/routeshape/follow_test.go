package routeshape

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// graph is a fake resolver: each function name maps to the calls through
// which the followed value leaves it. Functions live in "f.go" at line 1
// and calls are written at line 2.
type graph map[string][]Call

func fn(name string) Step { return Step{File: "f.go", Line: 1, Function: name} }

func to(name string) Call {
	return Call{Site: Step{File: "f.go", Line: 2, Function: name + "(r)"}, Target: fn(name), Value: "r"}
}

func stop(s Stop, detail string) Call {
	return Call{Site: Step{File: "f.go", Line: 2, Function: "x(r)"}, Stop: s, Detail: detail}
}

func (g graph) Visit(_ context.Context, f Frame) ([]Call, error) {
	if f.Function.Function == "broken" {
		return nil, errors.New("parse failed")
	}
	return g[f.Function.Function], nil
}

func summary(r Result) string {
	var parts []string
	for _, c := range r.Chains {
		var names []string
		for _, s := range c.Steps {
			names = append(names, s.Function)
		}
		parts = append(parts, fmt.Sprintf("%s@%d[%s]", c.Stop, c.Depth, strings.Join(names, ">")))
	}
	return strings.Join(parts, " ")
}

func TestFollowStops(t *testing.T) {
	g := graph{
		"handler": {to("a"), stop(StopUnresolved, "no definition"), stop(StopAmbiguous, "2 candidates"), stop(StopExternal, "lib"), to("broken")},
		"a":       {to("b"), stop(StopTransformed, "spread"), stop(StopOpaqueType, "any"), to("handler"), to("c")},
		"b":       {to("c"), {Site: Step{File: "f.go", Line: 3, Function: "y(r)"}}},
		"c":       nil,
	}
	res, err := Follow(context.Background(), Frame{Function: fn("handler"), Value: "r"}, DefaultDepth, g)
	if err != nil {
		t.Fatal(err)
	}
	want := "ended@3[a>b>c] unresolved@2[a>b] transformed@1[a] opaque_type@1[a] cycle@1[a] joined@1[a] " +
		"unresolved@0[] ambiguous@0[] external@0[] resolver_error@1[broken]"
	if got := summary(res); got != want {
		t.Fatalf("chains\n got %s\nwant %s", got, want)
	}
	if len(res.Frames) != 5 {
		t.Errorf("%d frames, want 5 (handler a b c broken)", len(res.Frames))
	}
	// The chain of an item found in c is the calls followed to reach it.
	if c := res.Frames[3]; c.Function.Function != "c" || len(c.Chain) != 3 || c.Chain[2] != (Step{File: "f.go", Line: 2, Function: "c"}) {
		t.Errorf("frame c: %+v", c)
	}
	for _, ch := range res.Chains {
		if (ch.Stop == StopEnded || ch.Stop == StopResolverError) != (ch.At == nil) {
			t.Errorf("%s: at %v", ch.Stop, ch.At)
		}
	}
}

func TestFollowDepthCap(t *testing.T) {
	// A straight line of 12 calls.
	g := graph{}
	for i := range 12 {
		g[fmt.Sprintf("f%d", i)] = []Call{to(fmt.Sprintf("f%d", i+1))}
	}
	for _, limit := range []int{1, DefaultDepth, MaxDepthLimit} {
		res, err := Follow(context.Background(), Frame{Function: fn("f0"), Value: "r"}, limit, g)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Chains) != 1 || res.Chains[0].Stop != StopDepthCap || res.Chains[0].Depth != limit || len(res.Chains[0].Steps) != limit {
			t.Fatalf("limit %d: %s", limit, summary(res))
		}
		if !strings.Contains(res.Chains[0].Detail, fmt.Sprintf("(%d)", limit)) || res.Chains[0].At.Function != fmt.Sprintf("f%d", limit+1) {
			t.Errorf("limit %d: %+v", limit, res.Chains[0])
		}
		// Stored on a shape, the chain validates.
		s := New(limit)
		s.Follow.Chains = Records(SectionBody, res)
		if vs := Validate(s); vs != nil {
			t.Errorf("limit %d: %v", limit, vs)
		}
	}
	for _, bad := range []int{0, MaxDepthLimit + 1} {
		if _, err := Follow(context.Background(), Frame{Function: fn("f0"), Value: "r"}, bad, g); err == nil {
			t.Errorf("limit %d accepted", bad)
		}
	}
}

func TestFollowBudget(t *testing.T) {
	// A wide tree: every node calls 20 distinct children, 3 levels.
	g := ResolverFunc(func(_ context.Context, f Frame) ([]Call, error) {
		if f.Depth >= 3 {
			return nil, nil
		}
		var calls []Call
		for i := range 20 {
			calls = append(calls, to(fmt.Sprintf("%s.%d", f.Function.Function, i)))
		}
		return calls, nil
	})
	res, err := Follow(context.Background(), Frame{Function: fn("root"), Value: "r"}, DefaultDepth, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != MaxFrames {
		t.Errorf("%d frames, want %d", len(res.Frames), MaxFrames)
	}
	stats := NewStats()
	stats.Add(res)
	if stats.Stop[StopBudget] == 0 || stats.Stop[StopEnded] == 0 {
		t.Errorf("stats %+v", stats)
	}
}

func TestFollowCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Follow(ctx, Frame{Function: fn("handler"), Value: "r"}, DefaultDepth, graph{"handler": {to("a")}})
	if err != nil {
		t.Fatal(err)
	}
	if summary(res) != "resolver_error@0[]" {
		t.Errorf("got %s", summary(res))
	}
}

func TestStatsTable(t *testing.T) {
	g := graph{"h": {to("a"), stop(StopUnresolved, "")}, "a": {to("b")}, "b": {to("c")}, "c": nil}
	stats := NewStats()
	for _, limit := range []int{2, 6} {
		res, err := Follow(context.Background(), Frame{Function: fn("h"), Value: "r"}, limit, g)
		if err != nil {
			t.Fatal(err)
		}
		stats.Add(res)
	}
	sh := New(2)
	sh.Follow.Chains = []ChainRecord{{Section: SectionQuery, Start: fn("h"), Steps: []Step{fn("a"), fn("b")}, Depth: 2, Stop: StopDepthCap, At: &Step{File: "f.go", Line: 2, Function: "c"}}}
	stats.AddShape(sh)
	want := "chains 5, at depth cap 2\nlimit 2: 3\nlimit 6: 2\ndepth 0: 2\ndepth 2: 2\ndepth 3: 1\nstop ended: 1\nstop depth_cap: 2\nstop unresolved: 2\n"
	if got := stats.Table(); got != want {
		t.Errorf("table\n%s\nwant\n%s", got, want)
	}
	if Summarize(sh).DepthCap != 1 {
		t.Error("summary does not count the depth cap")
	}
}
