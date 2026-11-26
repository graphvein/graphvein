//go:build graphveincorpus

package index_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/extract/openapi"
	"github.com/graphvein/graphvein/internal/scan"
	"github.com/graphvein/graphvein/internal/testscope"
)

// This gate deliberately reads the immutable census rather than rewriting it.
func TestOpenAPICorpusRevisionOne(t *testing.T) {
	type prediction struct {
		Method      string           `json:"method"`
		Path        string           `json:"path"`
		OperationID any              `json:"operation_id"`
		Paths       []map[string]any `json:"paths"`
	}
	type record struct {
		Checkout, Path string
		Confirmed      bool
		Operations     []prediction `json:"predicted_operations"`
	}
	f, err := os.Open("../../docs/contract/claims/data/openapi-census.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	census := map[string]map[string][]prediction{}
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 4096), 2<<20)
	for lines.Scan() {
		var r record
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Confirmed {
			if census[r.Checkout] == nil {
				census[r.Checkout] = map[string][]prediction{}
			}
			census[r.Checkout][r.Path] = r.Operations
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"react", "realworld", "express", "fastapi", "axum", "axum-ddd", "angular", "langflow", "self"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("../../spike/parsing/.corpus", name)
			if name == "self" {
				root = "../.."
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("required corpus checkout unavailable: %v", err)
			}
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			scanned, err := scan.Walk(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			sources := map[string][]byte{}
			for _, f := range scanned.Files {
				sources[f.Record.Path] = f.Source
			}
			nodes, _, ds := openapi.Link(sources, cfg.OpenAPI, cfg.Tests)
			for _, d := range ds {
				if d.Code == "openapi_coverage" {
					continue
				}
				if name != "self" || !selfTestData(d.File, cfg) {
					t.Errorf("R4 unexpected diagnostic: %+v", d)
				}
			}
			specs, ops := map[string]bool{}, map[string]prediction{}
			selfFixture := false
			for _, n := range nodes {
				if name == "self" {
					if !selfTestData(n.File, cfg) || n.Attributes["scope"] != "test" {
						t.Errorf("R4 unexpected self node: %+v", n)
					}
					if strings.HasPrefix(n.File, "testdata/fixtures/openapi/") {
						selfFixture = true
					}
					continue
				}
				if n.Attributes["scope"] != nil {
					t.Errorf("R4 corpus node is test-scope: %s", n.Identity)
				}
				if n.Kind == "api_spec" {
					specs[n.File] = true
					continue
				}
				paths := routePaths(t, n)
				// The immutable T013a census describes declaration metadata only.
				for _, p := range paths {
					for _, key := range []string{"status", "match", "candidates", "candidate_count"} {
						delete(p, key)
					}
				}
				ops[n.File+"#"+n.QualifiedName] = prediction{n.Attributes["method"].(string), n.Attributes["path"].(string), n.Attributes["operation_id"], paths}
				if name == "realworld" && n.QualifiedName == "GET /articles" {
					data, _ := json.Marshal(n.Attributes["parameters"])
					var params []map[string]any
					json.Unmarshal(data, &params)
					found := map[string]bool{}
					for _, p := range params {
						if p["in"] == "query" && p["required"] == false {
							found[p["name"].(string)] = true
						}
					}
					if !found["offset"] || !found["limit"] {
						t.Error("C2 component parameter resolution failed")
					}
				}
			}
			if name == "self" {
				if !selfFixture {
					t.Fatal("self fixture absent: no API node from testdata/fixtures/openapi/")
				}
				t.Logf("self: API nodes=%d diagnostics=%d", len(nodes), len(ds))
				return
			}
			wantOps := 0
			for file, predictions := range census[name] {
				if !specs[file] {
					t.Errorf("R4 missing spec %s", file)
				}
				for _, want := range predictions {
					wantOps++
					// R4 corrects the immutable census's omitted slash flags.
					if strings.HasSuffix(want.Path, "/") {
						for _, p := range want.Paths {
							if p["template"] != "/" {
								p["trailing_slash"] = true
							}
						}
					}
					got, ok := ops[file+"#"+want.Method+" "+want.Path]
					if !ok || !reflect.DeepEqual(got, want) {
						t.Errorf("R4 %s %s: got %+v want %+v", want.Method, want.Path, got, want)
					}
				}
			}
			if len(specs) != len(census[name]) || len(ops) != wantOps {
				t.Errorf("R4 counts: specs=%d/%d ops=%d/%d", len(specs), len(census[name]), len(ops), wantOps)
			}
			if name == "react" || name == "realworld" || name == "express" {
				data, err := os.ReadFile("../../testdata/corpus/realworld-api.txt")
				if err != nil {
					t.Fatal(err)
				}
				want, got := map[string]bool{}, map[string]bool{}
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						want[line] = true
					}
				}
				for _, op := range ops {
					for _, p := range op.Paths {
						got[op.Method+" "+p["template"].(string)] = true
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Error("operation templates differ from realworld-api.txt")
				}
			}
			t.Logf("%s: specs=%d ops=%d diagnostics=%d", name, len(specs), len(ops), len(ds))
		})
	}
}

// selfTestData reports whether a file in Graphvein's own tree may carry
// OpenAPI facts in the "self" case. Graphvein ships no API description of its
// own, so every spec it indexes in itself must be a committed test input:
// fixture specs and the generated OpenAPI documents stored as goldens. Those
// live only in the top-level testdata/ tree or in a package's testdata/
// directory under internal/ or cmd/, and the test-scope rules must classify
// them as test code. Anything else (a spec under docs/, web/, scripts/ or a
// package's own sources, or one leaking in from the ignored corpus checkouts)
// is a real finding.
func selfTestData(file string, cfg config.Config) bool {
	if testscope.Classify(file, cfg.Tests) != testscope.Test {
		return false
	}
	if strings.HasPrefix(file, "testdata/") {
		return true
	}
	parts := strings.Split(file, "/")
	if parts[0] != "internal" && parts[0] != "cmd" {
		return false
	}
	for _, p := range parts[1 : len(parts)-1] {
		if p == "testdata" {
			return true
		}
	}
	return false
}
