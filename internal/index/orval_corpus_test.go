//go:build graphveincorpus

package index_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestOrvalCorpusPredictions(t *testing.T) {
	type site struct {
		File, Classification, Operation string
		Line                            int
	}
	read := func(name string, each func([]byte)) {
		t.Helper()
		f, err := os.Open("../../docs/contract/claims/data/" + name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			each(scanner.Bytes())
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}
	sites := map[string]site{}
	intents := map[string]string{}
	intentFile, err := os.Open("../../testdata/corpus/realworld-react-calls.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer intentFile.Close()
	intentPattern := regexp.MustCompile(`^([A-Z]+) (\S+) <- (\S+) `)
	scanner := bufio.NewScanner(intentFile)
	for scanner.Scan() {
		if match := intentPattern.FindStringSubmatch(scanner.Text()); match != nil {
			intents[match[3]] = match[1] + " " + match[2]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(intents) != 27 {
		t.Fatal("intent inventory count", len(intents))
	}
	read("orval-census.jsonl", func(line []byte) {
		var s site
		if err := json.Unmarshal(line, &s); err != nil {
			t.Fatal(err)
		}
		if s.Classification == "direct" || s.Classification == "query_options" || s.Classification == "hook" {
			sites[fmt.Sprintf("%s:%d", s.File, s.Line)] = s
		}
	})
	targets := map[string]map[string]string{}
	read("orval-composites-r1.jsonl", func(line []byte) {
		var p struct {
			File, Backend, Target string
			Line                  int
		}
		if err := json.Unmarshal(line, &p); err != nil {
			t.Fatal(err)
		}
		if targets[p.Backend] == nil {
			targets[p.Backend] = map[string]string{}
		}
		targets[p.Backend][fmt.Sprintf("%s:%d", p.File, p.Line)] = p.Target
	})
	react := verifyRouteCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	for _, entry := range []struct{ name, pin string }{{"", ""}, {"express", "94a864e4e458a5e0ae2b07d3cf762791769e8690"}, {"axum", "f1b25654773228297e35c292f357d33b7121a101"}, {"axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2"}} {
		name := entry.name
		if name == "" {
			name = "react"
		}
		t.Run(name, func(t *testing.T) {
			root, prefix := react, ""
			if entry.name != "" {
				root = t.TempDir()
				prefix = "frontend/"
				copyClientCorpus(t, react, filepath.Join(root, "frontend"))
				copyClientCorpus(t, verifyRouteCorpus(t, entry.name, entry.pin), filepath.Join(root, "backend"))
			}
			s, opt := open(t), options(t)
			data, _, _ := emit(t, root, s, opt)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			edges := map[string]map[string][]contract.Edge{}
			for _, e := range snapshot.Edges {
				if edges[e.Source] == nil {
					edges[e.Source] = map[string][]contract.Edge{}
				}
				edges[e.Source][e.Kind] = append(edges[e.Source][e.Kind], e)
			}
			count := 0
			operations := map[string]bool{}
			kinds := map[string]int{}
			for _, n := range snapshot.Nodes {
				if n.Kind != "client_call" || n.Attributes["library"] != "orval" {
					continue
				}
				count++
				key := fmt.Sprintf("%s:%d", strings.TrimPrefix(n.File, prefix), n.Span.StartLine)
				intent, ok := sites[key]
				if !ok {
					t.Fatal("unexpected Orval call", n.Identity)
				}
				operation := intent.Operation
				if prefix != "" {
					operation = mappedIdentity(operation, "frontend")
				}
				if n.Attributes["operation"] != operation || n.Attributes["invocation"] != intent.Classification {
					t.Fatal("intent mismatch", key, n.Attributes)
				}
				kinds[intent.Classification]++
				operations[operation] = true
				opEdges := edges[n.Identity]["calls_operation"]
				if len(opEdges) != 1 || opEdges[0].Target != operation || opEdges[0].Confidence != .9 || opEdges[0].Provenance != "inferred" {
					t.Fatal("operation edge", n.Identity, opEdges)
				}
				paths := n.Attributes["paths"].([]any)
				if len(paths) != 1 {
					t.Fatal(n)
				}
				path := paths[0].(map[string]any)
				pieces := strings.SplitN(intents[key], " ", 2)
				if len(pieces) != 2 || n.Attributes["method"] != pieces[0] || path["template"] != pieces[1] {
					t.Fatal("method or template differs from intent", n)
				}
				if path["origin"] != nil || path["origin_resolution"] != "unresolved" || path["resolution"] != "generated" {
					t.Fatal("transport path", n)
				}
				routeEdges := edges[n.Identity]["calls_route"]
				if entry.name == "" {
					if len(routeEdges) != 0 || n.Attributes["status"] != "unmatched" {
						t.Fatal("standalone route result", n)
					}
				} else {
					if len(routeEdges) != 1 || routeEdges[0].Target != targets[entry.name][key] || routeEdges[0].Confidence != .7 || len(routeEdges[0].Evidence) < 2 || path["match"] != "exact" || n.Attributes["status"] != "linked" || path["status"] != "linked" {
						t.Fatal("composite prediction mismatch", key, routeEdges, path)
					}
				}
			}
			if count != 27 || len(operations) != 19 || kinds["direct"] != 18 || kinds["query_options"] != 9 || kinds["hook"] != 0 {
				t.Fatal(count, len(operations), kinds)
			}
			for _, d := range snapshot.Diagnostics {
				if d.Code == "generated_client_model_unproven" || d.Code == "generated_client_transport_unproven" {
					t.Fatal(d)
				}
			}
			again, warm, writes := emit(t, root, s, opt)
			if string(data) != string(again) || warm.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
				t.Fatal("corpus replay", warm, writes)
			}
		})
	}
}
