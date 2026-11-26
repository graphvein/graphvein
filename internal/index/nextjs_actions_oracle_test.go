package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// oracleAction and oracleUse are the records of
// scripts/oracle/nextjs_supabase/server_actions.cjs.
type oracleAction struct {
	File  string  `json:"file"`
	Scope string  `json:"scope"`
	Name  *string `json:"name"`
	Line  int     `json:"line"`
}

type oracleUse struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Action string `json:"action"`
	Form   string `json:"form"`
	// Conditional marks an alternative of a conditional value.
	Conditional bool `json:"conditional"`
	// Boundary is the compiler module graphs' boundary of a direct call
	// (client or unknown; T024 review).
	Boundary string `json:"boundary"`
}

func (a oracleAction) key() string {
	if a.Name == nil {
		return fmt.Sprintf("%s#@%d", a.File, a.Line)
	}
	return a.File + "#" + *a.Name
}

// TestServerActionsOracle compares rows SA-M1, SA-I1 and SA-C1..SA-C3 with
// the TypeScript compiler (scripts/oracle/nextjs_supabase/server_actions.cjs):
// every action Graphvein claims must be one the compiler's reading of the
// directive prologues and export names proves, and every linked use (each
// alternative of a conditional one) must be a use the checker's alias
// resolution finds, with the same form. The
// fixture always runs; each checkout of GRAPHVEIN_ACTIONS_ORACLE_CORPORA
// (OS path list) too. GRAPHVEIN_TS_ORACLE_DIR names the TypeScript 6.0.3
// install of scripts/oracle/ts_structure/README.md; without it the test
// skips. Actions and uses the compiler finds but Graphvein refuses are
// counted, not failures.
func TestServerActionsOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TS_ORACLE_DIR")
	if dir == "" {
		t.Skip("GRAPHVEIN_TS_ORACLE_DIR is not set: install the TypeScript oracle as scripts/oracle/ts_structure/README.md says")
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	script, err := filepath.Abs("../../scripts/oracle/nextjs_supabase/server_actions.cjs")
	if err != nil {
		t.Fatal(err)
	}
	roots := [][2]string{{"fixture", actionsFixture}}
	if list := os.Getenv("GRAPHVEIN_ACTIONS_ORACLE_CORPORA"); list != "" {
		for _, p := range filepath.SplitList(list) {
			roots = append(roots, [2]string{filepath.Base(p), p})
		}
	}
	for _, r := range roots {
		t.Run(r[0], func(t *testing.T) {
			root := r[1]
			if r[0] == "fixture" {
				root = t.TempDir()
				if err := os.CopyFS(root, os.DirFS(actionsFixture)); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(node, script, root)
			cmd.Env = append(os.Environ(), "TS_DIR="+dir)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("oracle: %v\n%s", err, stderr.String())
			}
			var oracle struct {
				Actions []oracleAction `json:"actions"`
				Uses    []oracleUse    `json:"uses"`
			}
			if err := json.Unmarshal(out, &oracle); err != nil {
				t.Fatal(err)
			}
			s, opt := open(t), options(t)
			emit(t, root, s, opt)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			compareActions(t, snapshot.Nodes, oracle.Actions, oracle.Uses)
		})
	}
}

func compareActions(t *testing.T, nodes []contract.Node, actions []oracleAction, uses []oracleUse) {
	t.Helper()
	proven := map[string]bool{}
	for _, a := range actions {
		proven[a.key()] = true
	}
	keys := map[string]string{}
	claimed := map[string]bool{}
	agreeActions := 0
	for _, n := range nodes {
		if n.Kind != "server_action" {
			continue
		}
		key := n.File + "#"
		if name, ok := n.Attributes["action"].(string); ok {
			key += name
		} else {
			key += fmt.Sprintf("@%d", n.Span.StartLine)
		}
		keys[n.Identity] = key
		claimed[key] = true
		if !proven[key] {
			t.Errorf("Graphvein claims action %s, which the compiler does not prove", key)
			continue
		}
		agreeActions++
	}
	var refused []string
	for _, a := range actions {
		if !claimed[a.key()] {
			refused = append(refused, a.key())
		}
	}
	forms := map[string]string{"form_action": "form_action", "action_state": "action_state", "direct": "direct"}
	matched := map[int]bool{}
	agreeUses, boundaries, clients := 0, 0, 0
	for _, n := range nodes {
		if n.Kind != "action_call" {
			continue
		}
		form, _ := n.Attributes["form"].(string)
		var targets []string
		if target, ok := n.Attributes["action"].(string); ok {
			targets = append(targets, target)
		}
		alternatives, _ := n.Attributes["alternatives"].([]any)
		for _, a := range alternatives {
			if s, ok := a.(string); ok {
				targets = append(targets, s)
			}
		}
		conditional := n.Attributes["conditional"] == true
		for _, target := range targets {
			found := false
			oracleBoundary := ""
			for i, u := range uses {
				if !matched[i] && u.File == n.File && u.Action == keys[target] && u.Form == forms[form] && u.Conditional == conditional && u.Line >= n.Span.StartLine && u.Line <= n.Span.EndLine {
					matched[i] = true
					found = true
					oracleBoundary = u.Boundary
					break
				}
			}
			if !found {
				t.Errorf("Graphvein links %s (%s) to %s, which the checker does not confirm", n.Identity, form, keys[target])
				continue
			}
			agreeUses++
			if form == "direct" {
				boundaries++
				// A client label is a claim: the compiler's module graphs
				// must prove it (row SA-C3). unknown claims nothing.
				if n.Attributes["boundary"] == "client" && oracleBoundary != "client" {
					t.Errorf("Graphvein labels %s boundary client, the compiler module graphs do not prove it (%s)", n.Identity, oracleBoundary)
				}
				if n.Attributes["boundary"] == "client" {
					clients++
				}
			}
		}
	}
	unlinked := map[string]int{}
	for i, u := range uses {
		if !matched[i] {
			unlinked[u.Form]++
		}
	}
	sort.Strings(refused)
	t.Logf("actions: %d agree, %d compiler actions refused %v; uses: %d agree, compiler uses not linked by form %v; boundaries: %d direct calls compared, %d client labels proven by the compiler graphs", agreeActions, len(refused), refused, agreeUses, unlinked, boundaries, clients)
}
