package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

func uniqueNodes(t *testing.T, data []byte) []any {
	t.Helper()
	seen := map[string]bool{}
	var records []any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var record map[string]any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["type"] == "node" {
			identity := record["identity"].(string)
			if seen[identity] {
				t.Fatalf("duplicate emitted node %s", identity)
			}
			seen[identity] = true
		}
		records = append(records, record)
	}
	return records
}

func TestLanguageFixtureUniqueNodes(t *testing.T) {
	for _, language := range []string{"typescript", "identities", "go", "python", "rust"} {
		t.Run(language, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/repos/"+language)); err != nil {
				t.Fatal(err)
			}
			if language == "typescript" {
				for _, names := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
					if err := os.Rename(filepath.Join(root, names[0]), filepath.Join(root, names[1])); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := open(t)
			opt := options(t)
			opt.Parser.Workers = 1
			data, summary, _ := emit(t, root, s, opt)
			for _, workers := range []int{2, 4} {
				opt.Parser.Workers = workers
				again, parsed, _ := emit(t, root, open(t), opt)
				if !bytes.Equal(data, again) || parsed.Parsed != summary.Parsed {
					t.Fatalf("stream differs at %d workers", workers)
				}
			}
			records := uniqueNodes(t, data)
			if language != "identities" {
				return
			}
			golden.Check(t, "../../testdata/repos/identities.golden.json", records)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			names := map[string]bool{}
			for _, node := range snapshot.Nodes {
				names[node.Identity] = true
			}
			for _, identity := range []string{
				"method:owners.ts#first.save", "method:owners.ts#second.save", "method:owners.ts#third.save",
				"method:owners.ts#first.nested.leaf.save", "method:owners.ts#first.save%402", "method:owners.ts#save",
				"method:owners.ts#factory.local.save", "method:owners.ts#factory.save",
				"function:callbacks.tsx#tests.helper", "function:callbacks.tsx#tests.helper%402",
				"function:callbacks.tsx#tests.mock", "function:callbacks.tsx#tests.mock%402",
				"function:callbacks.tsx#hooks.visit", "function:callbacks.tsx#hooks.visit%402",
				"function:modules.mjs#run.helper", "function:modules.mjs#run.helper%402",
			} {
				if !names[identity] {
					t.Error("missing", identity)
				}
			}
			calls := map[string]bool{}
			for _, edge := range snapshot.Edges {
				if !names[edge.Source] || !names[edge.Target] {
					t.Fatal("dangling endpoint", edge)
				}
				if edge.Kind == "calls" {
					if edge.Target != "function:callbacks.tsx#unique" {
						t.Fatal("ambiguous helper call resolved", edge)
					}
					calls[edge.Source] = true
				}
			}
			for _, caller := range []string{"function:callbacks.tsx#tests.helper", "function:callbacks.tsx#tests.helper%402"} {
				if !calls[caller] {
					t.Error("missing uniquely resolved call from", caller)
				}
			}
			again, warm, writes := emit(t, root, s, opt)
			if !bytes.Equal(data, again) || warm.Parsed != 0 || warm.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
				t.Fatal("warm fixture changed", warm, writes)
			}
		})
	}
}
