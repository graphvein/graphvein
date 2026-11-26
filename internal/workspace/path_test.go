package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// caseInsensitive reports whether dir's file system folds letter case.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// TestSamePath checks WS-4's path comparison on every platform: equal
// strings are one folder; spellings that differ in case are one folder
// only where the file system says so, never from the text alone.
func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	if dir, _ = Resolve(dir); dir == "" {
		t.Fatal("resolve temp dir")
	}
	folder := filepath.Join(dir, "Foo")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "FOO")
	insensitive := caseInsensitive(t, dir)
	if !SamePath(folder, folder) {
		t.Fatal("a path is not the same as itself")
	}
	if got := SamePath(folder, other); got != insensitive {
		t.Fatalf("SamePath(%q, %q) = %v on a file system with case folding %v", folder, other, got, insensitive)
	}
	if SamePath(folder, filepath.Join(dir, "Bar")) {
		t.Fatal("different names are the same")
	}
	gone := filepath.Join(dir, "gone")
	if SamePath(gone, filepath.Join(dir, "GONE")) {
		t.Fatal("a missing folder was matched by its text")
	}
	child := filepath.Join(other, "child")
	if got := Inside(child, folder); got != insensitive {
		t.Fatalf("Inside(%q, %q) = %v, case folding %v", child, folder, got, insensitive)
	}
	if !Inside(filepath.Join(folder, "x"), folder) || Inside(folder, folder) || Inside(folder+"x", folder) {
		t.Fatal("Inside with the exact spelling")
	}
}

// TestResolveCase checks WS-23: on a case-insensitive file system a
// folder typed in another case resolves to the file system's own casing,
// so registering it twice gives one entry. On a case-sensitive one the
// other spelling is no folder at all.
func TestResolveCase(t *testing.T) {
	dir, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "MixedCase")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	typed := filepath.Join(dir, "mixedcase")
	got, err := Resolve(typed)
	if !caseInsensitive(t, dir) {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("case-sensitive: %q %v", got, err)
		}
		return
	}
	if err != nil || got != folder {
		t.Fatalf("Resolve(%q) = %q %v, want %q", typed, got, err, folder)
	}
	r := Open(filepath.Join(t.TempDir(), "workspace.yaml"))
	a, _, err := r.Add(Entry{Kind: KindRepository, Name: "m", Path: folder})
	if err != nil {
		t.Fatal(err)
	}
	// An entry written with another spelling (before this rule, or by
	// hand) still matches the folder.
	b, added, err := r.Add(Entry{Kind: KindRepository, Name: "m", Path: typed})
	if err != nil || added || b.ID != a.ID {
		t.Fatalf("second spelling: %+v %v %v", b, added, err)
	}
}

// TestRealCaseRule checks the casing rule on every platform with a fake
// file system: a component listed in another case and confirmed the same
// folder takes the listed spelling; an exact listing, an unlistable
// folder, no confirmation or two candidates keep the typed spelling.
func TestRealCaseRule(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.VolumeName(os.TempDir()) + sep
	p := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	listing := map[string][]string{
		root:              {"Users", "data"},
		p("Users"):        {"Ann", "ann2"},
		p("data"):         {"X", "x"},
		p("Users", "Ann"): {"Src"},
	}
	list := func(dir string) ([]string, error) {
		if names, ok := listing[dir]; ok {
			return names, nil
		}
		return nil, errors.New("cannot list")
	}
	yes := func(a, b string) bool { return true }
	no := func(a, b string) bool { return false }
	for _, c := range []struct {
		in, want string
		same     func(a, b string) bool
	}{
		{p("users", "ANN", "src"), p("Users", "Ann", "Src"), yes},
		{p("Users", "Ann", "Src"), p("Users", "Ann", "Src"), yes},
		{p("users", "ann"), p("users", "ann"), no},
		{p("data", "X"), p("data", "X"), yes},
		{p("DATA", "X"), p("data", "X"), yes},
		{p("data", "Y"), p("data", "Y"), yes},
		{p("Data", "xx"), p("data", "xx"), yes},
	} {
		if got := realCaseWith(c.in, list, c.same); got != c.want {
			t.Errorf("realCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Two listed names folding to the typed one: the typed one is kept.
	listing[p("data")] = []string{"X", "x"}
	if got := realCaseWith(p("data", "x"), list, yes); got != p("data", "x") {
		t.Errorf("exact listing beside a fold: %q", got)
	}
	listing[p("data")] = []string{"Ab", "aB"}
	if got := realCaseWith(p("data", "AB"), list, yes); got != p("data", "AB") {
		t.Errorf("two candidates: %q", got)
	}
}
