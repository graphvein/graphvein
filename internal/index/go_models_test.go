package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// TestGoModelPayloadCache checks the Go model payload's cache wiring (ADR
// 0063): a second run replays it, and an entry with another payload
// version, a damaged payload or no payload (written before extractors-36) is
// a cache miss whose re-extraction restores the payload.
func TestGoModelPayloadCache(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.22\n\nrequire gorm.io/gorm v1.31.2\n")
	write("model/user.go", "package model\n\ntype User struct {\n\tID   uint `gorm:\"primaryKey\"`\n\tName string\n}\n")
	write("db/db.go", "package db\n\nimport (\n\t\"example.com/app/model\"\n\t\"gorm.io/gorm\"\n)\n\nfunc Migrate(db *gorm.DB) error {\n\treturn db.AutoMigrate(&model.User{})\n}\n")
	s, opt := open(t), options(t)
	if _, summary, _ := emit(t, root, s, opt); summary.Parsed != 2 {
		t.Fatalf("first run parsed %d files", summary.Parsed)
	}
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, f := range snapshot.Files {
		hashes[f.Path] = f.ContentHash
	}
	version := fmt.Sprintf("%s/max=%d", index.Version, 2<<20)
	read := func(path string) facts.Facts {
		t.Helper()
		data, err := s.FactCache(context.Background(), "fixture", path, hashes[path], version)
		if err != nil || len(data) == 0 {
			t.Fatal(path, err)
		}
		var cached facts.Facts
		if err := json.Unmarshal(data, &cached); err != nil {
			t.Fatal(err)
		}
		return cached
	}
	for path, gated := range map[string]bool{"model/user.go": false, "db/db.go": true} {
		g := read(path).Go
		if g == nil || g.Version != facts.GoModelsVersion || g.Gated != gated {
			t.Fatalf("%s: cached payload %+v", path, g)
		}
	}
	if len(read("model/user.go").Go.Structs) != 1 || len(read("db/db.go").Go.Funcs) != 1 {
		t.Fatal("cached payload lacks the struct or the function")
	}
	if _, summary, _ := emit(t, root, s, opt); summary.Parsed != 0 || summary.Reused != 2 {
		t.Fatalf("second run parsed %d, reused %d", summary.Parsed, summary.Reused)
	}
	want := read("db/db.go").Go
	damage := map[string]func(*facts.Facts){
		"version":    func(f *facts.Facts) { f.Go.Version = facts.GoModelsVersion + 1 },
		"no payload": func(f *facts.Facts) { f.Go = nil },
		"site kind":  func(f *facts.Facts) { f.Go.Funcs[0].Sites[0].Call.Kind = "lambda" },
		"ungated":    func(f *facts.Facts) { f.Go.Gated = false },
	}
	for name, mutate := range damage {
		cached := read("db/db.go")
		mutate(&cached)
		broken, err := json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveFactCache(context.Background(), "fixture", "db/db.go", hashes["db/db.go"], version, broken); err != nil {
			t.Fatal(err)
		}
		if _, summary, _ := emit(t, root, s, opt); summary.Parsed != 1 {
			t.Fatalf("%s: damaged Go payload replayed (%+v)", name, summary)
		}
		if got := read("db/db.go").Go; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: re-extracted payload differs", name)
		}
	}
}
