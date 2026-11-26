package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryPath checks WS-1: GRAPHVEIN_WORKSPACE_FILE wins, otherwise
// workspace.yaml in the graphvein folder of the user configuration
// directory; a registry without a file is empty, and an unavailable one
// reports its reason on every call.
func TestRegistryPath(t *testing.T) {
	override := filepath.Join(t.TempDir(), "w.yaml")
	t.Setenv(EnvFile, override)
	if p, err := DefaultPath(); err != nil || p != override {
		t.Fatalf("override: %q %v", p, err)
	}
	t.Setenv(EnvFile, "")
	dir, err := os.UserConfigDir()
	if err == nil {
		if p, err := DefaultPath(); err != nil || p != filepath.Join(dir, "graphvein", FileName) {
			t.Fatalf("default: %q %v", p, err)
		}
	}
	entries, err := Open(override).Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing file: %v %v", entries, err)
	}
	r := Unavailable(os.ErrNotExist)
	if _, err := r.Entries(); err == nil {
		t.Fatal("an unavailable registry listed entries")
	}
	if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "a", Path: t.TempDir()}); err == nil {
		t.Fatal("an unavailable registry added an entry")
	}
}

// TestRegistryFormat checks WS-2: version 1 with kind, name, absolute
// path and time added; a damaged file, another version, an unknown field
// or an invalid entry is an error and is never rewritten.
func TestRegistryFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.yaml")
	r := Open(path)
	folder := t.TempDir()
	e, added, err := r.Add(Entry{Kind: KindRepository, Name: "api", Path: folder})
	if err != nil || !added || e.ID == "" || e.AddedAt == "" {
		t.Fatalf("add: %+v %v %v", e, added, err)
	}
	if again, added, err := r.Add(Entry{Kind: KindRepository, Name: "api", Path: folder}); err != nil || added || again.ID != e.ID || again.AddedAt != e.AddedAt {
		t.Fatalf("second add: %+v %v %v", again, added, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"version: 1", "kind: repository", "name: api", "path: " + folder, "added_at: "} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file lacks %q:\n%s", want, data)
		}
	}
	for name, content := range map[string]string{
		"damaged":       "version: 1\nentries: [\n",
		"version":       "version: 2\nentries: []\n",
		"unknown field": "version: 1\nentries:\n  - kind: repository\n    name: a\n    path: " + folder + "\n    store: x\n",
		"relative path": "version: 1\nentries:\n  - kind: repository\n    name: a\n    path: rel\n",
		"kind":          "version: 1\nentries:\n  - kind: folder\n    name: a\n    path: " + folder + "\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Entries(); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "b", Path: folder}); err == nil {
			t.Errorf("%s: added to a file that does not read", name)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Errorf("%s: the file was rewritten", name)
		}
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if entries, err := r.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("empty file: %v %v", entries, err)
	}
}

// TestRegistryAtomicWrite checks WS-3: every write replaces the file
// through a temporary file in the same folder, leaving no temporary file
// behind; a remove keeps the other entries.
func TestRegistryAtomicWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "workspace.yaml")
	r := Open(path)
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		e, _, err := r.Add(Entry{Kind: KindRepository, Name: name, Path: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	if removed, err := r.Remove(ids[1]); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, err := r.Remove(ids[1]); err != nil || removed {
		t.Fatalf("second remove: %v %v", removed, err)
	}
	entries, err := r.Entries()
	if err != nil || len(entries) != 2 || entries[0].ID != ids[0] || entries[1].ID != ids[2] {
		t.Fatalf("entries %+v %v", entries, err)
	}
	// Only the registry and its lock file remain; the lock file is kept
	// on purpose (see lock.go).
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 || files[0].Name() != FileName || files[1].Name() != FileName+LockSuffix {
		t.Fatalf("folder holds %v %v", files, err)
	}
}

// TestEntryID checks WS-4: the ID is the name, a tilde and eight hex
// digits of kind, path and name; it is the same on every read and differs
// for the same name in another folder or of another kind.
func TestEntryID(t *testing.T) {
	a := EntryID(KindRepository, "/src/api", "api")
	if a != EntryID(KindRepository, "/src/api", "api") || !strings.HasPrefix(a, "api~") || len(a) != len("api~")+8 {
		t.Fatalf("id %q", a)
	}
	for _, other := range []string{EntryID(KindRepository, "/other/api", "api"), EntryID(KindProject, "/src/api", "api"), EntryID(KindRepository, "/src/api", "api2")} {
		if other == a {
			t.Fatalf("id %q not unique", a)
		}
	}
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	folder := t.TempDir()
	e, _, err := Open(path).Add(Entry{Kind: KindProject, Name: "shop", Path: folder})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := Open(path).Entries()
	if err != nil || len(entries) != 1 || entries[0].ID != e.ID || e.ID != EntryID(KindProject, folder, "shop") {
		t.Fatalf("reread %+v %v", entries, err)
	}
	// One folder is one entry: adding it again under a new name (its
	// manifest or graphvein.yaml was renamed) or as the other kind
	// returns the stored entry and its id (T038 review follow-up).
	for _, again := range []Entry{{Kind: KindProject, Name: "shop2", Path: folder}, {Kind: KindRepository, Name: "shop", Path: folder}} {
		got, added, err := Open(path).Add(again)
		if err != nil || added || got.ID != e.ID || got.Name != "shop" {
			t.Fatalf("add %+v again: %+v added=%v %v", again, got, added, err)
		}
	}
	if entries, err := Open(path).Entries(); err != nil || len(entries) != 1 {
		t.Fatalf("a second entry for one folder: %+v %v", entries, err)
	}
}

// TestRegistryKeepsGoneFolders checks WS-5: an entry whose folder is gone
// is still listed; nothing drops it but Remove. Sort lists projects first,
// then by name and path.
func TestRegistryKeepsGoneFolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	r := Open(path)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "web", Path: gone}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Add(Entry{Kind: KindProject, Name: "zeta", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "api", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Entries()
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries %+v %v", entries, err)
	}
	Sort(entries)
	if entries[0].Name != "zeta" || entries[1].Name != "api" || entries[2].Name != "web" {
		t.Fatalf("order %+v", entries)
	}
}
