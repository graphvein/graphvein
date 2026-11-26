package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// goRoutesSnapshot renders the Go route nodes of a snapshot with every
// attribute but the shape, their owners, the handler edges and the route diagnostics.
func goRoutesSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"routes": {}, "exposes_route": {}, "diagnostics": {}}
	owners := map[string]string{}
	for _, e := range s.Edges {
		if e.Kind == "contains" && strings.HasPrefix(e.Target, "route:") {
			owners[e.Target] = e.Source
		}
		if e.Kind == "exposes_route" {
			out["exposes_route"] = append(out["exposes_route"], unescape(e.Source)+" -> "+unescape(e.Target))
		}
	}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		var keys []string
		for k := range n.Attributes {
			// The route shape (T029 SH-G) has its own golden
			// (go-shape.golden.json, TestGoShapeGoldenAndReplay).
			if k != "shape" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var attrs []string
		for _, k := range keys {
			v, err := json.Marshal(n.Attributes[k])
			if err != nil {
				t.Fatal(err)
			}
			attrs = append(attrs, k+"="+string(v))
		}
		out["routes"] = append(out["routes"], fmt.Sprintf("%s | %d:%d | %s | %s", unescape(n.Identity), n.Span.StartLine, n.Span.StartColumn, unescape(owners[n.Identity]), strings.Join(attrs, " ")))
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_") || d.Code == "go_routes_unread" {
			out["diagnostics"] = append(out["diagnostics"], d.File+": "+d.Severity+": "+d.Code+": "+d.Message)
		}
		if d.Code == "extract_invalid_record" || d.Code == "parse_failed" || d.Code == "extract_failed" || d.Code == "syntax_error" {
			t.Fatal(d)
		}
	}
	for key := range out {
		sort.Strings(out[key])
	}
	return out
}

func unescape(identity string) string {
	if v, err := url.PathUnescape(identity); err == nil {
		return v
	}
	return identity
}

// TestGoRoutesGoldenAndReplay indexes the go-routes fixture (T023:
// recognition, objects, registrations, handlers and the refusals of
// D; paths come with step GR3), checks the golden and that a warm run
// replays every file and emits the same records.
func TestGoRoutesGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/go-routes")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/go-routes.golden.json", goRoutesSnapshot(t, snapshot))
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
}

// TestGoRouteFormCounts checks the structured counts behind
// go_routes_unread (couldn't-read row CR-38): the file node of every file
// with the diagnostic holds go_route_forms_outside_contract equal to the
// counts its message writes (parsed here, in the test only), and no other
// file node holds the attribute.
func TestGoRouteFormCounts(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/go-routes")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]float64{}
	message := regexp.MustCompile(`^(\d+) Go router forms are outside the route claim contract and claim nothing \((.*)\)$`)
	for _, d := range snapshot.Diagnostics {
		if d.Code != "go_routes_unread" {
			continue
		}
		m := message.FindStringSubmatch(d.Message)
		if m == nil {
			t.Fatalf("message form %q", d.Message)
		}
		forms, sum := map[string]float64{}, 0
		for _, part := range strings.Split(m[2], ", ") {
			form, n, _ := strings.Cut(part, "=")
			v, err := strconv.Atoi(n)
			if err != nil {
				t.Fatal(part)
			}
			forms[form] = float64(v)
			sum += v
		}
		if strconv.Itoa(sum) != m[1] {
			t.Fatalf("message total %s, forms %v", m[1], forms)
		}
		want[d.File] = forms
	}
	if len(want) < 5 {
		t.Fatalf("only %d files with go_routes_unread", len(want))
	}
	got := map[string]map[string]float64{}
	for _, n := range snapshot.Nodes {
		v, ok := n.Attributes["go_route_forms_outside_contract"]
		if !ok {
			continue
		}
		if n.Kind != "file" {
			t.Fatalf("%s holds the attribute", n.Identity)
		}
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		forms := map[string]float64{}
		if err := json.Unmarshal(data, &forms); err != nil {
			t.Fatal(err)
		}
		got[n.File] = forms
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attribute %v\nmessages %v", got, want)
	}
}
