package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
)

// TestStoredMatcherReplaysClientLinks checks that the stored attributes
// the project link pass reads (ADR 0075) are enough to reproduce the
// in-repository client linker: for every client call alternative of the
// client fixtures, the matcher built from stored route nodes alone
// (equivalent_registrations as ADR 0022's proof) given the stored method,
// template, scope, origin and base (the alternative's own base where the
// call has several or conditional alternatives, extractors-47) returns the
// stored status and candidates. Equivalent Express registrations and
// conditional alternatives are both in these fixtures.
func TestStoredMatcherReplaysClientLinks(t *testing.T) {
	for _, fixture := range []string{"clients", "client-suffix-precision", "client-exact-literal-precision", "env-wrappers", "url-evaluation", "angular-httpclient", "heyapi", "orval", "build-constants", "test-scope", "projectlink/frontend", "link-precision-fl", "unknown-prefix", "unknown-prefix-helpers"} {
		t.Run(fixture, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS(filepath.Join("../../testdata/fixtures", fixture))); err != nil {
				t.Fatal(err)
			}
			// angular-httpclient's revision3 is indexed as its own repository
			// by its golden test; the projectlink spec has an inert name.
			_ = os.RemoveAll(filepath.Join(root, "revision3"))
			// link-precision-fl keeps its catch-all routes under an inert
			// name (its README); they are what rows LF-1 and LF-2 replay.
			if fixture == "link-precision-fl" {
				if err := os.Rename(filepath.Join(root, "fallback.ts.inert"), filepath.Join(root, "fallback.ts")); err != nil {
					t.Fatal(err)
				}
			}
			s, opt := open(t), options(t)
			emit(t, root, s, opt)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			var routes []contract.Node
			for _, n := range snapshot.Nodes {
				if n.Kind == "route" {
					routes = append(routes, n)
				}
			}
			matcher := clients.NewStoredRouteMatcher(routes)
			replayed, several := 0, 0
			for _, n := range snapshot.Nodes {
				a := n.Attributes
				method, ok := a["method"].(string)
				if n.Kind != "client_call" || !ok || a["status"] == "wrapper" {
					continue
				}
				paths, _ := a["paths"].([]any)
				multi := len(paths) > 1
				for _, raw := range paths {
					p := raw.(map[string]any)
					multi = multi || p["conditional"] == true
				}
				for _, raw := range paths {
					p := raw.(map[string]any)
					template, ok := p["template"].(string)
					status, _ := p["status"].(string)
					if !ok || status == "dynamic" {
						continue
					}
					base, _ := a["base"].(string)
					if multi {
						stored, has := p["base"].(string)
						if !has {
							t.Fatalf("%s %s: no stored alternative base", n.Identity, template)
						}
						base = stored
						several++
					}
					scope, _ := a["scope"].(string)
					origin, _ := p["origin"].(string)
					request := clients.CallRequest{Method: method, Template: template, Scope: scope, Base: base, Origin: origin, OriginUnresolved: p["origin_resolution"] == "unresolved", TrailingSlash: p["trailing_slash"] == true}
					got := matcher.Call(request)
					want := []string{}
					for _, c := range p["candidates"].([]any) {
						want = append(want, c.(string))
					}
					count := fmt.Sprint(p["candidate_count"])
					if got.Status != status || !reflect.DeepEqual(got.Candidates, want) || fmt.Sprint(got.CandidateCount) != count {
						t.Errorf("%s %s: stored matcher %+v, in-repository status %s candidates %v count %s", n.Identity, template, got, status, want, count)
					}
					replayed++
				}
			}
			t.Logf("%s: %d alternatives replayed, %d with a stored alternative base", fixture, replayed, several)
			if replayed == 0 {
				t.Fatal("nothing replayed")
			}
		})
	}
}
