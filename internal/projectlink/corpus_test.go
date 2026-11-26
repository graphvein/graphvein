//go:build graphveincorpus

package projectlink_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// corpusRoot is spike/parsing/.corpus, or GRAPHVEIN_CORPUS_DIR when a
// worktree reads another checkout's corpus.
func corpusRoot() string {
	if dir := os.Getenv("GRAPHVEIN_CORPUS_DIR"); dir != "" {
		return dir
	}
	return "../../spike/parsing/.corpus"
}

// verifiedCorpus checks a corpus checkout's pin and cleanliness.
func verifiedCorpus(t *testing.T, name, pin string) string {
	t.Helper()
	root := filepath.Join(corpusRoot(), name)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			t.Fatalf("verifying %s: %v", name, err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := git("rev-parse", "HEAD"); got != pin {
		t.Fatalf("%s pin %s, expected %s", name, got, pin)
	}
	if status := git("status", "--porcelain"); status != "" {
		t.Fatalf("%s is dirty: %s", name, status)
	}
	return root
}

// copyCorpus copies a checkout without its .git, so no nested repository
// is indexed and the checkout is never written.
func copyCorpus(t *testing.T, source, dest string) {
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
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// unmapped turns a composite identity under prefix/ back into the
// identity the same node has in its own repository.
func unmapped(t *testing.T, identity, prefix string) string {
	t.Helper()
	kind, file, name, err := contract.ParseIdentity(identity)
	if err != nil || !strings.HasPrefix(file, prefix+"/") {
		t.Fatalf("composite identity %s is not under %s/", identity, prefix)
	}
	if kind == "file" {
		name = strings.TrimPrefix(name, prefix+"/")
	}
	id, err := contract.Identity(kind, strings.TrimPrefix(file, prefix+"/"), name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// crossLink is a link reduced to what the composite comparison checks.
type crossLink struct {
	kind, source, target string
	confidence           float64
}

// compositeLinks indexes frontend/ + backend/ as one repository and
// returns its edges from a frontend node to a backend node, in the
// separate repositories' identities.
func compositeLinks(t *testing.T, react, backend string) []crossLink {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	copyCorpus(t, react, filepath.Join(root, "frontend"))
	copyCorpus(t, backend, filepath.Join(root, "backend"))
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "composite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	indexMember(t, s, root, "composite")
	dump, err := s.Dump(ctx, "composite")
	if err != nil {
		t.Fatal(err)
	}
	var out []crossLink
	for _, e := range dump.Edges {
		_, from, _, _ := contract.ParseIdentity(e.Source)
		_, to, _, _ := contract.ParseIdentity(e.Target)
		if strings.HasPrefix(from, "frontend/") && strings.HasPrefix(to, "backend/") && e.Kind != "imports" {
			out = append(out, crossLink{e.Kind, unmapped(t, e.Source, "frontend"), unmapped(t, e.Target, "backend"), e.Confidence})
		}
	}
	sortLinks(out)
	return out
}

func sortLinks(links []crossLink) {
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		return a.kind+"\x00"+a.source+"\x00"+a.target < b.kind+"\x00"+b.source+"\x00"+b.target
	})
}

func projectLinks(result projectlink.Result) []crossLink {
	var out []crossLink
	for _, l := range result.Set.Links {
		out = append(out, crossLink{l.Kind, l.Source.Identity, l.Target.Identity, l.Confidence})
	}
	sortLinks(out)
	return out
}

func count(links []crossLink, kind string) int {
	n := 0
	for _, l := range links {
		if l.kind == kind {
			n++
		}
	}
	return n
}

// TestCorpusRealWorld is the T022 acceptance check on RealWorld: react,
// express and axum-ddd indexed as separate repositories into one store and
// linked as projects reproduce the composite's links (with the
// assumption), stay ambiguous with two backends, and follow consumes
// declarations and selectors. Results are recorded in
// the T022 task record (internal) (step B3 notes).
func TestCorpusRealWorld(t *testing.T) {
	ctx := context.Background()
	react := verifiedCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := verifiedCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	axum := verifiedCorpus(t, "axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2")

	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scratch := t.TempDir()
	for name, source := range map[string]string{"react": react, "express": express, "axum-ddd": axum} {
		copyCorpus(t, source, filepath.Join(scratch, name))
		indexMember(t, s, filepath.Join(scratch, name), name)
	}
	before := map[string]string{}
	for _, name := range []string{"react", "express", "axum-ddd"} {
		dump, err := s.Dump(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = jsonText(t, dump)
	}

	pass := func(t *testing.T, name string, members ...config.Member) projectlink.Result {
		t.Helper()
		result, _, err := projectlink.Run(ctx, s, config.Project{Name: name, Members: members}, "sha256:"+name)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	orval := func(t *testing.T, result projectlink.Result) map[string]int {
		t.Helper()
		calls, err := s.Nodes(ctx, "react", store.Query{Kind: "client_call"})
		if err != nil {
			t.Fatal(err)
		}
		isOrval := map[string]bool{}
		for _, n := range calls {
			if n.Attributes["library"] == "orval" {
				isOrval[n.Identity] = true
			}
		}
		rows := map[string]int{}
		for _, o := range result.Outcomes {
			if o.Kind == projectlink.KindCallsRoute && isOrval[o.Source.Identity] {
				rows[o.Row]++
			}
		}
		return rows
	}

	// alternatives counts the outcomes of react's calls with several or
	// conditional alternatives by row (one outcome per alternative since
	// extractors-47; PL-R3 for every such call before).
	alternatives := func(t *testing.T, result projectlink.Result) (int, map[string]int) {
		t.Helper()
		calls, err := s.Nodes(ctx, "react", store.Query{Kind: "client_call"})
		if err != nil {
			t.Fatal(err)
		}
		several := map[string]bool{}
		for _, n := range calls {
			paths, _ := n.Attributes["paths"].([]any)
			multi := len(paths) > 1
			for _, raw := range paths {
				if p, ok := raw.(map[string]any); ok && p["conditional"] == true {
					multi = true
				}
			}
			several[n.Identity] = multi
		}
		count := 0
		for _, multi := range several {
			if multi {
				count++
			}
		}
		rows := map[string]int{}
		for _, o := range result.Outcomes {
			if o.Kind == projectlink.KindCallsRoute && several[o.Source.Identity] {
				rows[o.Row]++
			}
		}
		return count, rows
	}

	composites := map[string][]crossLink{}
	// The Express equivalence proof is stored as equivalent_registrations
	// (extractors-47): express registers /api/articles/:slug/favorite twice
	// (POST and DELETE), and the project pass collapses each pair to the
	// first registration as the composite does. No composite link is
	// missing.
	collapsed := map[string][2]int{"express": {0, 0}, "axum-ddd": {0, 0}}
	for _, backend := range []struct{ name, source string }{{"express", express}, {"axum-ddd", axum}} {
		t.Run("react+"+backend.name, func(t *testing.T) {
			want := compositeLinks(t, react, backend.source)
			composites[backend.name] = want
			if count(want, "calls_route") != 27 || count(want, "documents_route") != 19 || len(want) != 46 {
				t.Fatalf("composite: %d calls_route, %d documents_route, %d cross-root edges", count(want, "calls_route"), count(want, "documents_route"), len(want))
			}
			result := pass(t, "realworld-"+backend.name, config.Member{Repository: "react"}, config.Member{Repository: backend.name})
			got := map[string]bool{}
			for _, l := range gotStrings(projectLinks(result)) {
				got[l] = true
			}
			composite := map[string]bool{}
			for _, l := range gotStrings(want) {
				composite[l] = true
			}
			for l := range got {
				if !composite[l] {
					t.Errorf("link not made by the composite: %s", l)
				}
			}
			outcomes := map[[2]string]projectlink.Outcome{}
			for _, o := range result.Outcomes {
				outcomes[[2]string{o.Kind, o.Source.Identity}] = o
			}
			missing := map[string]int{}
			for _, l := range want {
				if got[gotStrings([]crossLink{l})[0]] {
					continue
				}
				o := outcomes[[2]string{l.kind, l.source}]
				found := false
				for _, c := range o.Candidates {
					found = found || c == projectlink.Endpoint{Repository: backend.name, Identity: l.target}
				}
				if o.Row != projectlink.RowProviderAmbiguous || !found {
					t.Errorf("composite link %v missing without an ambiguity inside %s: %+v", l, backend.name, o)
				}
				missing[l.kind]++
			}
			if missing["calls_route"] != collapsed[backend.name][0] || missing["documents_route"] != collapsed[backend.name][1] {
				t.Errorf("composite links not reproduced: %v", missing)
			}
			for _, l := range result.Set.Links {
				if l.Assumption != projectlink.DefaultAssumption || l.Target.Repository != backend.name {
					t.Fatalf("link %+v", l)
				}
				if l.Kind == projectlink.KindCallsRoute && (l.Confidence != 0.7 || l.Attributes["match"] != "exact") {
					t.Fatalf("calls_route %+v", l)
				}
			}
			if rows := orval(t, result); rows[projectlink.RowCallDefault] != 27-collapsed[backend.name][0] || rows[projectlink.RowProviderAmbiguous] != collapsed[backend.name][0] {
				t.Fatalf("Orval rows %v", rows)
			}
			links := projectLinks(result)
			multi, rows := alternatives(t, result)
			t.Logf("react+%s: %d calls_route, %d documents_route, all made by the composite; composite links left ambiguous inside %s: %v; react calls with several or conditional alternatives: %d, alternative outcomes %v", backend.name, count(links, "calls_route"), count(links, "documents_route"), backend.name, missing, multi, rows)
		})
	}
	if len(composites) != 2 {
		t.Fatal("two-member checks failed")
	}
	// Every link a three-member pass makes must be one a two-member pass
	// (the composite) makes for the same provider: anything else is wrong.
	allowed := map[crossLink]string{}
	for name, links := range composites {
		for _, l := range links {
			allowed[crossLink{l.kind, l.source, l.target, 0}] = name
		}
	}
	checkAllowed := func(t *testing.T, result projectlink.Result) {
		t.Helper()
		for _, l := range result.Set.Links {
			if allowed[crossLink{l.Kind, l.Source.Identity, l.Target.Identity, 0}] != l.Target.Repository {
				t.Errorf("wrong link: %s %s -> %s %s", l.Kind, l.Source.Identity, l.Target.Repository, l.Target.Identity)
			}
		}
	}
	all := []config.Member{{Repository: "react"}, {Repository: "express"}, {Repository: "axum-ddd"}}

	t.Run("react+express+axum-ddd", func(t *testing.T) {
		result := pass(t, "realworld-three", all...)
		checkAllowed(t, result)
		links := projectLinks(result)
		if rows := orval(t, result); rows[projectlink.RowSeveralProviders] != 27 || len(rows) != 1 {
			t.Fatalf("Orval rows %v", rows)
		}
		if count(links, "calls_route") != 0 {
			t.Fatalf("%d calls_route with two backends", count(links, "calls_route"))
		}
		documents := map[string]int{}
		for _, o := range result.Outcomes {
			if o.Kind == projectlink.KindDocumentsRoute {
				documents[o.Row]++
			}
		}
		t.Logf("react+express+axum-ddd: 27 Orval calls ambiguous (PL-R11), 0 calls_route, %d documents_route (all also made by a two-member pass), documents outcomes %v", count(links, "documents_route"), documents)
	})

	t.Run("consumes express", func(t *testing.T) {
		members := append([]config.Member(nil), all...)
		members[0].Consumes = []config.Consume{{Repository: "express"}}
		result := pass(t, "realworld-consumes", members...)
		checkAllowed(t, result)
		links := projectLinks(result)
		if count(links, "calls_route") != 27 || count(links, "documents_route") != 19 {
			t.Fatalf("%d calls_route, %d documents_route", count(links, "calls_route"), count(links, "documents_route"))
		}
		if rows := orval(t, result); rows[projectlink.RowCallDeclared] != 27 || len(rows) != 1 {
			t.Fatalf("Orval rows %v", rows)
		}
		for _, l := range result.Set.Links {
			if l.Target.Repository != "express" || l.Assumption != "declared in graphvein-project.yaml: `react` consumes `express`" {
				t.Fatalf("link %+v", l)
			}
		}
	})

	t.Run("selector per provider", func(t *testing.T) {
		members := append([]config.Member(nil), all...)
		members[0].Consumes = []config.Consume{
			{Repository: "express", Select: config.Selector{PathPrefixes: []string{"/api/articles"}}},
			{Repository: "axum-ddd", Select: config.Selector{PathPrefixes: []string{"/api/profiles"}}},
		}
		result := pass(t, "realworld-selector", members...)
		checkAllowed(t, result)
		targets := map[string]int{}
		for _, l := range result.Set.Links {
			if l.Kind != projectlink.KindCallsRoute {
				continue
			}
			template, _ := l.Attributes["template"].(string)
			want := "express"
			if strings.HasPrefix(template, "/api/profiles") {
				want = "axum-ddd"
			}
			if l.Target.Repository != want {
				t.Fatalf("link %+v", l)
			}
			targets[want]++
		}
		// Calls outside both prefixes (/api/user, /api/users, /api/tags) are
		// not declared (PL-R6); the favorite calls link to express's first
		// registrations.
		rows := orval(t, result)
		if rows[projectlink.RowCallSelector] != 22 || rows[projectlink.RowNotDeclared] != 5 || len(rows) != 2 {
			t.Fatalf("Orval rows %v", rows)
		}
		t.Logf("selector per provider: Orval calls linked %v, rows %v", targets, rows)
	})

	for _, name := range []string{"react", "express", "axum-ddd"} {
		dump, err := s.Dump(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if jsonText(t, dump) != before[name] {
			t.Errorf("%s changed by the project link passes", name)
		}
	}
}

func gotStrings(links []crossLink) []string {
	out := []string{}
	for _, l := range links {
		out = append(out, fmt.Sprintf("%s %s %s %g", l.kind, l.source, l.target, l.confidence))
	}
	return out
}
