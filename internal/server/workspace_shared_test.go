package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/workspace"
)

// TestWorkspaceSharedStore checks WS-57: in a store every entry shares,
// two entries in different folders that use one repository or project
// name are both refused (list, per-index requests, background check, an
// open index included), and a typed path that would add a third user of
// the name is refused before its store is read. A member that is the
// other entry's own folder, two members without a path and a store that
// is not shared refuse nothing; a member without a path conflicts with a
// folder that uses its name; renaming one folder's repository ends the
// conflict.
func TestWorkspaceSharedStore(t *testing.T) {
	shared := func(o *WorkspaceOptions) { o.SharedStore = true }
	mkdir := func(parts ...string) string {
		t.Helper()
		dir := filepath.Join(parts...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	sc := t.TempDir()
	a, b, c := mkdir(sc, "a", "app"), mkdir(sc, "b", "app"), mkdir(sc, "c", "app")

	t.Run("not shared", func(t *testing.T) {
		f := newWSFixture(t, sqliteOpen, nil)
		ea := f.register(workspace.KindRepository, "app", a)
		f.register(workspace.KindRepository, "app", b)
		if it := f.item(ea.ID); it.Conflict != nil {
			t.Fatalf("own stores conflict: %+v", it.Conflict)
		}
		if got := f.checkPath(c); got.Outcome != outcomeReady {
			t.Fatalf("own stores: %+v %+v", got, got.Refusal)
		}
	})

	t.Run("two folders, one repository name", func(t *testing.T) {
		f := newWSFixture(t, sqliteOpen, shared)
		ea := f.register(workspace.KindRepository, "app", a)
		// a is open before b is registered: the open index is refused too.
		oi, err := f.ws.acquire(context.Background(), ea.ID, true, true)
		if err != nil {
			t.Fatal(err)
		}
		f.ws.release(oi)
		f.ws.checkEntry(context.Background(), ea)
		if it := f.item(ea.ID); it.Conflict != nil || it.Freshness == nil {
			t.Fatalf("one user of the name: %+v %+v", it.Conflict, it.Freshness)
		}
		eb := f.register(workspace.KindRepository, "app", b)
		for _, pair := range [][2]workspace.Entry{{ea, eb}, {eb, ea}} {
			it := f.item(pair[0].ID)
			if it.Conflict == nil || it.Conflict.Code != "shared_store_conflict" || it.Conflict.Entry != pair[1].ID || it.Conflict.EntryPath != pair[1].Path || it.Conflict.Kind != "repository" || it.Conflict.Name != "app" {
				t.Fatalf("%s: conflict %+v", pair[0].Path, it.Conflict)
			}
			if it.Freshness != nil {
				t.Fatalf("%s: freshness %+v shown over a shared name", pair[0].Path, it.Freshness)
			}
			if !strings.Contains(it.Conflict.Message, "graphvein.yaml") || !strings.Contains(it.Conflict.Message, pair[1].Path) {
				t.Fatalf("message %q", it.Conflict.Message)
			}
			rec := f.do(http.MethodGet, "/api/v0/status?ws="+pair[0].ID, "", nil)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"shared_store_conflict"`) {
				t.Fatalf("%s: status %d %s", pair[0].Path, rec.Code, rec.Body.String())
			}
		}
		// The background check records nothing for either.
		f.ws.checkEntry(context.Background(), ea)
		f.ws.mu.Lock()
		_, recorded := f.ws.checks[ea.ID]
		f.ws.mu.Unlock()
		if recorded {
			t.Fatal("a check was recorded for an entry in conflict")
		}
		// A third folder with the name is refused before its store is read.
		opens := f.count(f.opens, "app")
		got := f.checkPath(c)
		if got.Outcome != outcomeRefused || got.Refusal == nil || got.Refusal.Row != "WS-57" || got.Refusal.Code != "shared_store_conflict" || got.Refusal.Entry == nil || got.Confirm != nil || got.Index != nil {
			t.Fatalf("third folder: %+v %+v", got, got.Refusal)
		}
		if f.count(f.opens, "app") != opens {
			t.Fatal("the check read a store under a shared name")
		}
		// Renaming b's repository ends the conflict for both.
		if err := os.WriteFile(filepath.Join(b, "graphvein.yaml"), []byte("version: 1\nrepository: app-b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if f.item(ea.ID).Conflict != nil || f.item(eb.ID).Conflict != nil {
			t.Fatal("conflict after the rename")
		}
		if rec := f.do(http.MethodGet, "/api/v0/status?ws="+ea.ID, "", nil); rec.Code != http.StatusOK {
			t.Fatalf("status after the rename: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("project members", func(t *testing.T) {
		f := newWSFixture(t, sqliteOpen, shared)
		shop := mkdir(sc, "shop")
		api := mkdir(shop, "api")
		writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n  - repository: billing\n")
		p := f.register(workspace.KindProject, "shop", shop)
		// The member's own folder registered as a repository reads the
		// same facts: no conflict.
		own := f.register(workspace.KindRepository, "api", api)
		for _, id := range []string{p.ID, own.ID} {
			if it := f.item(id); it.Conflict != nil {
				t.Fatalf("%s: %+v", id, it.Conflict)
			}
		}
		// billing has no path: it would serve whatever folder indexed
		// billing into the store as its own (T038 re-check W4), so a
		// registered folder named billing puts both in conflict.
		billing := f.register(workspace.KindRepository, "billing", mkdir(sc, "billing"))
		if it := f.item(p.ID); it.Conflict == nil || it.Conflict.Entry != billing.ID || it.Conflict.Name != "billing" || !strings.Contains(it.Conflict.Message, "without a path") {
			t.Fatalf("project with a member without a path: %+v", it.Conflict)
		}
		if it := f.item(billing.ID); it.Conflict == nil || it.Conflict.Entry != p.ID {
			t.Fatalf("billing: %+v", it.Conflict)
		}
		rec := f.do(http.MethodGet, "/api/v0/status?ws="+p.ID+"&repository=billing", "", nil)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"shared_store_conflict"`) {
			t.Fatalf("status of the member without a path: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := f.reg.Remove(billing.ID); err != nil {
			t.Fatal(err)
		}
		// A second project whose member billing has no path either
		// shares what the store holds by design: no conflict.
		mall := mkdir(sc, "mall")
		writeManifest(t, mall, "version: 1\nname: mall\nmembers:\n  - repository: billing\n")
		em := f.register(workspace.KindProject, "mall", mall)
		for _, id := range []string{p.ID, own.ID, em.ID} {
			if it := f.item(id); it.Conflict != nil {
				t.Fatalf("%s: %+v", id, it.Conflict)
			}
		}
		if _, err := f.reg.Remove(em.ID); err != nil {
			t.Fatal(err)
		}
		// Another folder named api conflicts with the project's member.
		other := f.register(workspace.KindRepository, "api", mkdir(sc, "elsewhere", "api"))
		if it := f.item(p.ID); it.Conflict == nil || it.Conflict.Entry != other.ID || it.Conflict.Name != "api" {
			t.Fatalf("project: %+v", it.Conflict)
		}
		if it := f.item(other.ID); it.Conflict == nil || it.Conflict.Kind != "repository" {
			t.Fatalf("other: %+v", it.Conflict)
		}
		// Two projects with one name share their links.
		shop2 := mkdir(sc, "shop2")
		writeManifest(t, shop2, "version: 1\nname: shop\nmembers:\n  - repository: web\n")
		got := f.checkPath(shop2)
		if got.Refusal == nil || got.Refusal.Code != "shared_store_conflict" || !strings.Contains(got.Refusal.Message, "graphvein-project.yaml") {
			t.Fatalf("second project shop: %+v %+v", got, got.Refusal)
		}
	})
}
