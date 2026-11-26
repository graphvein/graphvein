package projectlink_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const upFixtureDir = "../../testdata/fixtures/projectlink-unknown-prefix"

// TestUnknownPrefixProjectScenarios indexes the projectlink-unknown-prefix
// fixture (T033; rows UP-21, UP-25 and UP-26 of
// docs/contract/claims/unknown-prefix-paths.md, PL-R13 and PL-L7 of
// docs/extract/project-links.md): a frontend whose calls reach only routes
// below a configured base in two Go members. Without a consumes entry no
// call links on those routes (PL-R13); an entry naming the provider links
// them at 0.6 with the declaration assumed (PL-L7), a literal origin only
// through an entry naming that origin (UP-21); a tie inside the provider
// or a second declared provider stays ambiguous (UP-17, PL-R10, PL-R11).
// Every outcome is checked against its row independently of the golden.
func TestUnknownPrefixProjectScenarios(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(upFixtureDir)); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, m := range []string{"web", "api", "api2"} {
		indexMember(t, s, filepath.Join(root, m), m)
	}
	const (
		list   = "route:server.go#GET%20%2Fupd%2Forders%2Flist"
		order  = "route:server.go#GET%20%2Fupd%2Forder%2F%7Bid%7D"
		shared = "route:server.go#GET%20%2Fupd%2Fshared%2Fitems"
	)
	type want struct {
		row, target string
		assumes     []string
	}
	aligned := []string{clients.AssumeUnknownPrefix, clients.AssumeSamePrefix, clients.AssumeDeclaredProvider}
	known := []string{clients.AssumeUnknownPrefix, clients.AssumeDeclaredProvider}
	// Outcomes by a distinctive part of the call's identity.
	scenarios := map[string]map[string]want{
		"undeclared": {
			"withBase%28%22%2Fupd%2Forders%2Flist":      {row: projectlink.RowUnknownPrefix},
			"%2Fv1%2Fupd%2Forder%2F":                    {row: projectlink.RowUnknownPrefix},
			"https%3A%2F%2Fapi.example.com":             {row: projectlink.RowLiteralOrigin},
			"withBase%28%22%2Fupd%2Fshared%2Fitems":     {row: projectlink.RowUnknownPrefix},
			"withBase%28%22%2Fupd%2Ftie%2Fitems":        {row: projectlink.RowUnknownPrefix},
			"withBase%28%22%2Fupdping":                  {row: projectlink.RowNoCandidate},
			"import.meta.env.VITE_API%7D%2Fupd%2Forder": {row: projectlink.RowUnknownPrefix},
		},
		"declared": {
			"withBase%28%22%2Fupd%2Forders%2Flist":      {row: projectlink.RowCallUnknownPrefix, target: list, assumes: aligned},
			"%2Fv1%2Fupd%2Forder%2F":                    {row: projectlink.RowCallUnknownPrefix, target: order, assumes: known},
			"https%3A%2F%2Fapi.example.com":             {row: projectlink.RowLiteralOrigin},
			"withBase%28%22%2Fupd%2Fshared%2Fitems":     {row: projectlink.RowCallUnknownPrefix, target: shared, assumes: aligned},
			"withBase%28%22%2Fupd%2Ftie%2Fitems":        {row: projectlink.RowProviderAmbiguous},
			"withBase%28%22%2Fupdping":                  {row: projectlink.RowNoCandidate},
			"import.meta.env.VITE_API%7D%2Fupd%2Forder": {row: projectlink.RowCallUnknownPrefix, target: order, assumes: aligned},
		},
		"declared-origin": {
			"withBase%28%22%2Fupd%2Forders%2Flist":      {row: projectlink.RowNotDeclared},
			"%2Fv1%2Fupd%2Forder%2F":                    {row: projectlink.RowNotDeclared},
			"https%3A%2F%2Fapi.example.com":             {row: projectlink.RowCallUnknownPrefix, target: list, assumes: known},
			"withBase%28%22%2Fupd%2Fshared%2Fitems":     {row: projectlink.RowNotDeclared},
			"withBase%28%22%2Fupd%2Ftie%2Fitems":        {row: projectlink.RowNotDeclared},
			"withBase%28%22%2Fupdping":                  {row: projectlink.RowNotDeclared},
			"import.meta.env.VITE_API%7D%2Fupd%2Forder": {row: projectlink.RowNotDeclared},
		},
		"declared-two": {
			"withBase%28%22%2Fupd%2Forders%2Flist":      {row: projectlink.RowCallUnknownPrefix, target: list, assumes: aligned},
			"%2Fv1%2Fupd%2Forder%2F":                    {row: projectlink.RowCallUnknownPrefix, target: order, assumes: known},
			"https%3A%2F%2Fapi.example.com":             {row: projectlink.RowLiteralOrigin},
			"withBase%28%22%2Fupd%2Fshared%2Fitems":     {row: projectlink.RowSeveralProviders},
			"withBase%28%22%2Fupd%2Ftie%2Fitems":        {row: projectlink.RowProviderAmbiguous},
			"withBase%28%22%2Fupdping":                  {row: projectlink.RowNoCandidate},
			"import.meta.env.VITE_API%7D%2Fupd%2Forder": {row: projectlink.RowCallUnknownPrefix, target: order, assumes: aligned},
		},
	}
	tags := map[string]int{}
	for _, name := range []string{"undeclared", "declared", "declared-origin", "declared-two"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "scenarios", name+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			project, err := config.ParseProject(root, data, nil)
			if err != nil {
				t.Fatal(err)
			}
			clients.SetUnknownPrefixTagHook(func(row string) { tags[row]++ })
			result, _, err := projectlink.Run(ctx, s, project, "sha256:"+name)
			clients.SetUnknownPrefixTagHook(nil)
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, filepath.Join(upFixtureDir+"."+name+".golden.json"), view(result))
			expected := scenarios[name]
			seen := map[string]bool{}
			for _, o := range result.Outcomes {
				if o.Source.Repository != "web" {
					continue
				}
				key := ""
				for k := range expected {
					if strings.Contains(o.Source.Identity, k) {
						key = k
					}
				}
				if key == "" {
					t.Errorf("outcome without an expectation: %+v", o)
					continue
				}
				seen[key] = true
				w := expected[key]
				if o.Row != w.row {
					t.Errorf("%s: row %s (%s), want %s", key, o.Row, o.Reason, w.row)
					continue
				}
				if w.target == "" {
					if o.Target != nil {
						t.Errorf("%s: unexpected target %+v", key, o.Target)
					}
					continue
				}
				if o.Target == nil || o.Target.Repository != "api" || o.Target.Identity != w.target {
					t.Errorf("%s: target %+v, want api %s", key, o.Target, w.target)
				}
				for _, l := range result.Set.Links {
					if l.Source.Identity != o.Source.Identity {
						continue
					}
					if l.Confidence != 0.6 || !reflect.DeepEqual(l.Assumes, w.assumes) || !strings.HasSuffix(l.Assumption, projectlink.UnknownPrefixAssumptionNote(w.assumes)) || !strings.HasPrefix(l.Assumption, "declared in ") || l.Attributes["route_prefix"] != "unresolved" || l.Attributes["match"] != "suffix" {
						t.Errorf("%s: link %+v", key, l)
					}
				}
			}
			for k := range expected {
				if !seen[k] {
					t.Errorf("%s: no outcome", k)
				}
			}
		})
	}
	if tags["UP-21"] == 0 {
		t.Errorf("the fixture does not reach row UP-21 (tags %v)", tags)
	}
}

// TestUnknownPrefixProjectRows keeps the project link rows of the public
// unknown-prefix contract (section "Project link pass") and the project
// link rows that implement them 1:1: UP-25 is PL-R13 and UP-26 is PL-L7,
// each row naming the other.
func TestUnknownPrefixProjectRows(t *testing.T) {
	data, err := os.ReadFile("../../docs/contract/claims/unknown-prefix-paths.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Project link pass\n")
	if start < 0 {
		t.Fatal("no Project link pass section")
	}
	section := text[start+1:]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	public := map[string]string{}
	var ids []string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| UP-") {
			continue
		}
		id := strings.TrimSpace(strings.Split(line, "|")[1])
		ids = append(ids, id)
		public[id] = line
	}
	pairs := [][2]string{{"UP-25", projectlink.RowUnknownPrefix}, {"UP-26", projectlink.RowCallUnknownPrefix}}
	if !reflect.DeepEqual(ids, []string{"UP-25", "UP-26"}) {
		t.Fatalf("public project link rows %v, want [UP-25 UP-26]", ids)
	}
	docs := map[string]string{}
	for _, cells := range docsRows(t) {
		docs[cells[0]] = strings.Join(cells, " | ")
	}
	for _, p := range pairs {
		if !strings.Contains(public[p[0]], p[1]) {
			t.Errorf("public row %s does not name %s", p[0], p[1])
		}
		if !strings.Contains(docs[p[1]], p[0]) {
			t.Errorf("project link row %s does not name %s", p[1], p[0])
		}
	}
}
