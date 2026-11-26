package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/testscope"
)

// TestWalkScope checks MC-31's filter on reached nodes the session
// fixtures do not have: a node in a file graphvein.yaml's tests section
// classifies, a node with scope attribute test, and a production node
// whose stored path passes through test code (kept, via_test_code). A
// project member without a checkout gets the built-in rules only.
func TestWalkScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.File), []byte("version: 1\ntests:\n  include:\n    - e2e/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node := func(id, file string, attrs map[string]any) map[string]any {
		return map[string]any{"node": map[string]any{"identity": id, "file": file, "attributes": attrs}}
	}
	step := func(source, target string) map[string]any { return map[string]any{"source": source, "target": target} }
	helper := node("function:e2e/helper.ts#helper", "e2e/helper.ts", nil)
	helper["path"] = []any{step("function:e2e/helper.ts#helper", "function:src/a.ts#target")}
	call := node("client_call:src/b.ts#c", "src/b.ts", map[string]any{"scope": "test"})
	prod := node("function:src/main.ts#main", "src/main.ts", nil)
	prod["path"] = []any{step("function:src/main.ts#main", "function:e2e/helper.ts#helper"), step("function:e2e/helper.ts#helper", "function:src/a.ts#target")}
	plain := node("function:src/c.ts#plain", "src/c.ts", nil)
	plain["path"] = []any{step("function:src/c.ts#plain", "function:src/a.ts#target")}

	s := &Server{opt: Options{Root: root, Repository: "r"}}
	w := &walkScope{s: s, scope: "production", rules: map[string]testscope.Rules{}, test: map[string]bool{}}
	if err := w.classify([]any{helper, call, prod, plain}); err != nil {
		t.Fatal(err)
	}
	target := nodeKey("", "function:src/a.ts#target")
	for id, want := range map[string]bool{"function:e2e/helper.ts#helper": true, "client_call:src/b.ts#c": true, "function:src/main.ts#main": false, "function:src/c.ts#plain": false} {
		if got := w.hidden("", id); got != want {
			t.Errorf("hidden(%s) = %v, want %v", id, got, want)
		}
	}
	if !w.viaTest(prod, target, nodeKey("", "function:src/main.ts#main")) {
		t.Error("main's path passes through test code: want via_test_code")
	}
	if w.viaTest(plain, target, nodeKey("", "function:src/c.ts#plain")) || w.viaTest(helper, target, nodeKey("", "function:e2e/helper.ts#helper")) {
		t.Error("via_test_code must not count the result itself or the target")
	}
	all := &walkScope{s: s, scope: "all", test: w.test}
	if all.hidden("", "function:e2e/helper.ts#helper") {
		t.Error("scope all hides nothing")
	}
	e := newEnvelope("callers")
	w.report(e, 2, 1)
	if len(e.Unknowns) != 2 || e.Unknowns[0].Kind != "test_scope_hidden" || e.Unknowns[0].Row != "MC-31" || e.Unknowns[1].Kind != "test_scope_unfiltered" || e.Unknowns[1].Row != "MC-31" {
		t.Errorf("report: %+v", e.Unknowns)
	}
	// Production scope always names what it does not filter, also when
	// nothing was hidden; scope all names neither.
	e = newEnvelope("callers")
	w.report(e, 0, 0)
	if len(e.Unknowns) != 1 || e.Unknowns[0].Kind != "test_scope_unfiltered" {
		t.Errorf("report with nothing hidden: %+v", e.Unknowns)
	}
	e = newEnvelope("callers")
	all.report(e, 2, 1)
	if len(e.Unknowns) != 0 {
		t.Errorf("report in scope all: %+v", e.Unknowns)
	}

	// A project member without a checkout: built-in rules only.
	project := &config.Project{Name: "p", Members: []config.Member{{Repository: "m", Path: root, Checkout: false}}}
	pw := &walkScope{s: &Server{opt: Options{Project: project}}, scope: "production", rules: map[string]testscope.Rules{}, test: map[string]bool{}}
	member := node("function:e2e/helper.ts#helper", "e2e/helper.ts", nil)
	member["node"].(map[string]any)["repository"] = "m"
	if err := pw.classify([]any{member}); err != nil {
		t.Fatal(err)
	}
	if pw.hidden("m", "function:e2e/helper.ts#helper") {
		t.Error("a member without a checkout has no graphvein.yaml rules: e2e/ is not test code by the built-in names")
	}
	if _, err := walkScopeParam("test"); err == nil {
		t.Error("scope test: want bad_request")
	}
}

// TestWalkScopeUnfiltered is the T043 review's input (A1, B1): the
// api-section fixture's test file gains two unresolved calls and a call
// of the tie route. In production scope, impact's count of unresolved
// call sites in the files of the results counts the shown results' files
// only (never the hidden test file's), the tie route's near miss in the
// test file says test_code, and both answers name the unfiltered parts.
func TestWalkScopeUnfiltered(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		src := "export function fromTest() {\n  undefinedHelperOne();\n  undefinedHelperTwo();\n  fetch('/t016d-tie');\n  return fetch('/t016d-items');\n}\n"
		if err := os.WriteFile(filepath.Join(root, "web/src/http.test.ts"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, root, w, "api")
		tr := runSession(t, Options{Root: root, Repository: "api", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, root, []toolCall{
			{"impact", args("target", "listItems")},
			{"impact", args("target", "listItems", "scope", "all")},
			{"callers", args("target", tieRoute)},
		})
		unknowns := func(i int) map[string]Unknown {
			raw, err := json.Marshal(tr.Calls[i].Envelope)
			if err != nil {
				t.Fatal(err)
			}
			var env Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatal(err)
			}
			out := map[string]Unknown{}
			for _, u := range env.Unknowns {
				if _, ok := out[u.Kind]; !ok {
					out[u.Kind] = u
				}
			}
			return out
		}
		prod, all, callers := unknowns(0), unknowns(1), unknowns(2)
		if _, ok := prod["test_scope_hidden"]; !ok {
			t.Fatalf("production impact hides the test file's results: %+v", prod)
		}
		// Shown results' files hold 3 unresolved call sites; the hidden
		// test file holds the other 2 that scope all counts.
		if got := prod["unknown_calls_in_result_files"].Text; !strings.HasPrefix(got, "3 ") {
			t.Errorf("production unknown_calls_in_result_files: %q", got)
		}
		if got := all["unknown_calls_in_result_files"].Text; !strings.HasPrefix(got, "5 ") {
			t.Errorf("scope all unknown_calls_in_result_files: %q", got)
		}
		for i, u := range []map[string]Unknown{prod, callers} {
			if _, ok := u["test_scope_unfiltered"]; !ok {
				t.Errorf("call %d: production scope without test_scope_unfiltered: %+v", i, u)
			}
		}
		if _, ok := all["test_scope_unfiltered"]; ok {
			t.Error("scope all filters nothing: no test_scope_unfiltered")
		}
		nm, ok := callers["near_miss"]
		data, _ := nm.Data.(map[string]any)
		if !ok || str(data["file"]) != "web/src/http.test.ts" || data["test_code"] != true {
			t.Errorf("near miss in the test file without test_code: %+v", nm)
		}
	})
}
