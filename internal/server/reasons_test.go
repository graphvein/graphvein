package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// codes returns the reason codes and details of a call, "-" for a nil
// detail.
func codes(rs []reason) [][2]string {
	out := [][2]string{}
	for _, r := range rs {
		d := "-"
		if r.Detail != nil {
			d = *r.Detail
		}
		out = append(out, [2]string{r.Code, d})
	}
	return out
}

func clientCall(attrs map[string]any) contract.Node {
	base := map[string]any{"library": "fetch", "method": "GET", "url_expression": "u", "base": "none"}
	for k, v := range attrs {
		base[k] = v
	}
	return contract.Node{Identity: "client_call:a.ts#c", Kind: "client_call", File: "a.ts", Name: "GET u", Attributes: base}
}

func entry(template any, status string, extra ...any) any {
	e := map[string]any{"template": template, "status": status, "resolution": "literal", "candidates": []any{}, "candidate_count": json.Number("0")}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i].(string)] = extra[i+1]
	}
	return e
}

func TestClientReasons(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attrs    map[string]any
		hasEdges bool
		want     [][2]string
	}{
		{"linked", map[string]any{"status": "linked", "paths": []any{entry("/a", "linked")}}, true, [][2]string{}},
		{"linked without edge", map[string]any{"status": "linked", "paths": []any{entry("/a", "linked")}}, false, [][2]string{}},
		{"unknown prefix", map[string]any{"status": "linked", "assumes": []any{"UP-V1"}, "paths": []any{entry("/v1/api/a", "linked", "route_prefix", "unresolved")}}, true, [][2]string{{"prefix_assumed", "UP-V1"}}},
		{"unknown prefix, aligned base", map[string]any{"status": "linked", "assumes": []any{"UP-V1", "UP-V2"}, "paths": []any{entry("/api/a", "linked", "route_prefix", "unresolved")}}, true, [][2]string{{"prefix_assumed", "UP-V1"}, {"prefix_assumed", "UP-V2"}}},
		{"path assumption of the matched entry", map[string]any{"status": "linked", "assumes": []any{"RO-V2"}, "paths": []any{entry("/z/b", "linked", "assumes", []any{"RO-V2"})}}, true, [][2]string{{"path_assumed", "RO-V2"}}},
		{"unknown prefix and path assumption", map[string]any{"status": "linked", "assumes": []any{"UP-V1", "GR-V5"}, "paths": []any{entry("/v1/api/a", "linked", "route_prefix", "unresolved")}}, true, [][2]string{{"prefix_assumed", "UP-V1"}, {"path_assumed", "GR-V5"}}},
		{"next.js assumption not shown", map[string]any{"status": "linked", "assumes": []any{"NX-V1"}, "paths": []any{entry("/a", "linked")}}, true, [][2]string{}},
		{"unknown prefix alternative", map[string]any{"status": "unmatched", "assumes": []any{"UP-V1"}, "paths": []any{entry("/v1/api/a", "linked"), entry("/b", "unmatched")}}, true, [][2]string{{"no_matching_route", "GET /b"}, {"alternative_linked", "-"}, {"prefix_assumed", "UP-V1"}}},
		{"wrapper", map[string]any{"status": "wrapper", "paths": []any{entry("/{p}", "dynamic")}}, false, [][2]string{{"wrapper_inner", "-"}}},
		{"unmatched", map[string]any{"status": "unmatched", "paths": []any{entry("/a", "unmatched")}}, false, [][2]string{{"no_matching_route", "GET /a"}}},
		{"unmatched, method unknown", map[string]any{"status": "dynamic", "method": nil, "paths": []any{entry("/a", "unmatched")}}, false, [][2]string{{"no_matching_route", "/a"}, {"method_unknown", "-"}}},
		{"ambiguous", map[string]any{"status": "ambiguous", "paths": []any{entry("/a", "ambiguous", "candidates", []any{"r", "s"}, "candidate_count", json.Number("3"))}}, false, [][2]string{{"ambiguous", "/a"}}},
		{"test only", map[string]any{"status": "test_only", "paths": []any{entry("/a", "test_only")}}, false, [][2]string{{"test_only_target", "/a"}}},
		{"url unevaluated", map[string]any{"status": "dynamic", "paths": []any{entry(nil, "dynamic")}}, false, [][2]string{{"url_unevaluated", "u"}}},
		{"some unevaluated", map[string]any{"status": "dynamic", "paths": []any{entry("/a", "linked"), entry(nil, "dynamic")}}, true, [][2]string{{"some_alternatives_unevaluated", "u"}, {"alternative_linked", "-"}}},
		{"method unknown", map[string]any{"status": "dynamic", "method": nil, "method_expression": "opts.method", "paths": []any{entry("/a", "dynamic")}}, false, [][2]string{{"method_unknown", "opts.method"}}},
		{"base unresolved", map[string]any{"status": "unmatched", "base": "unresolved", "base_expression": "API", "paths": []any{entry("/a", "unmatched")}}, false, [][2]string{{"no_matching_route", "GET /a"}, {"base_unresolved", "API"}}},
		{"empty base expression", map[string]any{"status": "unmatched", "base": "unresolved", "base_expression": "", "paths": []any{entry("/a", "unmatched")}}, false, [][2]string{{"no_matching_route", "GET /a"}}},
		{"generated", map[string]any{"status": "dynamic", "library": "orval", "operation": "listThings", "paths": []any{entry(nil, "dynamic", "resolution", "generated")}}, false, [][2]string{{"url_unevaluated", "u"}, {"generated_unproven", "listThings"}}},
		{"alternative linked", map[string]any{"status": "ambiguous", "paths": []any{entry("/a", "linked"), entry("/b", "ambiguous")}}, true, [][2]string{{"ambiguous", "/b"}, {"alternative_linked", "-"}}},
		{"fallback", map[string]any{"status": "dynamic", "paths": []any{entry("/a", "dynamic")}}, false, [][2]string{{"status", "dynamic"}}},
		{"unknown status", map[string]any{"status": "weird"}, true, [][2]string{{"status_unknown", "weird"}}},
		{"case variant", map[string]any{"status": "LINKED"}, true, [][2]string{{"status_unknown", "LINKED"}}},
		{"trailing space", map[string]any{"status": "linked "}, true, [][2]string{{"status_unknown", "linked "}}},
		{"missing status", map[string]any{}, false, [][2]string{{"status_unknown", ""}}},
		{"tauri status on http", map[string]any{"status": "plugin"}, false, [][2]string{{"status_unknown", "plugin"}}},
	} {
		got := codes(callReasons(httpTransport, clientCall(tc.attrs), tc.hasEdges))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// The ambiguous text names the full tie size, never the capped list.
	r := callReasons(httpTransport, clientCall(map[string]any{"status": "ambiguous", "paths": []any{entry("/a", "ambiguous", "candidates", []any{"r"}, "candidate_count", json.Number("12"))}}), false)
	if r[0].Text != "12 routes match equally" {
		t.Fatalf("text %q", r[0].Text)
	}
	// Without a stored count, a list cut at the cap is only a lower bound.
	capped := []any{}
	for i := 0; i < 10; i++ {
		capped = append(capped, string(rune('a'+i)))
	}
	noCount := map[string]any{"template": "/a", "status": "ambiguous", "candidates": capped}
	r = callReasons(httpTransport, clientCall(map[string]any{"status": "ambiguous", "paths": []any{noCount}}), false)
	if r[0].Text != "At least 10 routes match equally" {
		t.Fatalf("capped text %q", r[0].Text)
	}
	noCount["candidates"] = capped[:3]
	r = callReasons(httpTransport, clientCall(map[string]any{"status": "ambiguous", "paths": []any{noCount}}), false)
	if r[0].Text != "3 routes match equally" {
		t.Fatalf("uncapped text %q", r[0].Text)
	}
	r = callReasons(httpTransport, clientCall(map[string]any{"status": "dynamic", "paths": []any{entry("/a", "linked"), entry(nil, "dynamic")}}), true)
	if last := r[len(r)-1]; last.Text != `A stored link exists although the status is "dynamic"` {
		t.Fatalf("alternative text %q", last.Text)
	}
}

func TestCandidateTie(t *testing.T) {
	ten := []any{}
	for i := 0; i < 10; i++ {
		ten = append(ten, string(rune('a'+i)))
	}
	for _, tc := range []struct {
		name  string
		p     map[string]any
		count int
		exact bool
	}{
		{"stored", map[string]any{"candidates": []any{"a", "b"}, "candidate_count": json.Number("2")}, 2, true},
		{"stored beyond the cap", map[string]any{"candidates": ten, "candidate_count": json.Number("12")}, 12, true},
		{"one candidate stores zero", map[string]any{"candidates": []any{"a"}, "candidate_count": json.Number("0")}, 1, true},
		{"absent", map[string]any{"candidates": []any{"a", "b"}}, 2, true},
		{"absent at the cap", map[string]any{"candidates": ten}, 10, false},
		{"stored below the list, at the cap", map[string]any{"candidates": ten, "candidate_count": json.Number("3")}, 10, false},
		{"float", map[string]any{"candidates": []any{"a"}, "candidate_count": float64(4)}, 4, true},
	} {
		count, exact := candidateTie(tc.p)
		if count != tc.count || exact != tc.exact {
			t.Errorf("%s: %d %v, want %d %v", tc.name, count, exact, tc.count, tc.exact)
		}
	}
}

func ipcCall(attrs map[string]any) contract.Node {
	base := map[string]any{"framework": "tauri", "transport": "tauri_ipc", "form": "direct", "command": "x", "conditional": false}
	for k, v := range attrs {
		base[k] = v
	}
	return contract.Node{Identity: "ipc_call:a.ts#c", Kind: "ipc_call", File: "a.ts", Name: "INVOKE names[0]", Attributes: base}
}

func TestInvokeReasons(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attrs    map[string]any
		hasEdges bool
		want     [][2]string
	}{
		{"linked", map[string]any{"status": "linked"}, true, [][2]string{}},
		{"wrapper", map[string]any{"status": "wrapper", "command": nil, "wrapper": true}, false, [][2]string{{"wrapper_inner", "-"}}},
		{"dynamic", map[string]any{"status": "dynamic", "command": nil}, false, [][2]string{{"command_unevaluated", "names[0]"}}},
		{"dynamic alternative", map[string]any{"status": "dynamic", "command": nil, "alternatives": []any{map[string]any{"command": "x", "status": "linked"}, map[string]any{"command": nil, "status": "dynamic"}}}, true,
			[][2]string{{"command_unevaluated", "names[0]"}, {"alternative_linked", "-"}}},
		{"global dynamic", map[string]any{"status": "dynamic", "command": nil, "form": "global"}, false, [][2]string{{"command_unevaluated", "names[0]"}, {"global_form", "-"}}},
		{"global unlinked", map[string]any{"status": "global_unlinked", "form": "global"}, false, [][2]string{{"global_form", "-"}, {"global_unlinked", "-"}}},
		{"registry unproven", map[string]any{"status": "registry_unproven"}, false, [][2]string{{"registry_unproven", "-"}}},
		{"acl possible", map[string]any{"status": "acl_possible"}, false, [][2]string{{"acl_possible", "-"}}},
		{"ambiguous", map[string]any{"status": "ambiguous"}, false, [][2]string{{"ambiguous", "-"}}},
		{"shadow possible", map[string]any{"status": "shadow_possible"}, false, [][2]string{{"shadow_possible", "-"}}},
		{"unknown command", map[string]any{"status": "unknown_command"}, false, [][2]string{{"unknown_command", "-"}}},
		{"test only", map[string]any{"status": "test_only"}, false, [][2]string{{"test_only", "-"}}},
		{"unmatched", map[string]any{"status": "unmatched", "command": "nope"}, false, [][2]string{{"unmatched", "nope"}}},
		{"unmatched alternative", map[string]any{"status": "unmatched", "command": nil, "alternatives": []any{map[string]any{"command": "x", "status": "linked"}, map[string]any{"command": "nope", "status": "unmatched"}}}, true,
			[][2]string{{"unmatched", "nope"}, {"alternative_linked", "-"}}},
		{"plugin", map[string]any{"status": "plugin", "plugin": "fs", "plugin_command": "read_file"}, false, [][2]string{{"plugin", "fs|read_file"}}},
		{"plugin without command", map[string]any{"status": "plugin", "plugin": "x"}, false, [][2]string{{"plugin", "x"}}},
		{"fallback", map[string]any{"status": "dynamic"}, false, [][2]string{{"status", "dynamic"}}},
		{"unknown status", map[string]any{"status": "weird"}, false, [][2]string{{"status_unknown", "weird"}}},
		{"missing status", map[string]any{}, true, [][2]string{{"status_unknown", ""}}},
		{"http-only status", map[string]any{"status": "unresolved"}, false, [][2]string{{"status_unknown", "unresolved"}}},
	} {
		got := codes(callReasons(tauriTransport, ipcCall(tc.attrs), tc.hasEdges))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
