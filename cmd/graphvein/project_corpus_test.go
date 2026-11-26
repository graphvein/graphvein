//go:build graphveincorpus

package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// corpusDir is spike/parsing/.corpus, or GRAPHVEIN_CORPUS_DIR when a
// worktree reads another checkout's corpus.
func corpusDir() string {
	if dir := os.Getenv("GRAPHVEIN_CORPUS_DIR"); dir != "" {
		return dir
	}
	return "../../spike/parsing/.corpus"
}

// pinnedCorpus checks a corpus checkout's pin and cleanliness.
func pinnedCorpus(t *testing.T, name, pin string) string {
	t.Helper()
	root := filepath.Join(corpusDir(), name)
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

// copyCheckout copies a checkout without its .git, so the corpus checkout
// is never written and no nested repository is indexed.
func copyCheckout(t *testing.T, source, dest string) {
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

// compositeEdges indexes frontend/ + backend/ as one repository and
// returns its cross-root edges (not imports) as "kind source target
// confidence", in the separate repositories' identities.
func compositeEdges(t *testing.T, frontend, backend string) map[string]bool {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	copyCheckout(t, frontend, filepath.Join(root, "frontend"))
	copyCheckout(t, backend, filepath.Join(root, "backend"))
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "composite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stream bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "composite", Parser: parse.Options{Workers: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s); err != nil {
		t.Fatal(err)
	}
	dump, err := s.Dump(ctx, "composite")
	if err != nil {
		t.Fatal(err)
	}
	unprefixed := func(identity, prefix string) string {
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
	out := map[string]bool{}
	for _, e := range dump.Edges {
		_, from, _, _ := contract.ParseIdentity(e.Source)
		_, to, _, _ := contract.ParseIdentity(e.Target)
		if strings.HasPrefix(from, "frontend/") && strings.HasPrefix(to, "backend/") && e.Kind != "imports" {
			out[fmt.Sprintf("%s %s %s %g", e.Kind, unprefixed(e.Source, "frontend"), unprefixed(e.Target, "backend"), e.Confidence)] = true
		}
	}
	return out
}

// TestCorpusProjectIndex is T022 B4's corpus gate through the CLI path:
// graphvein index --project on RealWorld react + express links 27/27 Orval
// calls and 19 operations exactly as the composite does, react + express +
// axum-ddd leaves all 27 ambiguous with no link that a two-member project
// does not make (0 wrong). Results are recorded in the T022 task record (internal).
func TestCorpusProjectIndex(t *testing.T) {
	ctx := context.Background()
	react := pinnedCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := pinnedCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	axum := pinnedCorpus(t, "axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2")
	scratch := t.TempDir()
	for name, source := range map[string]string{"react": react, "express": express, "axum-ddd": axum} {
		copyCheckout(t, source, filepath.Join(scratch, name))
	}
	db := filepath.Join(t.TempDir(), "project.db")
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", db)

	// project indexes the named members as project name through the CLI and
	// returns the stored link set.
	project := func(t *testing.T, name string, members ...string) store.ProjectLinkSet {
		t.Helper()
		dir := filepath.Join(scratch, name)
		manifest := "version: 1\nname: " + name + "\nmembers:\n"
		for _, m := range members {
			manifest += "  - repository: " + m + "\n    path: ../" + m + "\n"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, dir, manifest)
		code, out, errOut := runIndex(t, nil, "index", "--project", dir)
		if code != 0 {
			t.Fatalf("index --project %s = %d\n%s\n%s", name, code, out, errOut)
		}
		t.Logf("%s:\n%s", name, out)
		s, err := sqlite.OpenRead(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		set, err := s.ProjectLinks(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	orvalRows := func(t *testing.T, set store.ProjectLinkSet) map[string]int {
		t.Helper()
		s, err := sqlite.OpenRead(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		calls, err := s.Nodes(ctx, "react", store.Query{Kind: "client_call"})
		if err != nil {
			t.Fatal(err)
		}
		orval := map[string]bool{}
		for _, n := range calls {
			if n.Attributes["library"] == "orval" {
				orval[n.Identity] = true
			}
		}
		rows := map[string]int{}
		for _, o := range set.Outcomes {
			if o.Kind == projectlink.KindCallsRoute && orval[o.Source.Identity] {
				rows[o.Row]++
			}
		}
		return rows
	}
	key := func(l store.ProjectLink) string {
		return fmt.Sprintf("%s %s %s %g", l.Kind, l.Source.Identity, l.Target.Identity, l.Confidence)
	}
	counts := func(set store.ProjectLinkSet) map[string]int {
		out := map[string]int{}
		for _, l := range set.Links {
			out[l.Kind]++
		}
		return out
	}

	allowed := map[string]string{}
	composite := compositeEdges(t, react, express)
	two := project(t, "realworld-express", "react", "express")
	if c := counts(two); c["calls_route"] != 27 || c["documents_route"] != 19 || len(two.Links) != len(composite) {
		t.Fatalf("react+express: %v links, composite %d", c, len(composite))
	}
	for _, l := range two.Links {
		if !composite[key(l)] {
			t.Errorf("react+express link not made by the composite: %s", key(l))
		}
		if l.Assumption != projectlink.DefaultAssumption || (l.Kind == "calls_route" && l.Confidence != 0.7) {
			t.Errorf("react+express link %+v", l)
		}
		allowed[key(l)] = l.Target.Repository
	}
	if rows := orvalRows(t, two); rows[projectlink.RowCallDefault] != 27 || len(rows) != 1 {
		t.Fatalf("react+express Orval rows %v", rows)
	}

	axumTwo := project(t, "realworld-axum", "react", "axum-ddd")
	if c := counts(axumTwo); c["calls_route"] != 27 || c["documents_route"] != 19 {
		t.Fatalf("react+axum-ddd: %v", c)
	}
	for _, l := range axumTwo.Links {
		allowed[key(l)] = l.Target.Repository
	}

	three := project(t, "realworld-three", "react", "express", "axum-ddd")
	if rows := orvalRows(t, three); rows[projectlink.RowSeveralProviders] != 27 || len(rows) != 1 {
		t.Fatalf("react+express+axum-ddd Orval rows %v", rows)
	}
	if c := counts(three); c["calls_route"] != 0 {
		t.Fatalf("react+express+axum-ddd: %v", c)
	}
	for _, l := range three.Links {
		if allowed[key(l)] != l.Target.Repository {
			t.Errorf("wrong link: %s -> %s", key(l), l.Target.Repository)
		}
	}
	t.Logf("CLI path: react+express %v (all in the composite), react+axum-ddd %v, three members %v with 27 Orval calls ambiguous", counts(two), counts(axumTwo), counts(three))
}
