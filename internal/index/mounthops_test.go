package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestMountHopsGolden indexes the mount-hops fixture (T029 FU-2): chi
// Route, gin Group, net/http StripPrefix and wrapper hops, an Express use
// and FastAPI include_router and router prefixes that are not constants,
// and Axum nest prefixes that are not constants (T029 FU-3).
// Every route below a hop stores the hop's key, hash and its paths below
// the hop (route attribute unresolved_mounts); routes with a path read
// from code store none.
func TestMountHopsGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/mount-hops")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/mount-hops.golden.json", goRoutesSnapshot(t, snapshot))
}

// TestMountHopRegistrationCoversAbove: the registration of a hop whose
// routers above are proven hashes the composition above it (T029 FU-2
// review): changing the read prefix "/outer" above Nested's
// r.Route(base) changes that hop's registration, so a value set for it
// is "registration changed"; hops elsewhere keep theirs.
func TestMountHopRegistrationCoversAbove(t *testing.T) {
	hashes := func(edit bool) map[string]string {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/mount-hops")); err != nil {
			t.Fatal(err)
		}
		if edit {
			file := filepath.Join(root, "goapp", "chi.go")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(`r.Route("/outer"`), []byte(`r.Route("/outer2"`), 1)
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s, opt := open(t), options(t)
		emit(t, root, s, opt)
		snapshot, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, n := range snapshot.Nodes {
			data, _ := json.Marshal(n.Attributes["unresolved_mounts"])
			var mounts []struct {
				Key, Registration string
				Paths             []struct{ Above string }
			}
			_ = json.Unmarshal(data, &mounts)
			for _, m := range mounts {
				out[m.Key] = m.Registration
				if len(m.Paths) > 0 {
					out[m.Key+" above"] = m.Paths[0].Above
				}
			}
		}
		return out
	}
	before, after := hashes(false), hashes(true)
	nested, router := "mount:goapp/chi.go#Nested%20r.Route%28base%29", "mount:goapp/chi.go#Router%20r.Route%28base%29"
	if before[nested] == "" || before[nested] == after[nested] {
		t.Errorf("Nested hop registration %q -> %q: want a change", before[nested], after[nested])
	}
	if before[nested+" above"] != "/outer" || after[nested+" above"] != "/outer2" {
		t.Errorf("Nested above %q -> %q", before[nested+" above"], after[nested+" above"])
	}
	if before[router] == "" || before[router] != after[router] {
		t.Errorf("Router hop registration %q -> %q: want it unchanged", before[router], after[router])
	}
}
