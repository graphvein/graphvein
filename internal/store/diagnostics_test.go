package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestDiagnosticsByFile checks the per-file diagnostics read (ADR 0048) on
// both backends: the file filter, stored order across chunks of 128 files,
// duplicate and unknown files, the empty list, repository isolation, and
// equality with Dump's diagnostics filtered in Go.
func TestDiagnosticsByFile(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		write := func(repo string, diags []contract.Diagnostic) {
			t.Helper()
			tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-04T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range diags {
				if err := tx.Diagnostic(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		diag := func(file, code string, n int) contract.Diagnostic {
			return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: fmt.Sprintf("%s %d", file, n)}
		}
		// 300 files, written in reverse name order with two diagnostics
		// each, interleaved, so ordinal order differs from file order and
		// the read spans three chunks.
		var all []contract.Diagnostic
		for round := 0; round < 2; round++ {
			for i := 299; i >= 0; i-- {
				all = append(all, diag(fmt.Sprintf("f%03d.sql", i), "c", round))
			}
		}
		write("one", all)
		write("two", []contract.Diagnostic{diag("f001.sql", "other", 0)})

		snapshot, err := s.Dump(ctx, "one")
		if err != nil {
			t.Fatal(err)
		}
		filter := func(files ...string) []contract.Diagnostic {
			want := map[string]bool{}
			for _, f := range files {
				want[f] = true
			}
			out := []contract.Diagnostic{}
			for _, d := range snapshot.Diagnostics {
				if want[d.File] {
					out = append(out, d)
				}
			}
			return out
		}
		var every []string
		for i := 0; i < 300; i++ {
			every = append(every, fmt.Sprintf("f%03d.sql", i))
		}
		for name, files := range map[string][]string{
			"every file":   every,
			"one file":     {"f007.sql"},
			"duplicates":   {"f007.sql", "f007.sql", "f200.sql"},
			"unknown file": {"missing.sql", "f150.sql"},
			"only unknown": {"missing.sql"},
		} {
			got, err := s.Diagnostics(ctx, "one", files)
			if err != nil {
				t.Fatal(err)
			}
			if want := filter(files...); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got %d diagnostics, want %d (first %v)", name, len(got), len(want), first(got))
			}
		}
		if got, err := s.Diagnostics(ctx, "one", every); err != nil || len(got) != 600 || got[0].File != "f299.sql" || got[300].Message != "f299.sql 1" {
			t.Fatalf("stored order: %d diagnostics, first %v, err %v", len(got), first(got), err)
		}
		for _, files := range [][]string{nil, {}} {
			got, err := s.Diagnostics(ctx, "one", files)
			if err != nil || got == nil || len(got) != 0 {
				t.Errorf("empty list %v: %v, %v", files, got, err)
			}
		}
		got, err := s.Diagnostics(ctx, "two", []string{"f001.sql", "f002.sql"})
		if err != nil || len(got) != 1 || got[0].Code != "other" {
			t.Errorf("repository isolation: %v, %v", got, err)
		}
		if got, err := s.Diagnostics(ctx, "absent", []string{"f001.sql"}); err != nil || len(got) != 0 {
			t.Errorf("unknown repository: %v, %v", got, err)
		}
	})
}

func first(ds []contract.Diagnostic) any {
	if len(ds) == 0 {
		return nil
	}
	return ds[0]
}

// TestDiagnosticsWithoutFile checks the read of diagnostics whose file is
// not stored (ADR 0048, T018b1 review): scanner diagnostics on paths
// that were not read and empty-file diagnostics come back in stored order,
// diagnostics of stored files and of other repositories do not.
func TestDiagnosticsWithoutFile(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		write := func(repo string, files []string, diags []contract.Diagnostic) {
			t.Helper()
			tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-04T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: f, Language: "toml", Precision: "best_effort", ContentHash: contract.ContentHash([]byte(f))}); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range diags {
				if err := tx.Diagnostic(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		diag := func(file, code string) contract.Diagnostic {
			return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: code, Message: code + " " + file}
		}
		write("one", []string{"Cargo.toml", "a/Cargo.toml"}, []contract.Diagnostic{
			diag("a/Cargo.toml", "schema_manifest_unreadable"),
			diag("b/Cargo.toml", "symlink"),
			diag("Cargo.toml", "unresolved_import"),
			diag("", "invalid_path"),
			diag("c", "scan_read"),
		})
		write("two", nil, []contract.Diagnostic{diag("d/Cargo.toml", "file_read")})
		got, err := s.DiagnosticsWithoutFile(ctx, "one")
		if err != nil {
			t.Fatal(err)
		}
		want := []contract.Diagnostic{diag("b/Cargo.toml", "symlink"), diag("", "invalid_path"), diag("c", "scan_read")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got, err := s.DiagnosticsWithoutFile(ctx, "absent"); err != nil || got == nil || len(got) != 0 {
			t.Errorf("unknown repository: %v, %v", got, err)
		}
	})
}

// TestDiagnosticsWithCodes checks the read of diagnostics by code on both
// backends: exact codes and prefixes on any file (stored or not), stored
// order, codes that only share a prefix's letters in another case or
// position, repository isolation, and the empty filter.
func TestDiagnosticsWithCodes(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		write := func(repo string, diags []contract.Diagnostic) {
			t.Helper()
			tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-04T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: "a.py", Language: "python", Precision: "best_effort", ContentHash: contract.ContentHash([]byte("a"))}); err != nil {
				t.Fatal(err)
			}
			for _, d := range diags {
				if err := tx.Diagnostic(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		diag := func(file, code string) contract.Diagnostic {
			return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: code + ` "code":"schema_x" ` + file}
		}
		write("one", []contract.Diagnostic{
			diag("a.py", "unresolved_calls"),
			diag("b.py", "schema_model_unread"),
			diag("a.py", "parse_failed"),
			diag("", "file_read"),
			diag("a.py", "SCHEMA_upper"),
			diag("a.py", "not_schema_x"),
			diag("a.py", "schema"),
			diag("a.py", "schema_model_core_tables"),
			diag("a.py", "parse_failed_twice"),
		})
		write("two", []contract.Diagnostic{diag("a.py", "schema_model_unread")})
		got, err := s.DiagnosticsWithCodes(ctx, "one", []string{"parse_failed", "file_read"}, []string{"schema_"})
		if err != nil {
			t.Fatal(err)
		}
		want := []contract.Diagnostic{diag("b.py", "schema_model_unread"), diag("a.py", "parse_failed"), diag("", "file_read"), diag("a.py", "schema_model_core_tables")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got, err := s.DiagnosticsWithCodes(ctx, "one", nil, []string{"schema_model_c"}); err != nil || len(got) != 1 || got[0].Code != "schema_model_core_tables" {
			t.Errorf("prefix only: %v, %v", got, err)
		}
		if got, err := s.DiagnosticsWithCodes(ctx, "one", []string{"unresolved_calls"}, nil); err != nil || len(got) != 1 || got[0].Code != "unresolved_calls" {
			t.Errorf("codes only: %v, %v", got, err)
		}
		if got, err := s.DiagnosticsWithCodes(ctx, "one", nil, nil); err != nil || got == nil || len(got) != 0 {
			t.Errorf("empty filter: %v, %v", got, err)
		}
		if got, err := s.DiagnosticsWithCodes(ctx, "absent", []string{"parse_failed"}, []string{"schema_"}); err != nil || got == nil || len(got) != 0 {
			t.Errorf("unknown repository: %v, %v", got, err)
		}
	})
}

// TestDiagnosticsWithMessage checks the read of one code's diagnostics by
// message text on both backends (T018g5 Revision 2, C3): the code must
// match exactly, the text anywhere in the message (JSON-escaped characters
// included), in stored order, per repository.
func TestDiagnosticsWithMessage(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		write := func(repo string, diags []contract.Diagnostic) {
			t.Helper()
			tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-04T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range diags {
				if err := tx.Diagnostic(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		diag := func(file, code, msg string) contract.Diagnostic {
			return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg}
		}
		const text = ` import (GORM v1): found, not read`
		v1 := diag("a.go", "schema_model_unread", `a.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read`)
		quoted := diag("b.go", "schema_model_unread", `b.go:1: "x" import (GORM v1): found, not read`)
		write("one", []contract.Diagnostic{
			diag("c.go", "schema_model_unread", "c.go:1: AutoMigrate site not read"),
			v1,
			diag("a.go", "schema_model_not_proven", "a.go:3: x"+text),
			quoted,
			diag("d.go", "schema_model_unread", "D import (GORM v1) found, not read"),
		})
		write("two", []contract.Diagnostic{diag("a.go", "schema_model_unread", "a.go:1: y"+text)})
		got, err := s.DiagnosticsWithMessage(ctx, "one", "schema_model_unread", text)
		if err != nil {
			t.Fatal(err)
		}
		if want := []contract.Diagnostic{v1, quoted}; !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got, err := s.DiagnosticsWithMessage(ctx, "one", "schema_model_unread", `"x"`); err != nil || len(got) != 1 || got[0].File != "b.go" {
			t.Errorf("escaped text: %v, %v", got, err)
		}
		if got, err := s.DiagnosticsWithMessage(ctx, "absent", "schema_model_unread", text); err != nil || got == nil || len(got) != 0 {
			t.Errorf("unknown repository: %v, %v", got, err)
		}
	})
}
