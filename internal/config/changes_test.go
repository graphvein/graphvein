package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

func TestChangesDefaultsMatchStore(t *testing.T) {
	d := DefaultChanges()
	if d.JournalRuns != store.DefaultJournalRuns || d.JournalBudget != store.DefaultJournalBudget || d.MaxPins != store.DefaultMaxPins || d.MaxRefSnapshots != store.DefaultMaxRefSnapshots {
		t.Fatalf("config defaults %+v differ from the store's", d)
	}
	if RefMarker != store.RefMarker {
		t.Fatalf("RefMarker %q differs from the store's %q", RefMarker, store.RefMarker)
	}
}

func TestChangesConfig(t *testing.T) {
	root := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := Load(root); err != nil || c.Changes != DefaultChanges() {
		t.Fatalf("missing file: %+v %v", c.Changes, err)
	}
	write("version: 1\nchanges: {journal_runs: 7}\n")
	if c, err := Load(root); err != nil || c.Changes != (Changes{JournalRuns: 7, JournalBudget: 1, MaxPins: 5, MaxRefSnapshots: 4}) {
		t.Fatalf("partial section: %+v %v", c.Changes, err)
	}
	write("version: 1\nchanges:\n  journal_runs: 10\n  journal_budget: 0.5\n  max_pins: 2\n  max_ref_snapshots: 2\n")
	if c, err := Load(root); err != nil || c.Changes != (Changes{JournalRuns: 10, JournalBudget: 0.5, MaxPins: 2, MaxRefSnapshots: 2}) {
		t.Fatalf("full section: %+v %v", c.Changes, err)
	}
	write("version: 1\nchanges: {journal_budget: 2}\n")
	if c, err := Load(root); err != nil || c.Changes.JournalBudget != 2 {
		t.Fatalf("integer budget: %+v %v", c.Changes, err)
	}
}

func TestRefMarkerRefused(t *testing.T) {
	root := t.TempDir()
	flag := "app@ref:0123"
	if _, err := ResolveRepository(root, &flag); err == nil || !strings.Contains(err.Error(), "@ref:") {
		t.Fatalf("flag with @ref: = %v", err)
	}
	dir := filepath.Join(root, "x@ref:y")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Skip("file system refuses the directory name:", err)
	}
	if _, err := ResolveRepository(dir, nil); err == nil {
		t.Fatal("directory basename with @ref: accepted")
	}
}
