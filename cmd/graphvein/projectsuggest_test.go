package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
)

const threeManifest = "version: 1\nname: shop-three\nmembers:\n  # frontend calls through VITE_API\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n  - repository: backend2\n    path: backend2\n"

var linkPassLine = regexp.MustCompile(`Link pass \d+: \d+ links \(added (\d+), removed (\d+), changed (\d+)`)

// TestProjectSuggestChangesNoLinkUntilAccepted is T022 B6's gate: on the
// project link fixture with two possible backends (every call
// ambiguous), listing the proposal writes nothing and the next link pass
// changes no link; only --accept writes the consumes entry, and only the
// pass after it links the calls to the proposed backend.
func TestProjectSuggestChangesNoLinkUntilAccepted(t *testing.T) {
	projectBackends(t, func(t *testing.T, open func() store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t, threeManifest)
		for path, content := range map[string]string{
			"frontend/.env": "VITE_API=http://localhost:3100\n",
			"backend/.env":  "PORT=3100\n",
		} {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		manifestPath := filepath.Join(root, config.ProjectFile)
		if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		s := open()
		before, err := s.ProjectLinks(ctx, "shop-three")
		if err != nil {
			t.Fatal(err)
		}

		code, out, errOut := runProject(t, "suggest", "--manifest", root)
		if code != 0 || errOut != "" || !strings.Contains(out, "1. frontend consumes backend  select: bases [VITE_API]\n") || !strings.Contains(out, "frontend: .env:1  VITE_API=http://localhost:3100  (SG-E1)") || !strings.Contains(out, "backend: .env:1  PORT=3100  (SG-E2)") {
			t.Fatalf("suggest = %d\n%s\n%s", code, out, errOut)
		}
		if data, err := os.ReadFile(manifestPath); err != nil || string(data) != threeManifest {
			t.Fatalf("listing changed the manifest: %q %v", data, err)
		}
		after, err := s.ProjectLinks(ctx, "shop-three")
		if err != nil || !sameJSON(t, before, after) {
			t.Fatalf("listing changed the stored links: %v", err)
		}
		code, out, errOut = runIndex(t, nil, "index", "--project", root)
		if m := linkPassLine.FindStringSubmatch(out); code != 0 || m == nil || m[1]+m[2]+m[3] != "000" {
			t.Fatalf("link pass after listing changed links = %d\n%s\n%s", code, out, errOut)
		}

		code, out, errOut = runProject(t, "suggest", "--manifest", root, "--accept", "1")
		if code != 0 || !strings.Contains(out, "frontend consumes backend (select: bases [VITE_API])") || !strings.Contains(out, "first consumes entry") {
			t.Fatalf("accept = %d\n%s\n%s", code, out, errOut)
		}
		data, err := os.ReadFile(manifestPath)
		if err != nil || !strings.Contains(string(data), "# frontend calls through VITE_API") || !strings.Contains(string(data), "consumes:\n      - repository: backend\n        select:\n          bases: [VITE_API]\n") {
			t.Fatalf("accepted manifest:\n%s %v", data, err)
		}
		code, out, errOut = runIndex(t, nil, "index", "--project", root)
		if m := linkPassLine.FindStringSubmatch(out); code != 0 || m == nil || m[1] == "0" {
			t.Fatalf("link pass after accepting added no link = %d\n%s\n%s", code, out, errOut)
		}
		linked, err := s.ProjectLinks(ctx, "shop-three")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range linked.Links {
			if l.Target.Repository != "backend" {
				t.Errorf("link to %s after declaring backend: %+v", l.Target.Repository, l)
			}
		}

		// The accepted proposal is now declared: accepting it again is
		// refused and writes nothing.
		code, _, errOut = runProject(t, "suggest", "--manifest", root, "--accept", "1")
		if again, _ := os.ReadFile(manifestPath); code != 2 || !strings.Contains(errOut, "already has a consumes entry for backend") || !bytes.Equal(again, data) {
			t.Fatalf("accepting a declared proposal = %d %s", code, errOut)
		}
	})
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(x, y)
}

// TestProjectSuggestOutputAndRefusals covers the JSON form, usage errors
// and proposals that do not exist, and that a project with no store yet
// proposes no base: no call is known to use it (T022 B6b).
func TestProjectSuggestOutputAndRefusals(t *testing.T) {
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	root := copyProjectFixture(t, threeManifest)
	if err := os.WriteFile(filepath.Join(root, "frontend", ".env"), []byte("VITE_API=http://localhost:3100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", ".env"), []byte("PORT=3100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runProject(t, "suggest", "--manifest", root)
	if code != 0 || !strings.Contains(out, "Proposals (0):") || !strings.Contains(out, "frontend has no stored index, so no call is known to use this address; run graphvein index --project, then suggest again") {
		t.Fatalf("suggest before indexing = %d\n%s\n%s", code, out, errOut)
	}
	if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 {
		t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = runProject(t, "suggest", "--manifest", root, "--json")
	var result struct {
		Proposals []struct {
			Number   int    `json:"number"`
			Member   string `json:"member"`
			Provider string `json:"provider"`
		} `json:"proposals"`
	}
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &result) != nil || len(result.Proposals) != 1 || result.Proposals[0].Provider != "backend" {
		t.Fatalf("suggest --json = %d\n%s\n%s", code, out, errOut)
	}
	for _, args := range [][]string{
		{"suggest", "--manifest", root, "--accept", "2"},
		{"suggest", "--manifest", root, "--accept", "-1"},
		{"suggest", "--manifest", root, "--json", "--accept", "1"},
		{"suggest", "--manifest", root, "extra"},
		{"suggest", "--manifest", filepath.Join(root, "missing")},
	} {
		if code, out, errOut := runProject(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d %q %q", args, code, out, errOut)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, config.ProjectFile)); err != nil || string(data) != threeManifest {
		t.Fatalf("a refused suggest changed the manifest: %v", err)
	}
}
