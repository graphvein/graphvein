package storeloc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/workspace"
)

// fixture is a resolver over temporary folders only: its environment is
// a map (never the process's), stores.yaml and the data folder are under
// t.TempDir, and folder is an existing folder to index.
type fixture struct {
	r      *Resolver
	env    map[string]string
	folder string
	data   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{env: map[string]string{}, data: filepath.Join(base, "data")}
	f.r = &Resolver{
		Getenv:     func(k string) string { return f.env[k] },
		StoresFile: filepath.Join(base, "config", StoresFileName),
		DataDir:    f.data,
	}
	folder := filepath.Join(base, "src", "demo")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := workspace.Resolve(folder)
	if err != nil {
		t.Fatal(err)
	}
	f.folder = resolved
	return f
}

func (f *fixture) resolve(t *testing.T, scope Scope) Location {
	t.Helper()
	loc, err := f.r.Resolve(f.folder, scope)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// newStore creates an empty, migrated SQLite store at p.
func newStore(t *testing.T, p string) {
	t.Helper()
	s, err := sqlite.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUserDataDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"windows", map[string]string{"LocalAppData": `C:\Users\dev\AppData\Local`, "AppData": `C:\Users\dev\AppData\Roaming`}, `C:\Users\dev\AppData\Local`},
		{"darwin", map[string]string{"HOME": "/Users/dev"}, filepath.Join("/Users/dev", "Library", "Application Support")},
		{"linux", map[string]string{"HOME": "/home/dev", "XDG_DATA_HOME": "/xdg/data"}, "/xdg/data"},
		{"linux", map[string]string{"HOME": "/home/dev", "XDG_DATA_HOME": "relative/data"}, filepath.Join("/home/dev", ".local", "share")},
		{"freebsd", map[string]string{"HOME": "/home/dev"}, filepath.Join("/home/dev", ".local", "share")},
	} {
		if runtime.GOOS == "windows" && c.env["XDG_DATA_HOME"] != "" {
			// "/xdg/data" has no drive, so it is not absolute on a
			// Windows host; the rule is checked on the other hosts.
			continue
		}
		got, err := UserDataDir(c.goos, env(c.env))
		if err != nil || got != c.want {
			t.Errorf("%s %v: %q, %v; want %q", c.goos, c.env, got, err, c.want)
		}
	}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if _, err := UserDataDir(goos, env(nil)); err == nil {
			t.Errorf("%s without variables: no error", goos)
		}
	}
}

func TestKey(t *testing.T) {
	a := filepath.Join(string(filepath.Separator)+"src", "one", "demo")
	b := filepath.Join(string(filepath.Separator)+"src", "two", "demo")
	if Key(a) != Key(a) || Key(a) == Key(b) {
		t.Fatalf("Key(%s)=%s Key(%s)=%s: want stable and distinct", a, Key(a), b, Key(b))
	}
	if !strings.HasPrefix(Key(a), "demo-") || len(Key(a)) != len("demo-")+8 {
		t.Fatalf("Key = %s, want demo-<8 hex>", Key(a))
	}
	for name, want := range map[string]string{"my app": "my-app", "..": "folder", "naïve": "na-ve", strings.Repeat("x", 60): strings.Repeat("x", 40)} {
		if got := safeName(name); got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestResolveOrder walks the lookup steps of ADR 0093 one by one.
func TestResolveOrder(t *testing.T) {
	f := newFixture(t)
	platform := filepath.Join(f.data, "graphvein", "stores")

	// Step 5: nothing exists; the platform folder, nothing written.
	loc := f.resolve(t, Scope{CommandLine: true})
	want := filepath.Join(platform, Key(f.folder), StoreFile)
	if loc.Origin != OriginDefault || loc.Path != want || loc.Exists || loc.DirSource != DirPlatform || loc.Inside() {
		t.Fatalf("default: %+v, want %s", loc, want)
	}
	if _, err := os.Stat(f.data); !os.IsNotExist(err) {
		t.Fatalf("Resolve wrote %s", f.data)
	}

	// GRAPHVEIN_STORE_DIR replaces the folder.
	envDir := filepath.Join(filepath.Dir(f.data), "envstores")
	f.env[EnvStoreDir] = envDir
	loc = f.resolve(t, Scope{})
	if loc.Path != filepath.Join(envDir, Key(f.folder), StoreFile) || loc.DirSource != DirEnv {
		t.Fatalf("GRAPHVEIN_STORE_DIR: %+v", loc)
	}
	delete(f.env, EnvStoreDir)

	// stores.yaml default_dir, below the variable.
	yamlDir := filepath.Join(filepath.Dir(f.data), "yamlstores")
	if err := f.r.SetDefaultDir(yamlDir); err != nil {
		t.Fatal(err)
	}
	loc = f.resolve(t, Scope{})
	if loc.Path != filepath.Join(yamlDir, Key(f.folder), StoreFile) || loc.DirSource != DirStoresFile {
		t.Fatalf("default_dir: %+v", loc)
	}
	f.env[EnvStoreDir] = envDir
	if loc = f.resolve(t, Scope{}); loc.DirSource != DirEnv {
		t.Fatalf("GRAPHVEIN_STORE_DIR over default_dir: %+v", loc)
	}
	delete(f.env, EnvStoreDir)
	if err := f.r.SetDefaultDir(""); err != nil {
		t.Fatal(err)
	}

	// Step 4: a stores.yaml entry, before and after its file exists.
	chosen := filepath.Join(filepath.Dir(f.data), "chosen", "demo")
	if err := f.r.Assign(f.folder, chosen); err != nil {
		t.Fatal(err)
	}
	loc = f.resolve(t, Scope{})
	if loc.Origin != OriginMapped || loc.Path != filepath.Join(chosen, StoreFile) || loc.Exists {
		t.Fatalf("mapped: %+v", loc)
	}
	newStore(t, loc.Path)
	if loc = f.resolve(t, Scope{}); loc.Origin != OriginMapped || !loc.Exists {
		t.Fatalf("mapped, existing: %+v", loc)
	}
	// Assigning the default place removes the entry.
	if err := f.r.Assign(f.folder, filepath.Join(platform, Key(f.folder))); err != nil {
		t.Fatal(err)
	}
	if file, _ := f.r.readStores(); len(file.Entries) != 0 {
		t.Fatalf("entries after assigning the default place: %+v", file.Entries)
	}
	os.RemoveAll(chosen)

	// Step 3: an in-folder store.
	inFolder := filepath.Join(f.folder, InFolderDir, StoreFile)
	newStore(t, inFolder)
	loc = f.resolve(t, Scope{CommandLine: true})
	if loc.Origin != OriginInFolder || loc.Path != inFolder || !loc.Exists || !loc.Inside() {
		t.Fatalf("in folder: %+v", loc)
	}

	// Step 2: GRAPHVEIN_SQLITE_PATH, for the command-line folder only.
	envFile := filepath.Join(filepath.Dir(f.data), "env.db")
	f.env[EnvSQLitePath] = envFile
	if loc = f.resolve(t, Scope{CommandLine: true}); loc.Origin != OriginEnvFile || loc.Path != envFile || loc.Exists {
		t.Fatalf("GRAPHVEIN_SQLITE_PATH: %+v", loc)
	}
	if loc = f.resolve(t, Scope{CommandLine: false}); loc.Origin != OriginInFolder {
		t.Fatalf("GRAPHVEIN_SQLITE_PATH applied to another entry: %+v", loc)
	}

	// Step 1: Postgres wins over everything.
	f.env[EnvPostgres] = "postgres://example.invalid/db"
	if loc = f.resolve(t, Scope{CommandLine: true}); loc.Origin != OriginPostgres || loc.Path != "" || loc.Exists {
		t.Fatalf("Postgres: %+v", loc)
	}
}

// TestResolveRefusesTwoIndexes: an in-folder store and a store in the
// stores folder for one folder are both refused, naming both, and
// nothing is created.
func TestResolveRefusesTwoIndexes(t *testing.T) {
	f := newFixture(t)
	def := f.resolve(t, Scope{})
	newStore(t, def.Path)
	inFolder := filepath.Join(f.folder, InFolderDir, StoreFile)
	newStore(t, inFolder)
	_, err := f.r.Resolve(f.folder, Scope{CommandLine: true})
	var two *TwoIndexesError
	if !errors.As(err, &two) || len(two.Paths) != 2 || !strings.Contains(err.Error(), inFolder) || !strings.Contains(err.Error(), def.Path) {
		t.Fatalf("err = %v, want both paths", err)
	}
	if !strings.Contains(err.Error(), "Graphvein uses neither until one is removed") {
		t.Fatalf("err = %v", err)
	}
	// GRAPHVEIN_SQLITE_PATH for this folder does not consult them.
	f.env[EnvSQLitePath] = filepath.Join(t.TempDir(), "x.db")
	if loc := f.resolve(t, Scope{CommandLine: true}); loc.Origin != OriginEnvFile {
		t.Fatalf("with GRAPHVEIN_SQLITE_PATH: %+v", loc)
	}
}

// TestPrepare: a new store's folder is owner-only and names its folder;
// a store folder naming another folder is refused.
func TestPrepare(t *testing.T) {
	f := newFixture(t)
	loc := f.resolve(t, Scope{})
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(loc.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("store folder mode %v, want 0700", info.Mode().Perm())
	}
	if owner, _ := readOwner(loc.Dir()); owner != f.folder {
		t.Fatalf("folder.txt names %q, want %q", owner, f.folder)
	}
	if err := os.WriteFile(filepath.Join(loc.Dir(), FolderFile), []byte(filepath.Join(f.folder, "other")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var foreign *ForeignStoreError
	if _, err := f.r.Resolve(f.folder, Scope{}); !errors.As(err, &foreign) {
		t.Fatalf("Resolve with a foreign folder.txt: %v", err)
	}
	// Postgres, GRAPHVEIN_SQLITE_PATH and in-folder places are the
	// user's: Prepare creates nothing for them.
	for _, l := range []Location{{Origin: OriginPostgres}, {Origin: OriginEnvFile, Path: filepath.Join(t.TempDir(), "a", "index.db")}} {
		if err := l.Prepare(); err != nil {
			t.Fatal(err)
		}
		if l.Path != "" {
			if _, err := os.Stat(filepath.Dir(l.Path)); !os.IsNotExist(err) {
				t.Fatalf("Prepare created %s", filepath.Dir(l.Path))
			}
		}
	}
}

func TestStoresFileStrict(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.r.StoresFile), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"version: 2\nentries: []\n", "version: 1\nunknown: x\n", "version: 1\nentries:\n- path: relative\n  store: /x\n"} {
		if err := os.WriteFile(f.r.StoresFile, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.r.Resolve(f.folder, Scope{}); err == nil {
			t.Errorf("stores.yaml %q accepted", body)
		}
		if err := f.r.Assign(f.folder, t.TempDir()); err == nil {
			t.Errorf("stores.yaml %q rewritten", body)
		}
		if data, _ := os.ReadFile(f.r.StoresFile); string(data) != body {
			t.Errorf("stores.yaml changed to %q", data)
		}
	}
}

// TestSetDefaultDirKeepsStores: changing the default folder never moves an
// existing store; it gets an entry so it keeps resolving.
func TestSetDefaultDirKeepsStores(t *testing.T) {
	f := newFixture(t)
	loc := f.resolve(t, Scope{})
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	newStore(t, loc.Path)
	if err := f.r.SetDefaultDir(filepath.Join(t.TempDir(), "new")); err != nil {
		t.Fatal(err)
	}
	if now := f.resolve(t, Scope{}); now.Path != loc.Path || now.Origin != OriginMapped || !now.Exists {
		t.Fatalf("after changing the default folder: %+v, want %s", now, loc.Path)
	}
}

func TestMove(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	inFolder := filepath.Join(f.folder, InFolderDir, StoreFile)
	newStore(t, inFolder)
	want, err := sqlite.ReadCounts(ctx, inFolder)
	if err != nil {
		t.Fatal(err)
	}

	// In-folder to the default place: no stores.yaml entry needed, the
	// in-folder files and the emptied .graphvein folder go.
	plan, err := f.r.PlanMove(f.folder, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ToDefault || plan.From.Origin != OriginInFolder {
		t.Fatalf("plan %+v", plan)
	}
	res, err := f.r.Move(ctx, plan, MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recorded || !contains(res.Deleted, "index.db") || !contains(res.Deleted, "index.db-lock") {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(f.folder, InFolderDir)); !os.IsNotExist(err) {
		t.Fatalf(".graphvein kept: %v", err)
	}
	loc := f.resolve(t, Scope{})
	if loc.Origin != OriginDefault || !loc.Exists || loc.Path != plan.To {
		t.Fatalf("after the move: %+v", loc)
	}
	if got, err := sqlite.ReadCounts(ctx, loc.Path); err != nil || !got.Equal(want) {
		t.Fatalf("moved counts %+v %v, want %+v", got, err, want)
	}
	if _, err := os.Stat(loc.Path + ".moving"); !os.IsNotExist(err) {
		t.Fatal("temporary copy left")
	}

	// Default to a chosen folder: recorded in stores.yaml.
	dest := filepath.Join(t.TempDir(), "elsewhere")
	if plan, err = f.r.PlanMove(f.folder, dest, nil); err != nil {
		t.Fatal(err)
	}
	if res, err = f.r.Move(ctx, plan, MoveOptions{}); err != nil || !res.Recorded {
		t.Fatalf("move to %s: %+v %v", dest, res, err)
	}
	loc = f.resolve(t, Scope{})
	resolvedDest, _ := workspace.Resolve(dest)
	if loc.Origin != OriginMapped || loc.Path != filepath.Join(resolvedDest, Key(f.folder), StoreFile) {
		t.Fatalf("after the second move: %+v", loc)
	}
	if _, err := os.Stat(filepath.Dir(plan.From.Path)); !os.IsNotExist(err) {
		t.Fatalf("emptied store folder kept: %v", err)
	}

	// Back to the default: the entry goes.
	if plan, err = f.r.PlanMove(f.folder, "", nil); err != nil {
		t.Fatal(err)
	}
	if res, err = f.r.Move(ctx, plan, MoveOptions{}); err != nil || !res.Recorded {
		t.Fatalf("move back: %+v %v", res, err)
	}
	if file, _ := f.r.readStores(); len(file.Entries) != 0 {
		t.Fatalf("entries after moving back: %+v", file.Entries)
	}
	if loc = f.resolve(t, Scope{}); loc.Origin != OriginDefault || !loc.Exists {
		t.Fatalf("after moving back: %+v", loc)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestMoveRefusals: every refused plan writes nothing.
func TestMoveRefusals(t *testing.T) {
	f := newFixture(t)
	if _, err := f.r.PlanMove(f.folder, "", nil); err == nil || !strings.Contains(err.Error(), "has no index yet") {
		t.Fatalf("no index: %v", err)
	}
	loc := f.resolve(t, Scope{})
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	newStore(t, loc.Path)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	root := filepath.VolumeName(f.folder) + string(filepath.Separator)
	for name, dest := range map[string]string{
		"inside the indexed folder": filepath.Join(f.folder, "sub"),
		"inside a .git folder":      filepath.Join(t.TempDir(), ".git", "x"),
		"a file":                    file,
		"the root":                  root,
		"a registered folder":       filepath.Join(other, "x"),
		"its own place":             filepath.Dir(loc.Dir()),
	} {
		if _, err := f.r.PlanMove(f.folder, dest, []string{other}); err == nil {
			t.Errorf("%s (%s): accepted", name, dest)
		}
	}
	f.env[EnvPostgres] = "postgres://example.invalid/db"
	if _, err := f.r.PlanMove(f.folder, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "team mode") {
		t.Fatalf("Postgres: %v", err)
	}
}

// TestRefusalsThroughALink: the refusals hold when the paths reach their
// folders through a symbolic link, with the registered folders, the data
// folder and the typed destination each spelled through the link or
// resolved (on macOS every temporary folder is such a path: /var/folders
// is a link to /private/var/folders).
func TestRefusalsThroughALink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	resolvedReal, err := workspace.Resolve(real)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	r := &Resolver{
		Getenv:     func(k string) string { return env[k] },
		StoresFile: filepath.Join(link, "config", StoresFileName),
		DataDir:    filepath.Join(link, "data"),
	}
	for _, dir := range []string{"src/demo", "other", "dest"} {
		if err := os.MkdirAll(filepath.Join(real, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	folder := filepath.Join(link, "src", "demo")
	loc, err := r.Resolve(folder, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	newStore(t, loc.Path)
	stores := filepath.Join(r.DataDir, "graphvein", "stores")
	viaLink := []string{filepath.Join(link, "other")}
	resolved := []string{filepath.Join(resolvedReal, "other")}
	code := func(err error) string {
		var refusal *RefusalError
		if errors.As(err, &refusal) {
			return refusal.Code
		}
		return fmt.Sprint(err)
	}
	for _, tc := range []struct {
		name       string
		dest       string
		registered []string
		want       string
	}{
		{"registered through the link", filepath.Join(link, "other", "x"), viaLink, "inside_registered"},
		{"registered resolved, typed through the link", filepath.Join(link, "other", "x"), resolved, "inside_registered"},
		{"registered through the link, typed resolved", filepath.Join(resolvedReal, "other", "x"), viaLink, "inside_registered"},
		{"the registered folder itself", filepath.Join(resolvedReal, "other"), viaLink, "inside_registered"},
		{"the indexed folder through the link", filepath.Join(link, "src", "demo", "sub"), nil, "inside_folder"},
		{"its own place, typed resolved", filepath.Join(resolvedReal, "data", "graphvein", "stores"), nil, "same_place"},
		{"its own place through the link", stores, nil, "same_place"},
		{"a .git folder through the link", filepath.Join(link, "dest", ".git", "x"), nil, "inside_git"},
	} {
		if _, err := r.PlanMove(folder, tc.dest, tc.registered); code(err) != tc.want {
			t.Errorf("%s (%s): got %s, want %s", tc.name, tc.dest, code(err), tc.want)
		}
	}
	if err := CheckStoresDir(filepath.Join(resolvedReal, "other", "keep"), viaLink); code(err) != "inside_registered" {
		t.Errorf("stores folder inside a registered folder: %v", err)
	}
	if err := CheckStoresDir(filepath.Join(link, "other", "keep"), resolved); code(err) != "inside_registered" {
		t.Errorf("stores folder through the link inside a registered folder: %v", err)
	}
	// A destination outside every registered folder is accepted.
	if _, err := r.PlanMove(folder, filepath.Join(link, "dest"), viaLink); err != nil {
		t.Errorf("outside the registered folders: %v", err)
	}
	if !SamePath(stores, filepath.Join(resolvedReal, "data", "graphvein", "stores")) || SamePath(stores, filepath.Join(resolvedReal, "dest")) {
		t.Errorf("SamePath through the link")
	}
	if !InsidePath(filepath.Join(link, "other", "new", "x"), filepath.Join(resolvedReal, "other")) || InsidePath(filepath.Join(link, "dest"), filepath.Join(link, "other")) {
		t.Errorf("InsidePath through the link")
	}
}

// TestSamePathComparison checks the comparison behind the refusals with a
// resolution of its own, so it means the same on every system: a short
// or linked spelling and the long or real one are one place, also below
// the part that exists, and nothing else is.
func TestSamePathComparison(t *testing.T) {
	short := filepath.FromSlash("/users/RUNNER~1/temp")
	long := filepath.FromSlash("/users/runneradmin/temp")
	resolve := func(p string) string {
		if p == short || strings.HasPrefix(p, short+string(filepath.Separator)) {
			return long + p[len(short):]
		}
		return p
	}
	j := func(base string, parts ...string) string { return filepath.Join(append([]string{base}, parts...)...) }
	for _, tc := range []struct {
		a, b         string
		same, inside bool
	}{
		{short, long, true, false},
		{j(short, "003", "x"), j(long, "003", "x"), true, false},
		{j(short, "003", "x"), j(long, "003"), false, true},
		{j(long, "003", "x"), j(short, "003"), false, true},
		{j(short, "003"), j(long, "003", "x"), false, false},
		{j(short, "0031"), j(long, "003"), false, false},
		{j(short, "a"), j(long, "b"), false, false},
	} {
		if got := samePathBy(resolve, tc.a, tc.b); got != tc.same {
			t.Errorf("samePath(%s, %s) = %v", tc.a, tc.b, got)
		}
		if got := insidePathBy(resolve, tc.a, tc.b); got != tc.inside {
			t.Errorf("insidePath(%s, %s) = %v", tc.a, tc.b, got)
		}
	}
	// The real resolution: t.TempDir is the unresolved spelling on macOS
	// (/var/folders) and may be a short name on Windows; a path below it
	// that does not exist yet compares by its longest existing ancestor.
	dir := t.TempDir()
	resolved, err := workspace.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !SamePath(j(dir, "new", "x"), j(resolved, "new", "x")) || !InsidePath(j(resolved, "new", "x"), dir) || SamePath(j(dir, "new"), j(resolved, "other")) {
		t.Errorf("%s and %s do not compare as one place", dir, resolved)
	}
}

// TestMoveWaitsForTheLock: a move while a run holds the index lock is
// refused after the wait, keeps the original and leaves no copy.
func TestMoveWaitsForTheLock(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	inFolder := filepath.Join(f.folder, InFolderDir, StoreFile)
	newStore(t, inFolder)
	plan, err := f.r.PlanMove(f.folder, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	locker := sqlite.IndexLocker(inFolder)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.Move(ctx, plan, MoveOptions{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("Move while locked: %v", err)
	}
	if _, err := os.Stat(inFolder); err != nil {
		t.Fatal("original gone:", err)
	}
	if _, err := os.Stat(filepath.Dir(plan.To)); !os.IsNotExist(err) {
		t.Fatalf("a copy was started: %v", err)
	}
	lease.Release()
	if _, err := f.r.Move(ctx, plan, MoveOptions{}); err != nil {
		t.Fatal(err)
	}
	// The plan is spent: the folder no longer resolves to its original.
	if _, err := f.r.Move(ctx, plan, MoveOptions{}); err == nil {
		t.Fatal("a second Move of the same plan was accepted")
	}
}

// TestList: stores in the stores folder, a mapped store and an in-folder
// store of a registered folder; a store whose folder is gone is listed
// as not found.
func TestList(t *testing.T) {
	f := newFixture(t)
	loc := f.resolve(t, Scope{})
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	newStore(t, loc.Path)

	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	g, err := f.r.Resolve(gone, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Prepare(); err != nil {
		t.Fatal(err)
	}
	newStore(t, g.Path)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(t.TempDir(), "legacy")
	newStore(t, filepath.Join(legacy, InFolderDir, StoreFile))

	stores, err := f.r.List(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 3 {
		t.Fatalf("List = %+v, want 3 stores", stores)
	}
	byPath := map[string]Store{}
	for _, s := range stores {
		byPath[s.Path] = s
		if s.Size <= 0 {
			t.Errorf("%s: size %d", s.Path, s.Size)
		}
	}
	if s := byPath[loc.Path]; !s.FolderFound || s.Folder != f.folder {
		t.Errorf("own store: %+v", s)
	}
	if s := byPath[g.Path]; s.FolderFound || s.Folder != g.Folder {
		t.Errorf("store of a gone folder: %+v", s)
	}
	resolvedLegacy, _ := workspace.Resolve(legacy)
	if s := byPath[filepath.Join(resolvedLegacy, InFolderDir, StoreFile)]; s.Origin != OriginInFolder || !s.FolderFound {
		t.Errorf("in-folder store: %+v", s)
	}
}
