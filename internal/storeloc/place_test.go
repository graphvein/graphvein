package storeloc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPlanPlaceAndRecord: a new folder's place is planned without
// writing; recording a typed folder adds a stores.yaml entry, recording
// the default or the in-folder place removes it; a folder with an index
// keeps it.
func TestPlanPlaceAndRecord(t *testing.T) {
	f := newFixture(t)
	key := Key(f.folder)
	def, err := f.r.PlanPlace(f.folder, PlaceDefault, "", nil)
	if err != nil || def.Origin != OriginDefault || def.Path != filepath.Join(f.data, "graphvein", "stores", key, StoreFile) {
		t.Fatalf("default %+v %v", def, err)
	}
	in, err := f.r.PlanPlace(f.folder, PlaceInside, "", nil)
	if err != nil || in.Origin != OriginInFolder || in.Path != filepath.Join(f.folder, InFolderDir, StoreFile) {
		t.Fatalf("inside %+v %v", in, err)
	}
	dest := t.TempDir()
	typed, err := f.r.PlanPlace(f.folder, PlaceFolder, dest, nil)
	if err != nil || typed.Origin != OriginMapped {
		t.Fatalf("folder %+v %v", typed, err)
	}
	if _, err := os.Stat(f.r.StoresFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("planning wrote stores.yaml")
	}
	if err := f.r.RecordPlace(typed); err != nil {
		t.Fatal(err)
	}
	if loc := f.resolve(t, Scope{}); loc.Origin != OriginMapped || loc.Path != typed.Path {
		t.Fatalf("after record %+v", loc)
	}
	if err := f.r.RecordPlace(in); err != nil {
		t.Fatal(err)
	}
	if loc := f.resolve(t, Scope{}); loc.Origin != OriginDefault {
		t.Fatalf("entry kept %+v", loc)
	}
	// A folder with an index keeps it.
	newStore(t, def.Path)
	var re *RefusalError
	if _, err := f.r.PlanPlace(f.folder, PlaceInside, "", nil); !errors.As(err, &re) || re.Code != "store_fixed" {
		t.Fatalf("existing index: %v", err)
	}
	if loc, err := f.r.PlanPlace(f.folder, PlaceDefault, "", nil); err != nil || !loc.Exists {
		t.Fatalf("its own place %+v %v", loc, err)
	}
	// Team mode has no place.
	f.env[EnvPostgres] = "postgres://x"
	if _, err := f.r.PlanPlace(f.folder, PlaceDefault, "", nil); !errors.As(err, &re) || re.Code != "team_mode" {
		t.Fatalf("team mode: %v", err)
	}
}

// TestMoved: Moved accepts the place a location still resolves to and
// names both places, wrapping ErrMoved, once it resolves elsewhere.
func TestMoved(t *testing.T) {
	f := newFixture(t)
	loc := f.resolve(t, Scope{})
	if err := f.r.Moved(loc, Scope{}); err != nil {
		t.Fatal(err)
	}
	newStore(t, filepath.Join(f.folder, InFolderDir, StoreFile))
	err := f.r.Moved(loc, Scope{})
	if !errors.Is(err, ErrMoved) {
		t.Fatalf("moved: %v", err)
	}
}

// TestCheckStoresDir: the folder for new indexes is refused on the rules
// that need no indexed folder, with their codes.
func TestCheckStoresDir(t *testing.T) {
	reg := t.TempDir()
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for dir, code := range map[string]string{
		"rel":                      "not_absolute",
		filepath.Join(reg, "x"):    "inside_registered",
		file:                       "not_a_directory",
		filepath.Join(reg, ".git"): "inside_git",
	} {
		var re *RefusalError
		if err := CheckStoresDir(dir, []string{reg}); !errors.As(err, &re) || re.Code != code {
			t.Errorf("%s: %v, want %s", dir, err, code)
		}
	}
	if err := CheckStoresDir(filepath.Join(t.TempDir(), "new"), []string{reg}); err != nil {
		t.Fatal(err)
	}
}
