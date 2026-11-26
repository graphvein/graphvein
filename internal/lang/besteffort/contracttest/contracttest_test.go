package contracttest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestRowLiterals: only string literals count as branches, never a row id
// in a comment, and only ids of the given language, the shared rows and D.
func TestRowLiterals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.go")
	source := `package x

// t.trace("CS-I9") in a comment is not a branch, nor is CS-I8.
func f(trace func(string)) string {
	trace("CS-I1")
	trace(` + "`BE-8`" + `)
	_ = "JV-I2"
	_ = "CS-I1 and more"
	return "D"
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := RowLiterals([]string{path}, "CS")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]bool{"CS-I1": true, "BE-8": true, "D": true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
