package index

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestInvalidRecordIsolation(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var s store.Store
			var err error
			if backend == "sqlite" {
				s, err = sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
			} else {
				url := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
				if url == "" {
					if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
						t.Fatal("GRAPHVEIN_TEST_POSTGRES_URL is required")
					}
					t.Skip("GRAPHVEIN_TEST_POSTGRES_URL is unset")
				}
				s, err = postgres.Open(ctx, url)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/repos/typescript")); err != nil {
				t.Fatal(err)
			}
			for _, names := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
				if err := os.Rename(filepath.Join(root, names[0]), filepath.Join(root, names[1])); err != nil {
					t.Fatal(err)
				}
			}
			opt := Options{Repository: "isolation-" + rand.Text(), GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			run := func(extract func(string, []byte, *parse.Node, string) (typescript.Facts, error)) Summary {
				t.Helper()
				var stream bytes.Buffer
				summary, err := emit(ctx, root, s, &stream, opt, extract)
				if err != nil {
					t.Fatal(err)
				}
				if err := ingest.Stream(ctx, &stream, s); err != nil {
					t.Fatal(err)
				}
				return summary
			}
			run(extractFile)
			normal, err := s.Dump(ctx, opt.Repository)
			if err != nil {
				t.Fatal(err)
			}
			for _, fault := range []string{"node", "edge", "diagnostic", "linked", "reference", "target", "call_source", "oversize"} {
				t.Run(fault, func(t *testing.T) {
					// A unique repository prevents the baseline cache masking injection.
					opt.Repository += "-" + fault
					badFile := "src/blind.ts"
					if fault == "target" || fault == "call_source" {
						badFile = "src/lib.ts"
					}
					injected := func(file string, source []byte, root *parse.Node, language string) (typescript.Facts, error) {
						out, err := extractFile(file, source, root, language)
						if file == badFile {
							switch fault {
							case "node", "target":
								out.Nodes[0].Name = ""
							case "edge":
								out.Edges[0].Confidence = 2
							case "reference":
								out.Edges[0].Target = facts.ID("function", file, "absent")
							case "call_source":
								out.Calls[0].Source = facts.ID("function", file, "absent")
							case "oversize":
								out.Nodes[0].Attributes["injected"] = strings.Repeat("x", contract.MaxRecordBytes)
							case "diagnostic":
								out.Diagnostics = append(out.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "invalid", Code: "injected", Message: "injected"})
							case "linked":
								// A web-root import keeps an external node named by
								// the specifier, which the NUL makes invalid.
								out.Imports = append(out.Imports, facts.Import{Module: "/\x00", Span: contract.Span{StartLine: 1, EndLine: 1}})
							}
						}
						return out, err
					}
					summary := run(injected)
					if summary.Dropped != 1 {
						t.Fatalf("dropped: %+v", summary)
					}
					got, err := s.Dump(ctx, opt.Repository)
					if err != nil {
						t.Fatal(err)
					}
					identities := map[string]bool{}
					for _, n := range got.Nodes {
						if n.File == badFile {
							t.Fatal("rejected node survived", n)
						}
						identities[n.Identity] = true
					}
					for _, e := range got.Edges {
						if !identities[e.Source] || !identities[e.Target] {
							t.Fatal("dangling edge", e)
						}
					}
					count := 0
					for _, d := range got.Diagnostics {
						if d.File == badFile {
							count++
							if d.Code != "extract_invalid_record" || d.Severity != "error" || !strings.Contains(d.Message, badFile) {
								t.Fatal(d)
							}
							if fault == "node" && (!strings.Contains(d.Message, "kind=function identity=function:src/blind.ts#blind") || !strings.Contains(d.Message, "minLength")) {
								t.Fatal("diagnostic lacks record or violated rule", d)
							}
						}
						if d.Code == "invalid_record" || d.Code == "invalid_reference" {
							t.Fatal(d)
						}
					}
					if count != 1 {
						t.Fatal("expected exactly one rejection diagnostic", count)
					}
					for _, f := range got.Files {
						if f.Path == badFile {
							cached, err := s.FactCache(ctx, opt.Repository, f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", Version, 2<<20))
							if err != nil || len(cached) != 0 {
								t.Fatal("rejected file cached", err)
							}
						}
					}
					if fault != "target" && fault != "call_source" {
						// The baseline is checked against the normal golden by TestRepositoryGolden.
						var want []contract.Node
						for _, n := range normal.Nodes {
							if n.File != badFile {
								want = append(want, n)
							}
						}
						if !reflect.DeepEqual(want, got.Nodes) {
							t.Fatal("other files' facts changed")
						}
						var wantEdges []contract.Edge
						for _, e := range normal.Edges {
							_, sourceFile, _, _ := contract.ParseIdentity(e.Source)
							if sourceFile != badFile {
								wantEdges = append(wantEdges, e)
							}
						}
						// The rejected file's record stays, labelled best_effort:
						// its structure claims nothing (row PL-L3's refusal).
						var wantFiles []contract.File
						for _, f := range normal.Files {
							if f.Path == badFile {
								f.Precision = "best_effort"
							}
							wantFiles = append(wantFiles, f)
						}
						if !reflect.DeepEqual(wantEdges, got.Edges) || !reflect.DeepEqual(wantFiles, got.Files) {
							t.Fatal("other files' edges or file records changed")
						}
					} else {
						found := false
						for _, d := range got.Diagnostics {
							if d.File == "src/app.tsx" && d.Code == "unresolved_import" {
								found = true
							}
						}
						if !found {
							t.Fatal("dropped target did not become unresolved")
						}
					}
					// The repaired extractor reparses just the uncached rejected file.
					repaired := run(extractFile)
					if repaired.Parsed != 1 || repaired.Dropped != 0 {
						t.Fatal(repaired)
					}
				})
			}
		})
	}
}
