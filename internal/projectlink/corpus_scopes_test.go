//go:build graphveincorpus

package projectlink_test

import (
	"context"
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

// expressScopes declares the express corpus's three servers as link scopes
// (ADR 0021 note; T022 acceptance "Link scopes").
var expressScopes = []config.Scope{{Name: "app", Path: "apps/app"}, {Name: "user", Path: "apps/user"}, {Name: "article", Path: "apps/article"}}

// rowCounts counts outcomes of kind by row.
func rowCounts(result projectlink.Result, kind string) map[string]int {
	rows := map[string]int{}
	for _, o := range result.Outcomes {
		if o.Kind == kind {
			rows[o.Row]++
		}
	}
	return rows
}

// TestCorpusLinkScopes is the T022 B5 corpus check: express's apps/app,
// apps/user and apps/article declared as link scopes each own their
// routes; a consumer declaring one scope links only into it; and inside one
// repository holding react, express and axum-ddd, a declared scope
// resolves the calls the in-repository linker leaves ambiguous between the
// two backends, as a project overlay, to the links the react + express
// composite makes. Results are recorded in the T022 task record (internal)
// (step B5 notes).
func TestCorpusLinkScopes(t *testing.T) {
	ctx := context.Background()
	react := verifiedCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := verifiedCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	axum := verifiedCorpus(t, "axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2")

	// The composite react + express (one repository) is the reference: a
	// link not made there is wrong.
	reference := compositeLinks(t, react, express)
	allowed := map[[3]string]bool{}
	for _, l := range reference {
		allowed[[3]string{l.kind, l.source, l.target}] = true
	}

	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scratch := t.TempDir()
	for name, source := range map[string]string{"react": react, "express": express} {
		copyCorpus(t, source, filepath.Join(scratch, name))
		indexMember(t, s, filepath.Join(scratch, name), name)
	}
	before, err := s.Dump(ctx, "express")
	if err != nil {
		t.Fatal(err)
	}
	pass := func(t *testing.T, name string, members ...config.Member) projectlink.Result {
		t.Helper()
		result, _, err := projectlink.Run(ctx, s, config.Project{Name: name, Members: members}, "sha256:"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range result.Set.Links {
			if !allowed[[3]string{l.Kind, l.Source.Identity, l.Target.Identity}] {
				t.Errorf("wrong link (not made by the composite): %s %s -> %s", l.Kind, l.Source.Identity, l.Target.Identity)
			}
		}
		return result
	}
	targetScopes := func(result projectlink.Result) map[string]int {
		out := map[string]int{}
		for _, l := range result.Set.Links {
			scope, _ := l.Attributes["target_scope"].(string)
			out[l.Kind+" "+scope]++
		}
		return out
	}
	scoped := config.Member{Repository: "express", Scopes: expressScopes}

	// Express itself has no client calls and no route registered in two of
	// its servers, so its scopes change nothing inside it (its 19 spec
	// operations keep their own result); react without declarations still
	// links every call (each route lies in one scope).
	t.Run("express scopes, no declarations", func(t *testing.T) {
		result := pass(t, "scopes-default", config.Member{Repository: "react"}, scoped)
		calls := rowCounts(result, projectlink.KindCallsRoute)
		if len(result.Set.Links) != len(reference) {
			t.Errorf("%d links, the composite makes %d", len(result.Set.Links), len(reference))
		}
		t.Logf("react + express with scopes app/user/article: %d links (composite %d), by target scope %v; call rows %v; documents rows %v",
			len(result.Set.Links), len(reference), targetScopes(result), calls, rowCounts(result, projectlink.KindDocumentsRoute))
		// Express's own sources are the operations of its OpenAPI spec
		// (apps/app), which its own index decides: PL-R1, unchanged by
		// scopes.
		own := map[string]int{}
		for _, o := range result.Outcomes {
			if o.Source.Repository == "express" {
				own[o.Kind+" "+o.Row]++
			}
		}
		if len(own) != 1 || own[projectlink.KindDocumentsRoute+" "+projectlink.RowInRepository] != 19 {
			t.Errorf("express's own sources: %v", own)
		}
	})

	for _, scope := range []string{"user", "article", "app"} {
		t.Run("react consumes express/"+scope, func(t *testing.T) {
			front := config.Member{Repository: "react", Consumes: []config.Consume{{Repository: "express", Scope: scope}}}
			result := pass(t, "scopes-"+scope, front, scoped)
			for _, l := range result.Set.Links {
				_, file, _, err := contract.ParseIdentity(l.Target.Identity)
				if err != nil || !strings.HasPrefix(file, "apps/"+scope+"/") || l.Attributes["target_scope"] != scope {
					t.Errorf("link outside express/%s: %s", scope, l.Target.Identity)
				}
			}
			t.Logf("react consumes express/%s: %d links %v; call rows %v; documents rows %v", scope, len(result.Set.Links), targetScopes(result),
				rowCounts(result, projectlink.KindCallsRoute), rowCounts(result, projectlink.KindDocumentsRoute))
		})
	}
	after, err := s.Dump(ctx, "express")
	if err != nil {
		t.Fatal(err)
	}
	if jsonText(t, after) != jsonText(t, before) {
		t.Error("express changed by the project link passes")
	}

	// One repository holding react, express and axum-ddd: the in-repository
	// linker leaves calls ambiguous between the two backends. Declaring the
	// backends as scopes and the frontend scope as consuming express resolves
	// them as a project overlay.
	t.Run("one repository, frontend consumes the express scope", func(t *testing.T) {
		root := t.TempDir()
		copyCorpus(t, react, filepath.Join(root, "frontend"))
		copyCorpus(t, express, filepath.Join(root, "express"))
		copyCorpus(t, axum, filepath.Join(root, "axum"))
		mono, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "mono.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer mono.Close()
		indexMember(t, mono, root, "realworld")
		monoBefore, err := mono.Dump(ctx, "realworld")
		if err != nil {
			t.Fatal(err)
		}
		// In-repository ambiguity before any project pass.
		ambiguous := map[string]int{}
		for _, kind := range []string{"client_call", "api_operation"} {
			nodes, err := mono.Nodes(ctx, "realworld", store.Query{Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range nodes {
				if n.Attributes["status"] == "ambiguous" {
					ambiguous[kind]++
				}
				if paths, ok := n.Attributes["paths"].([]any); ok && kind == "api_operation" {
					for _, raw := range paths {
						if p, ok := raw.(map[string]any); ok && p["status"] == "ambiguous" {
							ambiguous["api_operation path"]++
						}
					}
				}
			}
		}
		member := config.Member{Repository: "realworld", Scopes: []config.Scope{
			{Name: "frontend", Path: "frontend", Consumes: []config.Consume{{Repository: "realworld", Scope: "express"}}},
			{Name: "express", Path: "express"},
			{Name: "axum", Path: "axum"},
		}}
		result, _, err := projectlink.Run(ctx, mono, config.Project{Name: "realworld-mono", Members: []config.Member{member}}, "sha256:mono")
		if err != nil {
			t.Fatal(err)
		}
		var wrong, links []string
		for _, l := range result.Set.Links {
			source, target := unmapped(t, l.Source.Identity, "frontend"), unmapped(t, l.Target.Identity, "express")
			links = append(links, l.Kind)
			if !allowed[[3]string{l.Kind, source, target}] || l.Attributes["target_scope"] != "express" {
				wrong = append(wrong, l.Kind+" "+l.Source.Identity+" -> "+l.Target.Identity)
			}
		}
		sort.Strings(wrong)
		if len(wrong) > 0 {
			t.Errorf("%d overlay links not made by the react + express composite: %v", len(wrong), wrong)
		}
		if len(result.Set.Links) != len(reference) {
			t.Errorf("%d overlay links, the react + express composite makes %d", len(result.Set.Links), len(reference))
		}
		monoAfter, err := mono.Dump(ctx, "realworld")
		if err != nil {
			t.Fatal(err)
		}
		if jsonText(t, monoAfter) != jsonText(t, monoBefore) {
			t.Error("the overlay changed the repository's own snapshot")
		}
		calls, documents := rowCounts(result, projectlink.KindCallsRoute), rowCounts(result, projectlink.KindDocumentsRoute)
		count := func(kind string) int {
			n := 0
			for _, k := range links {
				if k == kind {
					n++
				}
			}
			return n
		}
		t.Logf("one repository (frontend, express, axum): in-repository ambiguous %v; overlay links %d calls_route, %d documents_route, 0 wrong; call rows %v; documents rows %v",
			ambiguous, count(projectlink.KindCallsRoute), count(projectlink.KindDocumentsRoute), calls, documents)
	})
}
