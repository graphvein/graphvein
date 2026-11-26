package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// runSettings returns the settings record of every run of repo by run ID.
func runSettings(t *testing.T, s store.Store, repo string) map[string]string {
	t.Helper()
	runs, err := s.Runs(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range runs {
		out[r.ID] = r.Settings
	}
	return out
}

// TestRunSettings stores a run's settings record in its own column (row
// ST-41): Runs and the committed transaction return it, a run without one
// reads back empty (unknown), the option wrapper passes it on, and ref runs
// record it too.
func TestRunSettings(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		const record = `{"format":1,"hash":"sha256:x","values":{"index.shape_depth":4}}`
		with, _ := mustWrite(t, s, "r", baseGraph(), store.RunOptions{Settings: record})
		if with.Settings != record {
			t.Fatalf("committed run settings = %q", with.Settings)
		}
		without, _ := mustWrite(t, s, "r", baseGraph())
		wrapped, _ := mustWrite(t, store.WithRunOptions(s, store.RunOptions{Settings: record + " "}), "r", baseGraph())
		got := runSettings(t, s, "r")
		want := map[string]string{with.ID: record, without.ID: "", wrapped.ID: record + " "}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("run settings = %v, want %v", got, want)
		}
		// The record stays out of the run payload, so every other read of
		// a run is unchanged.
		runs, err := s.Runs(context.Background(), "r")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.Header.Repository != "r" || r.FinishedAt == "" {
				t.Fatalf("run %+v", r)
			}
		}
		ref, _ := mustWrite(t, s, "r"+"@ref:c1", baseGraph(), store.RunOptions{Settings: record, Namespace: &store.Namespace{Repository: "r", Commit: "c1", MaxSnapshots: 3}})
		if got := runSettings(t, s, "r@ref:c1"); got[ref.ID] != record {
			t.Fatalf("ref run settings = %v", got)
		}
	})
}

// TestRunSettingsFromVersion5 rebuilds a version-5 store: Open adds the
// column, the old runs read back without settings (unknown, never a
// record), and the next run records its own.
func TestRunSettingsFromVersion5(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		old, _ := mustWrite(t, s, "r", baseGraph())
		dropRunSettings(t, db)
		if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=5"); err != nil {
			t.Fatal(err)
		}
		dialect := "sqlite"
		if strings.HasSuffix(t.Name(), "/postgres") {
			dialect = "postgres"
		}
		migrated, err := sqlstore.Open(ctx, db, dialect)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version); err != nil || version != migrations.Version || version < 6 {
			t.Fatalf("migrated version = %d %v", version, err)
		}
		next, _ := mustWrite(t, migrated, "r", baseGraph(), store.RunOptions{Settings: "{}"})
		got := runSettings(t, migrated, "r")
		if len(got) != 2 || got[old.ID] != "" || got[next.ID] != "{}" {
			t.Fatalf("settings after migration = %v", got)
		}
	})
}

// TestOpenReadVersion5 keeps OpenRead's refusal of an older schema: the
// query commands never migrate, so a version-5 store needs one index run.
func TestOpenReadVersion5(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, s, "r", baseGraph())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	dropRunSettings(t, db)
	if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=5"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := sqlite.OpenRead(ctx, path); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("schema version 5 requires indexing with schema %d", migrations.Version)) {
		t.Fatalf("OpenRead on version 5 = %v", err)
	}
}
