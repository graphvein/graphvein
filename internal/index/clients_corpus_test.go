//go:build graphveincorpus

package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

func copyClientCorpus(t *testing.T, source, dest string) {
	t.Helper()
	err := filepath.WalkDir(source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mappedIdentity(identity, prefix string) string {
	kind, file, name, err := contract.ParseIdentity(identity)
	if err != nil || file == "." {
		return identity
	}
	if kind == "file" {
		name = prefix + "/" + name
	}
	id, err := contract.Identity(kind, prefix+"/"+file, name)
	if err != nil {
		panic(err)
	}
	return id
}

func mapCorpusValue(v any, prefix string) any {
	switch value := v.(type) {
	case string:
		return mappedIdentity(value, prefix)
	case map[string]any:
		for k, v := range value {
			if k == "git_blob_oid" {
				delete(value, k)
				continue
			}
			// "path" is a file path only in a file record; an
			// api_operation's "path" attribute is an OpenAPI template.
			if (k == "file" || k == "path" && value["type"] == "file") && v != "" && v != "." {
				if s, ok := v.(string); ok {
					value[k] = prefix + "/" + s
					continue
				}
			}
			value[k] = mapCorpusValue(v, prefix)
		}
		if attrs, ok := value["attributes"].(map[string]any); ok && (value["kind"] == "migration" || value["kind"] == "table") {
			// Schema records name the migration set folder and the package
			// manifest by repository-relative path (docs/extract/schema.md).
			for _, k := range []string{"set", "package"} {
				if s, ok := attrs[k].(string); ok && s != "" {
					attrs[k] = prefix + "/" + s
				}
			}
		}
		if value["kind"] == "file" {
			value["qualified_name"] = prefix + "/" + value["qualified_name"].(string)
		}
		return value
	case []any:
		for i, v := range value {
			value[i] = mapCorpusValue(v, prefix)
		}
		return value
	}
	return v
}

func compositeRecords(t *testing.T, s store.Snapshot, prefix string) (map[string]bool, map[string]bool) {
	t.Helper()
	records, clients := map[string]bool{}, map[string]bool{}
	diagnosticCounts := map[string]int{}
	add := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if prefix != "" {
			value = mapCorpusValue(value, prefix)
		} else if m, ok := value.(map[string]any); ok {
			delete(m, "git_blob_oid")
		}
		data, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		key := string(data)
		if m, ok := value.(map[string]any); ok && m["type"] == "diagnostic" {
			diagnosticCounts[key]++
			key += fmt.Sprintf("@%d", diagnosticCounts[key])
		}
		records[key] = true
	}
	for _, f := range s.Files {
		add(f)
	}
	for _, n := range s.Nodes {
		// Client calls and OpenAPI operations carry link results
		// (statuses, candidates) that cross the frontend/backend boundary
		// only in the composite (ADRs 0018, 0024), so only their identities
		// are compared.
		if n.Kind == "client_call" || n.Kind == "api_operation" {
			id := n.Identity
			if prefix != "" {
				id = mappedIdentity(id, prefix)
			}
			clients[id] = true
			continue
		}
		add(n)
	}
	for _, e := range s.Edges {
		if e.Kind == "calls_route" || e.Kind == "documents_route" || e.Kind == "contains" && (strings.HasPrefix(e.Target, "client_call:") || strings.HasPrefix(e.Target, "api_operation:")) {
			continue
		}
		add(e)
	}
	for _, d := range s.Diagnostics {
		if d.Code == "openapi_coverage" {
			// Counts of the documents_route outcomes above.
			continue
		}
		if prefix == "" {
			// Messages name repository-relative paths, which the
			// composite prefixes; file fields are compared as mapped.
			d.Message = strings.NewReplacer("frontend/", "", "backend/", "").Replace(d.Message)
		}
		add(d)
	}
	return records, clients
}

func TestClientComposites(t *testing.T) {
	react := verifyRouteCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	frontend := summarizeRouteCorpus(t, react, "react")
	for _, entry := range []struct{ name, pin string }{{"express", "94a864e4e458a5e0ae2b07d3cf762791769e8690"}, {"axum", "f1b25654773228297e35c292f357d33b7121a101"}} {
		t.Run(entry.name, func(t *testing.T) {
			backendRoot := verifyRouteCorpus(t, entry.name, entry.pin)
			backend := summarizeRouteCorpus(t, backendRoot, entry.name)
			root := t.TempDir()
			copyClientCorpus(t, react, filepath.Join(root, "frontend"))
			copyClientCorpus(t, backendRoot, filepath.Join(root, "backend"))
			composite := summarizeRouteCorpus(t, root, "realworld-react-"+entry.name)
			want, clientWant := compositeRecords(t, frontend, "frontend")
			second, clientSecond := compositeRecords(t, backend, "backend")
			for k := range second {
				want[k] = true
			}
			for k := range clientSecond {
				clientWant[k] = true
			}
			got, clientGot := compositeRecords(t, composite, "")
			if !reflect.DeepEqual(want, got) {
				for k := range want {
					if !got[k] {
						t.Errorf("missing union record %s", k)
					}
				}
				for k := range got {
					if !want[k] {
						t.Errorf("extra composite record %s", k)
					}
				}
			}
			if !reflect.DeepEqual(clientWant, clientGot) {
				t.Error("client identity sets differ")
			}
			for _, n := range composite.Nodes {
				if n.Kind == "client_call" && strings.HasPrefix(n.File, "frontend/") {
					t.Logf("React call %s: %s (opaque URL/Request forwarding)", n.Identity, n.Attributes["status"])
				}
			}
		})
	}
}

func TestLangflowClientSample(t *testing.T) {
	root := verifyRouteCorpus(t, "langflow", "504c02fc47e76087b82b0e7cbe4186e9cdd916d4")
	s := open(t)
	_, summary, _ := emit(t, root, s, options(t))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	sites := map[string][]contract.Node{}
	counts := map[string]int{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
		sites[key] = append(sites[key], n)
		for _, attr := range []string{"status", "base", "library"} {
			counts[attr+":"+fmt.Sprint(n.Attributes[attr])]++
		}
	}
	links := map[string][]string{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			links[e.Source] = append(links[e.Source], e.Target)
		}
	}
	data, err := os.ReadFile("../../testdata/corpus/langflow-calls-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	records := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		records++
		parts := strings.SplitN(line, " | ", 4)
		if len(parts) != 4 {
			t.Fatal("bad sample", line)
		}
		found := sites[parts[0]]
		if len(found) != 1 {
			t.Errorf("%s: expected one client, got %d", parts[0], len(found))
			continue
		}
		n := found[0]
		if n.Attributes["status"] != parts[1] {
			t.Errorf("%s: status %s, want %s", parts[0], n.Attributes["status"], parts[1])
		}
		real := strings.TrimPrefix(parts[3], "real: ")
		known := !strings.HasPrefix(real, "unknown:")
		allowed := map[string]bool{}
		if known {
			for _, id := range strings.Split(real, " | ") {
				allowed[id] = true
			}
		}
		var actualEntries []string
		for _, value := range n.Attributes["paths"].([]any) {
			p := value.(map[string]any)
			ids := []string{}
			for _, id := range p["candidates"].([]any) {
				ids = append(ids, id.(string))
			}
			sort.Strings(ids)
			actual := strings.Join(ids, ",")
			if actual == "" {
				actual = "-"
			}
			status := p["status"].(string)
			actualEntries = append(actualEntries, status+":"+actual)
			if status == "linked" {
				for _, id := range ids {
					if known && !allowed[id] {
						t.Errorf("unsafe linked entry %s -> %s", parts[0], id)
					}
				}
			}
		}
		if actual := strings.Join(actualEntries, "; "); actual != parts[2] {
			t.Errorf("%s: entries %s, want %s", parts[0], actual, parts[2])
		}
		outOfScope := map[string]string{"src/frontend/src/controllers/API/queries/files/use-download-files.ts:19": "B7 single dynamic segment cannot prove two path segments", "src/frontend/src/controllers/API/queries/memories/use-get-memory-messages.ts:86": "B1/B5 URL object and toString unsupported", "src/frontend/src/utils/buildUtils.ts:336": "B2 call-site let"}
		if rule, excluded := outOfScope[parts[0]]; excluded {
			t.Logf("out of scope %s: %s", parts[0], rule)
		} else if known {
			seen := map[string]bool{}
			for _, id := range links[n.Identity] {
				seen[id] = true
			}
			if n.Attributes["status"] != "linked" {
				t.Errorf("in-scope known site did not link: %s", parts[0])
			}
			for id := range allowed {
				if !seen[id] {
					t.Errorf("missing real target %s -> %s", parts[0], id)
				}
			}
		}
		_, ln, _ := strings.Cut(parts[0][strings.LastIndex(parts[0], ":"):], ":")
		if _, err := strconv.Atoi(ln); err != nil {
			t.Fatal(line)
		}
	}
	if records != 41 {
		t.Fatal("sample/witness count", records)
	}
	t.Logf("Langflow clients %v; parsed=%d reused=%d", counts, summary.Parsed, summary.Reused)
}
