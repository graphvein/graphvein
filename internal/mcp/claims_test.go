package mcp

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const claimsFile = "../../docs/contract/claims/mcp-server.md"

// TestClaimContract keeps claimRows and the public claim table 1:1: the
// same rows in the same order, each with the same part.
func TestClaimContract(t *testing.T) {
	data, err := os.ReadFile(claimsFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc []claimRow
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| MC-") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) != 7 {
			t.Errorf("row %q has %d cells, want 5 columns", cells[1], len(cells)-2)
			continue
		}
		doc = append(doc, claimRow{ID: strings.TrimSpace(cells[1]), Part: strings.TrimSpace(cells[2])})
	}
	if !reflect.DeepEqual(doc, claimRows) {
		t.Errorf("claim table rows\n%v\ncode rows\n%v", doc, claimRows)
	}
	if last := claimRows[len(claimRows)-1]; last.ID != "MC-D" {
		t.Errorf("the default row must be last, not %s", last.ID)
	}
}

// TestPaging checks MC-4's limits and the "more" sentence.
func TestPaging(t *testing.T) {
	for _, tc := range []struct {
		limit  int
		cursor string
		ok     bool
	}{{0, "", true}, {MaxLimit, "", true}, {MaxLimit + 1, "", false}, {-1, "", false}, {1, "x", false}, {1, "-2", false}, {1, "7", true}} {
		if _, _, err := pageParams(tc.limit, tc.cursor); (err == nil) != tc.ok {
			t.Errorf("pageParams(%d, %q) = %v, want ok %v", tc.limit, tc.cursor, err, tc.ok)
		}
	}
	e := newEnvelope("probe")
	got := page(e, []int{1, 2, 3, 4, 5}, 1, 2)
	if !reflect.DeepEqual(got, []int{2, 3}) || e.Page.More != 2 || e.Page.NextCursor != "3" || e.Page.Total != 5 || !strings.HasPrefix(e.Page.Text, "2 more not shown") {
		t.Errorf("page = %v %+v", got, e.Page)
	}
	e = newEnvelope("probe")
	if got := page(e, []int(nil), 0, 50); got == nil || e.Page.More != 0 || e.Page.Text != "" {
		t.Errorf("empty page = %v %+v", got, e.Page)
	}
}

// TestStatusSettings is row MC-33's mapping: an unchanged comparison is
// reported without an unknown; changed and unknown comparisons are
// unknowns (never restated as unchanged), with the keys and the reason;
// project mode prefixes the member.
func TestStatusSettings(t *testing.T) {
	reason := "the index run did not record its settings"
	cases := []struct {
		view  settingsView
		who   string
		kind  string
		text  string
		state string
	}{
		{settingsView{State: "unchanged"}, "", "", "", "unchanged"},
		{settingsView{State: "not_indexed"}, "", "", "", "not_indexed"},
		{settingsView{State: "changed", ChangedKeys: []string{"index.shape_depth", "tests.include"}}, "", "settings_changed", "graphvein.yaml changed an index setting since the last index run (index.shape_depth, tests.include): facts may differ after a re-index", "changed"},
		{settingsView{State: "unknown", Reason: &reason}, "member backend: ", "settings_unknown", "member backend: whether the index was built with the current graphvein.yaml settings is not known: " + reason, "unknown"},
		{settingsView{State: "later_state"}, "", "settings_unknown", "whether the index was built with the current graphvein.yaml settings is not known: no reason given", "later_state"},
	}
	for _, c := range cases {
		e := newEnvelope("status")
		out := settingsResult(e, &c.view, c.who)
		if out["state"] != c.state {
			t.Errorf("%s: state %v", c.view.State, out["state"])
		}
		if keys, ok := out["changed_keys"].([]string); !ok || keys == nil {
			t.Errorf("%s: changed_keys %v", c.view.State, out["changed_keys"])
		}
		if c.kind == "" {
			if len(e.Unknowns) != 0 {
				t.Errorf("%s: unknowns %+v", c.view.State, e.Unknowns)
			}
			continue
		}
		if len(e.Unknowns) != 1 || e.Unknowns[0].Kind != c.kind || e.Unknowns[0].Text != c.text || e.Unknowns[0].Row != "MC-33" {
			t.Errorf("%s: unknowns %+v", c.view.State, e.Unknowns)
		}
	}
}
