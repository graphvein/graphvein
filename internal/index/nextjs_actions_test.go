package index_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/actions"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	actionsFixture  = "../../testdata/fixtures/nextjs-actions"
	actionsGolden   = "../../testdata/fixtures/nextjs-actions.golden.json"
	actionsDocsPath = "../../docs/extract/server-actions.md"
)

// actionsSnapshot keeps what T024b decides: server_action and action_call
// nodes, their edges, the code calls into actions, and the counted
// refusals.
func actionsSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"server_actions": {}, "action_calls": {}, "exposes_action": {}, "calls_action": {}, "contains": {}, "calls_into_actions": {}, "diagnostics": {}}
	keep := func(attrs map[string]any) string {
		// The shape attribute (T029 SH-N27..SH-N29) is checked by the
		// nextjs-shape fixture, not by the T024b view.
		if _, ok := attrs["shape"]; ok {
			c := make(map[string]any, len(attrs))
			for k, v := range attrs {
				if k != "shape" {
					c[k] = v
				}
			}
			attrs = c
		}
		data, err := json.Marshal(attrs)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	kinds := map[string]string{}
	handlers := map[string]bool{}
	for _, n := range s.Nodes {
		kinds[n.Identity] = n.Kind
		switch n.Kind {
		case "server_action":
			out["server_actions"] = append(out["server_actions"], n.Identity+" "+keep(n.Attributes))
			if h, ok := n.Attributes["handler"].(string); ok {
				handlers[h] = true
			}
		case "action_call":
			out["action_calls"] = append(out["action_calls"], n.Identity+" "+keep(n.Attributes))
		}
	}
	for _, e := range s.Edges {
		switch {
		case e.Kind == "exposes_action" || e.Kind == "calls_action":
			out[e.Kind] = append(out[e.Kind], e.Source+" -> "+e.Target)
		case e.Kind == "contains" && (kinds[e.Target] == "server_action" || kinds[e.Target] == "action_call"):
			out["contains"] = append(out["contains"], e.Source+" -> "+e.Target)
		case e.Kind == "calls" && handlers[e.Target]:
			out["calls_into_actions"] = append(out["calls_into_actions"], e.Source+" -> "+e.Target)
		}
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "server_action") {
			out["diagnostics"] = append(out["diagnostics"], d.File+" "+d.Code+" "+d.Message)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// TestServerActionsFixture indexes the fixture against its golden and
// checks that it reaches every row of the contract and D, and that every
// tag a branch reports is a row (the tag half of
// TestServerActionsContract).
func TestServerActionsFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(actionsFixture)); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	actions.SetTagHook(func(row string) { tags[row]++ })
	defer actions.SetTagHook(nil)
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	actions.SetTagHook(nil)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, actionsGolden, actionsSnapshot(t, snapshot))
	known := map[string]bool{}
	for _, r := range actions.Contract {
		known[r.ID] = true
		if tags[r.ID] == 0 {
			t.Errorf("the fixture never reaches row %s", r.ID)
		}
	}
	for row := range tags {
		if !known[row] {
			t.Errorf("a branch reports %s, which is not a row", row)
		}
	}
}

// TestServerActionsContract keeps the code table (actions.Contract), the
// docs table and the task file's table 1:1: same rows, same order, D
// last.
func TestServerActionsContract(t *testing.T) {
	task, taskOrder := nextRows(t, nextTaskPath, "## Claim contract: server actions (T024b, rows SA)")
	docs, docsOrder := nextRows(t, actionsDocsPath, "## Claim contract")
	for id, line := range task {
		if docs[id] != line {
			t.Errorf("row %s differs between the task file and the docs:\ntask: %s\ndocs: %s", id, line, docs[id])
		}
	}
	var ids []string
	for _, r := range actions.Contract {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(taskOrder, ids) || !reflect.DeepEqual(docsOrder, ids) {
		t.Errorf("rows: task %v, docs %v, code %v", taskOrder, docsOrder, ids)
	}
	if ids[len(ids)-1] != "D" {
		t.Error("D is not the last row")
	}
}

// TestServerActionsServerGraphOpen checks row SA-C3's dynamic loads: a
// server entry point's import() of a literal specifier puts that module in
// the server graph (boundary unknown, shared_module), and one the
// resolver cannot follow may load any module, so a call reached only from
// a 'use client' file is boundary unknown (server_graph_incomplete),
// never client.
func TestServerActionsServerGraphOpen(t *testing.T) {
	cases := []struct {
		name, load string
		want       map[string]any
	}{
		{"literal", "await import('../lib/helper')", map[string]any{"boundary": "unknown", "shared_module": true}},
		{"non-literal", "await import(process.env.MODULE as string)", map[string]any{"boundary": "unknown", "server_graph_incomplete": "app/page.tsx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"package.json":        `{"dependencies":{"next":"15.1.0"}}`,
				"app/actions.ts":      "'use server';\nexport async function save(id: number) { return id; }\n",
				"app/page.tsx":        "export default async function Page() {\n  const m = " + tc.load + ";\n  return <span>{String(m)}</span>;\n}\n",
				"lib/helper.ts":       "import { save } from '@/app/actions';\nexport function run() { return save(1); }\n",
				"components/Form.tsx": "'use client';\nimport { run } from '@/lib/helper';\nexport function Form() { return <button onClick={() => run()}>go</button>; }\n",
				"tsconfig.json":       `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./*"]},"jsx":"preserve"}}`,
			}
			for name, body := range files {
				p := root + "/" + name
				if err := os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			s, opt := open(t), options(t)
			emit(t, root, s, opt)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range snapshot.Nodes {
				if n.Kind != "action_call" || n.File != "lib/helper.ts" {
					continue
				}
				found = true
				for k, v := range tc.want {
					if n.Attributes[k] != v {
						t.Errorf("lib/helper.ts call: %v, want %s %v", n.Attributes, k, v)
					}
				}
				if n.Attributes["client_entry"] != nil {
					t.Errorf("lib/helper.ts call: %v, want no client label", n.Attributes)
				}
			}
			if !found {
				t.Fatal("no action_call in lib/helper.ts")
			}
		})
	}
}
