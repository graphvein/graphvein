package index

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/tableaccess"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// emitDiagnostics indexes the files into a temporary repository and returns
// the emitted diagnostics. The run must succeed.
func emitDiagnostics(t *testing.T, files map[string]string) []contract.Diagnostic {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var b bytes.Buffer
	opt := Options{Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
	if _, err := Emit(context.Background(), root, s, &b, opt); err != nil {
		t.Fatalf("index run failed: %v", err)
	}
	var out []contract.Diagnostic
	for _, line := range bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n")) {
		if !bytes.Contains(line, []byte(`"type":"diagnostic"`)) {
			continue
		}
		var d contract.Diagnostic
		if err := json.Unmarshal(line, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

// TestDuplicateCrateNamesIndex is the regression for a duplicate_crate_name
// diagnostic on "." that aborted the whole run: two crates named db index
// successfully and the diagnostic sits on each crate's Cargo.toml.
func TestDuplicateCrateNamesIndex(t *testing.T) {
	diagnostics := emitDiagnostics(t, map[string]string{
		"one/Cargo.toml":  "[package]\nname = \"db\"\n",
		"one/src/lib.rs":  "pub fn open() {}\n",
		"two/Cargo.toml":  "[package]\nname = \"db\"\n",
		"two/src/lib.rs":  "pub fn open() {}\n",
		"app/Cargo.toml":  "[package]\nname = \"app\"\n",
		"app/src/main.rs": "fn main() {}\n",
	})
	var files []string
	for _, d := range diagnostics {
		if d.Code == "duplicate_crate_name" {
			files = append(files, d.File)
		}
	}
	if strings.Join(files, ",") != "one/Cargo.toml,two/Cargo.toml" {
		t.Fatalf("duplicate_crate_name files %q, want one/Cargo.toml and two/Cargo.toml; all: %+v", files, diagnostics)
	}
}

// TestInvalidDiagnosticPathDoesNotAbort injects linker diagnostics whose
// files the contract rejects. Each becomes an extract_invalid_record
// diagnostic on the run (empty file) and the run still succeeds.
func TestInvalidDiagnosticPathDoesNotAbort(t *testing.T) {
	saved := tableAccessLink
	t.Cleanup(func() { tableAccessLink = saved })
	bad := []string{".", "/abs/file.rs", "../outside.rs", "a/./b.rs"}
	tableAccessLink = func(tableaccess.Input) tableaccess.Output {
		var out tableaccess.Output
		for _, file := range bad {
			out.Diagnostics = append(out.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "injected_bad_path", Message: "injected"})
		}
		return out
	}
	diagnostics := emitDiagnostics(t, map[string]string{"src/lib.rs": "pub fn f() {}\n", "Cargo.toml": "[package]\nname = \"app\"\n"})
	replaced := 0
	for _, d := range diagnostics {
		if d.Code == "injected_bad_path" {
			t.Errorf("invalid diagnostic emitted unchanged: %+v", d)
		}
		if d.Code == "extract_invalid_record" && strings.Contains(d.Message, "injected_bad_path") {
			replaced++
			if d.File != "" {
				t.Errorf("replacement on file %q, want the run (empty file)", d.File)
			}
		}
	}
	if replaced != len(bad) {
		t.Fatalf("%d replacements, want %d; diagnostics %+v", replaced, len(bad), diagnostics)
	}
}

// TestCheckedDiagnosticKeepsValidFile checks that a diagnostic invalid for a
// reason other than its path is replaced on its own file.
func TestCheckedDiagnosticKeepsValidFile(t *testing.T) {
	d := checkedDiagnostic(contract.Diagnostic{Type: "diagnostic", File: "src/lib.rs", Severity: "bogus", Code: "x", Message: "m"})
	if d.File != "src/lib.rs" || d.Code != "extract_invalid_record" {
		t.Fatalf("got %+v", d)
	}
	if err := validateRecord(d); err != nil {
		t.Fatalf("replacement invalid: %v", err)
	}
	ok := contract.Diagnostic{Type: "diagnostic", File: "", Severity: "info", Code: "x", Message: "m"}
	if checkedDiagnostic(ok) != ok {
		t.Fatal("valid diagnostic changed")
	}
}
