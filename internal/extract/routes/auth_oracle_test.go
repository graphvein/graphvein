package routes_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestFastAPISecurityOracle compares the stored security_dependencies of
// the auth-fastapi fixture with FastAPI's own app.openapi() security per
// operation (scripts/oracle/fastapi_security.sh). Opt-in, never run by CI:
//
//   - GRAPHVEIN_FASTAPI_ORACLE_ROOT: the scratch copy the oracle imported;
//   - GRAPHVEIN_FASTAPI_ORACLE_OPENAPI: the oracle's JSON output.
//
// Precision: every scheme name the route states must be in the operation's
// security. Recall: every operation scheme must be stated, unless the
// route has an unknown entry or a stated scheme name is unknown. Either
// failure is a disagreement (a precision stop).
func TestFastAPISecurityOracle(t *testing.T) {
	root, out := os.Getenv("GRAPHVEIN_FASTAPI_ORACLE_ROOT"), os.Getenv("GRAPHVEIN_FASTAPI_ORACLE_OPENAPI")
	if root == "" || out == "" {
		t.Skip("set GRAPHVEIN_FASTAPI_ORACLE_ROOT and GRAPHVEIN_FASTAPI_ORACLE_OPENAPI (scripts/oracle/fastapi_security.sh)")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		FastAPI string                         `json:"fastapi"`
		Apps    map[string]map[string][]string `json:"apps"`
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := indexAuthFixture(t, root)
	operations := map[string][]string{}
	for _, ops := range oracle.Apps {
		for key, names := range ops {
			operations[key] = names
		}
	}
	agree, disagree, compared := 0, 0, map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		method, _ := n.Attributes["method"].(string)
		stated, unknown := map[string]bool{}, n.Attributes["security_dependencies_unknown"] != nil
		schemes, _ := n.Attributes["security_dependencies"].([]any)
		for _, s := range schemes {
			m, _ := s.(map[string]any)
			name, _ := m["scheme_name"].(string)
			if name == "unknown" {
				unknown = true
				continue
			}
			stated[name] = true
		}
		paths, _ := n.Attributes["paths"].([]any)
		for _, p := range paths {
			entry, _ := p.(map[string]any)
			template, _ := entry["template"].(string)
			if entry["trailing_slash"] == true {
				template += "/"
			}
			key := method + " " + template
			names, ok := operations[key]
			if !ok {
				continue
			}
			compared[key] = true
			want := map[string]bool{}
			for _, name := range names {
				want[name] = true
			}
			var problems []string
			for name := range stated {
				if !want[name] {
					problems = append(problems, "states "+name+", not in the operation")
				}
			}
			for name := range want {
				if !stated[name] && !unknown {
					problems = append(problems, "misses "+name+" with no unknown entry")
				}
			}
			sort.Strings(problems)
			if len(problems) > 0 {
				disagree++
				t.Errorf("%s (%s): %s", n.Identity, key, strings.Join(problems, "; "))
				continue
			}
			agree++
		}
	}
	for _, ops := range oracle.Apps {
		for key := range ops {
			if !compared[key] {
				t.Errorf("operation %s has no stored route", key)
			}
		}
	}
	t.Logf("fastapi %s: %d operations agree, %d disagree", oracle.FastAPI, agree, disagree)
}
