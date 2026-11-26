package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
)

// TestProjectForget is the B11 follow-up gate on both backends: after a
// rename made on disk, project forget removes the old name's stored index
// and project rows and keeps the other member's; a name still in the
// manifest is refused and nothing is removed; forgetting it again says
// nothing is stored.
func TestProjectForget(t *testing.T) {
	projectBackends(t, func(t *testing.T, open func() store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t, twoManifest)
		if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		s := open()
		before, err := s.ProjectLinks(ctx, "shop-two")
		if err != nil || len(before.Links) == 0 {
			t.Fatalf("no links before forget: %d %v", len(before.Links), err)
		}

		// A current member is refused, whatever the store holds.
		code, out, errOut := runProject(t, "forget", "--manifest", root, "backend")
		if code != 2 || out != "" || !strings.Contains(errOut, "backend is a member of project shop-two") {
			t.Fatalf("forget a member = %d %q %q", code, out, errOut)
		}
		if got := repositoryIDs(t, s); !slices.Equal(got, []string{"backend", "frontend"}) {
			t.Fatalf("repositories after a refused forget = %v", got)
		}

		// Rename on disk: backend is now api.
		writeManifest(t, root, strings.Replace(twoManifest, "repository: backend", "repository: api", 1))
		code, out, errOut = runProject(t, "forget", "backend", "--manifest", filepath.Join(root, config.ProjectFile))
		if code != 0 || errOut != "" || !strings.Contains(out, "Removed backend from project shop-two: its stored index and its project rows.") {
			t.Fatalf("forget = %d %q %q", code, out, errOut)
		}
		if got := repositoryIDs(t, s); !slices.Equal(got, []string{"frontend"}) {
			t.Fatalf("repositories after forget = %v", got)
		}
		after, err := s.ProjectLinks(ctx, "shop-two")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range after.Links {
			if l.Source.Repository == "backend" || l.Target.Repository == "backend" {
				t.Fatalf("a link still names backend: %+v", l)
			}
		}
		for _, m := range after.Members {
			if m.Repository == "backend" {
				t.Fatal("backend is still a stored member")
			}
		}

		// Again: nothing stored, not an error.
		code, out, errOut = runProject(t, "forget", "--manifest", root, "backend")
		if code != 0 || errOut != "" || !strings.Contains(out, "Nothing stored under backend") {
			t.Fatalf("forget again = %d %q %q", code, out, errOut)
		}

		// The project links again under the new name.
		if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 || !strings.Contains(out, "Link pass 2: ") {
			t.Fatalf("index --project after forget = %d\n%s\n%s", code, out, errOut)
		}
		if got := repositoryIDs(t, s); !slices.Equal(got, []string{"api", "frontend"}) {
			t.Fatalf("repositories after re-index = %v", got)
		}
	})
}

func TestProjectForgetUsage(t *testing.T) {
	root := copyProjectFixture(t, twoManifest)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	for _, args := range [][]string{
		{"forget", "--manifest", root},
		{"forget", "--manifest", root, "a", "b"},
		{"forget", "--manifest", filepath.Join(root, "missing"), "old"},
		{"forget", "--manifest", root, "old@ref:main"},
	} {
		if code, out, errOut := runProject(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d %q %q", args, code, out, errOut)
		}
	}
}

func repositoryIDs(t *testing.T, s store.Store) []string {
	t.Helper()
	repos, err := s.Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range repos {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

// TestProjectForgetNotAMember: a repository no stored pass of the project
// had as a member (here a standalone index in the same store) is refused
// and keeps its index.
func TestProjectForgetNotAMember(t *testing.T) {
	projectBackends(t, func(t *testing.T, open func() store.Store) {
		root := copyProjectFixture(t, twoManifest)
		if code, out, errOut := runIndex(t, nil, "index", filepath.Join(root, "backend"), "--repository", "standalone"); code != 0 {
			t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
		}
		for _, linked := range []bool{false, true} {
			if linked {
				if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 {
					t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
				}
			}
			code, out, errOut := runProject(t, "forget", "--manifest", root, "standalone")
			if code != 2 || out != "" || !strings.Contains(errOut, "no stored link pass of project shop-two names standalone") {
				t.Fatalf("forget (linked %v) = %d %q %q", linked, code, out, errOut)
			}
			if got := repositoryIDs(t, open()); !slices.Contains(got, "standalone") {
				t.Fatalf("standalone removed: %v", got)
			}
		}
	})
}
