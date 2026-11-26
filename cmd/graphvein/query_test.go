package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/graphvein/graphvein/internal/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestCLIQueryGoldens(t *testing.T) {
	var cases []struct{ Language, Term, Target, Ambiguous, File string }
	data, err := os.ReadFile("../../testdata/repos/cli-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Language, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/repos/"+tc.Language)); err != nil {
				t.Fatal(err)
			}
			if tc.Language == "typescript" {
				for _, p := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
					if err := os.Rename(filepath.Join(root, p[0]), filepath.Join(root, p[1])); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
			t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
			var stdout, stderr bytes.Buffer
			if code := run([]string{"index", root, "--repository", "fixture"}, &stdout, &stderr); code != 0 {
				t.Fatal(code, stderr.String())
			}
			var outputs []query.Response
			fileTarget, _ := contract.Identity("file", tc.File, tc.File)
			for _, tc := range []struct {
				command, term string
				depth, code   string
			}{{"query", tc.Term, "", "0"}, {"callers", tc.Target, "2", "0"}, {"impact", tc.Target, "3", "0"}, {"impact", tc.Ambiguous, "3", "1"}, {"impact", fileTarget, "3", "0"}, {"callers", "absent", "1", "1"}} {
				stdout.Reset()
				stderr.Reset()
				args := []string{tc.command, tc.term, root, "--repository", "fixture", "--json"}
				if tc.depth != "" {
					args = append(args, "--depth", tc.depth)
				}
				code := run(args, &stdout, &stderr)
				if string(rune('0'+code)) != tc.code || stderr.Len() != 0 {
					t.Fatal(code, stderr.String())
				}
				var out query.Response
				if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
					t.Fatal(err, stdout.String())
				}
				outputs = append(outputs, out)
			}
			golden.CheckRoot(t, "../../testdata/repos/"+tc.Language+".cli.golden.json", outputs, root)
			stdout.Reset()
			stderr.Reset()
			if code := run([]string{"impact", tc.Target, root, "--depth", "3", "--repository", "fixture"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Blind spot:") || !strings.HasPrefix(stdout.String(), "0 files changed") {
				t.Fatal(code, stdout.String(), stderr.String())
			}
			// A query opening the index does not publish runs or cache entries.
			s, err := sqlite.OpenRead(context.Background(), os.Getenv("GRAPHVEIN_SQLITE_PATH"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			runs, err := s.Runs(context.Background(), "fixture")
			if err != nil || len(runs) != 1 {
				t.Fatal(runs, err)
			}
			if err := s.SaveFactCache(context.Background(), "fixture", "x", "h", "v", []byte("x")); err == nil {
				t.Fatal("read-only connection accepted write")
			}
		})
	}
}

func TestQueryUsageAndMissingStore(t *testing.T) {
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	root := t.TempDir()
	p := filepath.Join(root, "absent", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", p)
	for _, args := range [][]string{{"query"}, {"query", "x", "--limit", "0"}, {"impact", "x", "--depth", "-1"}, {"callers", "x", "--bogus"}, {"query", "x", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Fatal(args, code, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"query", "x", root}, &out, &errOut); code != 1 {
		t.Fatal(code, errOut.String())
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Fatal("query created directory", err)
	}
}
