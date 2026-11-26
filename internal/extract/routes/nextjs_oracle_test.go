package routes_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
)

// routeFile matches an App Router route handler file with a default page
// extension.
var routeFile = regexp.MustCompile(`(^|/)route\.(tsx|ts|jsx|js)$`)

// appRouteFiles lists route files under every app directory below root
// (app/ or src/app/), relative to that app directory.
func appRouteFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !routeFile.MatchString(rel) {
			return nil
		}
		for _, marker := range []string{"src/app/", "app/"} {
			if i := strings.Index("/"+rel, "/"+marker); i >= 0 {
				out = append(out, rel[i+len(marker):])
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestNextRoutesOracle compares row NX-R1's templates with Next.js's own
// path helpers (scripts/oracle/nextjs_supabase/next_paths.cjs) on the
// fixture's route files and, opt-in, on each checkout listed in
// GRAPHVEIN_NEXT_ORACLE_CORPORA (OS path list). GRAPHVEIN_NEXT_ORACLE_DIST
// names an extracted `next` package's dist directory (npm pack next@<v>,
// read before use); without it the test skips. Forms Graphvein refuses are
// counted, not compared; any disagreement on a read form fails.
func TestNextRoutesOracle(t *testing.T) {
	dist := os.Getenv("GRAPHVEIN_NEXT_ORACLE_DIST")
	if dist == "" {
		t.Skip("GRAPHVEIN_NEXT_ORACLE_DIST is not set: extract a next package as scripts/oracle/nextjs_supabase/README.md says")
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	roots := map[string]string{"fixture": "../../../testdata/fixtures/nextjs-routes"}
	if list := os.Getenv("GRAPHVEIN_NEXT_ORACLE_CORPORA"); list != "" {
		for _, p := range filepath.SplitList(list) {
			roots[filepath.Base(p)] = p
		}
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			files := appRouteFiles(t, root)
			cmd := exec.Command(node, "../../../scripts/oracle/nextjs_supabase/next_paths.cjs", dist)
			cmd.Stdin = strings.NewReader(strings.Join(files, "\n") + "\n")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("oracle: %v: %s", err, stderr.String())
			}
			agree, refused, private := 0, 0, 0
			scanner := bufio.NewScanner(bytes.NewReader(out))
			for scanner.Scan() {
				var o struct {
					Path, Template, Error string
					Optional, Private     bool
				}
				if err := json.Unmarshal(scanner.Bytes(), &o); err != nil {
					t.Fatal(err)
				}
				dirs := strings.Split(o.Path, "/")
				template, optional, refusal, isPrivate := routes.NextAppTemplate(dirs[:len(dirs)-1])
				switch {
				case isPrivate || o.Private:
					private++
					if isPrivate != o.Private {
						t.Errorf("%s: private folder: graphvein %t, next %t", o.Path, isPrivate, o.Private)
					}
				case refusal != "":
					refused++
				case o.Error != "":
					t.Errorf("%s: graphvein reads %s, next refuses: %s", o.Path, template, o.Error)
				case template != o.Template || optional != o.Optional:
					t.Errorf("%s: graphvein %s (optional %t), next %s (optional %t)", o.Path, template, optional, o.Template, o.Optional)
				default:
					agree++
				}
			}
			t.Logf("%s: %d route files, %d agree, %d refused by Graphvein (not compared), %d private", name, len(files), agree, refused, private)
		})
	}
}

// rewriteCases are the literal rewrite rules of the fixture's proxy
// project (with its base path) and a few edge forms, each with request
// paths of literal segments.
var rewriteCases = []struct {
	source, destination string
	paths               []string
}{
	{"/proxy/old/:path*", "/proxy/api/new/:path*", []string{"/proxy/old/a/b", "/proxy/old", "/proxy/OLD/a", "/proxy/old/a/", "/old/a/b"}},
	{"/proxy/ext/:path*", "https://ext.example.com/:path*", []string{"/proxy/ext/x", "/proxy/ext"}},
	{"/proxy/odd/:id", "/proxy/api/v-:id", []string{"/proxy/odd/1", "/proxy/odd"}},
	{"/proxy/hop/:p", "/proxy/api/items/:p", []string{"/proxy/hop/1", "/proxy/hop/1/2"}},
	{"/proxy/api/static", "/proxy/api/dest", []string{"/proxy/api/static", "/proxy/api/static/x"}},
	{"/proxy/api/items/:id", "/proxy/api/things/:id", []string{"/proxy/api/items/7", "/proxy/api/items"}},
	{"/proxy/api/:path*", "/proxy/api/fallback/:path*", []string{"/proxy/api/missing", "/proxy/api", "/proxy/api/a/b/c"}},
	{"/api/legacy/:path*", "https://legacy.example.com/:path*", []string{"/api/legacy/old", "/api/legacy"}},
	{"/a/:x?", "/b/:x?", []string{"/a", "/a/1", "/a/1/2"}},
	{"/a/:x+", "/b/:x+", []string{"/a", "/a/1", "/a/1/2"}},
	{"/a/:x", "/b/:x*", []string{"/a/1"}},
	{"/a/:x*/z/:y", "/b/:y/:x*", []string{"/a/z/1", "/a/p/q/z/1", "/a/z/z/1"}},
	{"/a/:x?", "/b/:x", []string{"/a"}},
	{"/a/:x+", "/b/:x", []string{"/a/1/2"}},
	{"/a", "/b?q=1", []string{"/a"}},
	{"/a", "//host/b", []string{"/a"}},
	{"/", "/home", []string{"/", "/x"}},
}

// TestNextRewritesOracle compares row NX-L3's rewrite reading
// (routes.NextRewrite) with Next.js's own getPathMatch and
// prepareDestination (scripts/oracle/nextjs_supabase/next_rewrites.cjs),
// opt-in like TestNextRoutesOracle. A rewrite Graphvein applies must
// match Next.js's path; a source Graphvein reads as not matching must not
// match in Next.js; unsure and unmapped readings are counted, not
// compared.
func TestNextRewritesOracle(t *testing.T) {
	dist := os.Getenv("GRAPHVEIN_NEXT_ORACLE_DIST")
	if dist == "" {
		t.Skip("GRAPHVEIN_NEXT_ORACLE_DIST is not set: extract a next package as scripts/oracle/nextjs_supabase/README.md says")
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	var input bytes.Buffer
	for _, c := range rewriteCases {
		for _, p := range c.paths {
			line, err := json.Marshal(map[string]string{"source": c.source, "destination": c.destination, "path": p})
			if err != nil {
				t.Fatal(err)
			}
			input.Write(append(line, '\n'))
		}
	}
	cmd := exec.Command(node, "../../../scripts/oracle/nextjs_supabase/next_rewrites.cjs", dist)
	cmd.Stdin = &input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("oracle: %v: %s", err, stderr.String())
	}
	agree, counted := 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var o struct {
			Source, Destination, Path, Pathname, Error string
			Matched, External                          bool
		}
		if err := json.Unmarshal(scanner.Bytes(), &o); err != nil {
			t.Fatal(err)
		}
		segments := strings.FieldsFunc(o.Path, func(r rune) bool { return r == '/' })
		got, outcome := routes.NextRewrite(o.Source, o.Destination, segments, strings.HasSuffix(o.Path, "/") && o.Path != "/")
		name := o.Source + " -> " + o.Destination + " on " + o.Path
		switch outcome {
		case routes.RewriteNoMatch:
			if o.Matched {
				t.Errorf("%s: graphvein reads no match, next matches", name)
				continue
			}
		case routes.RewriteApplied:
			want := strings.TrimSuffix(o.Pathname, "/")
			if !o.Matched || o.External || o.Error != "" || "/"+strings.Join(got, "/") != want && !(want == "" && len(got) == 0) {
				t.Errorf("%s: graphvein rewrites to /%s, next: matched %t, external %t, path %q, error %q", name, strings.Join(got, "/"), o.Matched, o.External, o.Pathname, o.Error)
				continue
			}
		case routes.RewriteExternal:
			if !o.Matched || o.Error == "" && !o.External {
				t.Errorf("%s: graphvein reads an external destination, next: matched %t, external %t, path %q", name, o.Matched, o.External, o.Pathname)
				continue
			}
		default:
			counted++
			t.Logf("%s: graphvein %s (not compared); next: matched %t, path %q, error %q", name, outcome, o.Matched, o.Pathname, o.Error)
			continue
		}
		agree++
	}
	t.Logf("%d rewrite cases agree, %d unsure or unmapped in Graphvein (not compared)", agree, counted)
}
