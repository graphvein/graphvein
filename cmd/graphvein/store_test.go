package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

// storeEnv gives one test its own registry and stores.yaml (beside it),
// its own stores folder and no store variables, and returns the stores
// folder. Nothing outside t.TempDir is read or written.
func storeEnv(t *testing.T) string {
	t.Helper()
	freshWorkspace(t)
	stores := filepath.Join(t.TempDir(), "stores")
	t.Setenv(storeloc.EnvStoreDir, stores)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	return stores
}

// fixtureFolder copies the Go fixture into a new folder named name and
// returns it resolved.
func fixtureFolder(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(dir, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	resolved, err := workspace.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func storeMoveCLI(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := storeCommand(append([]string{"move"}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestIndexLinePerUserStore: a first index creates the store in the
// stores folder, says so on the first line and ends with the two hints;
// a later run prints the path alone; nothing is written into the folder.
func TestIndexLinePerUserStore(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "demo")
	store := filepath.Join(stores, storeloc.Key(root), "index.db")
	code, out, errOut := runCLI("index", root, "--repository", "fixture")
	if code != 0 {
		t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
	}
	got := lines(out)
	if want := "Index: " + store + " (new; outside the repository; folder from GRAPHVEIN_STORE_DIR)"; got[0] != want {
		t.Fatalf("first line %q, want %q", got[0], want)
	}
	if !strings.HasPrefix(got[1], "Files: ") || !strings.HasPrefix(got[3], "Timings: ") {
		t.Fatalf("today's lines moved: %q", got)
	}
	if len(got) != 6 || got[4] != "Browse it:          graphvein serve "+quoteArg(root) || got[5] != "Keep it elsewhere:  graphvein store move "+quoteArg(root)+" <folder>" {
		t.Fatalf("hints: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".graphvein")); !os.IsNotExist(err) {
		t.Fatalf("index wrote into the folder: %v", err)
	}
	if owner, err := os.ReadFile(filepath.Join(filepath.Dir(store), storeloc.FolderFile)); err != nil || strings.TrimSpace(string(owner)) != root {
		t.Fatalf("folder.txt = %q, %v", owner, err)
	}
	if info, err := os.Stat(filepath.Dir(store)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("store folder: %v %v", info, err)
	}
	code, out, errOut = runCLI("index", root, "--repository", "fixture")
	if code != 0 {
		t.Fatalf("second index = %d\n%s", code, errOut)
	}
	if got := lines(out); got[0] != "Index: "+store+" (folder from GRAPHVEIN_STORE_DIR)" || len(got) != 4 {
		t.Fatalf("second run: %q", got)
	}
	// status, query and serve read the same store.
	if code, out, _ := runCLI("status", root, "--repository", "fixture"); code != 0 || strings.Contains(out, "Not indexed") {
		t.Fatalf("status = %d %q", code, out)
	}
	ix, err := openWorkspaceIndex(context.Background(), workspace.Entry{Kind: workspace.KindRepository, Name: "fixture", Path: root}, false)
	if err != nil {
		t.Fatalf("serve's entry open: %v", err)
	}
	if ix.Options.StoreDir != filepath.Dir(store) {
		t.Fatalf("entry store folder = %s, want %s", ix.Options.StoreDir, filepath.Dir(store))
	}
	ix.Close()
}

// TestIndexLinePlatformFolder: without GRAPHVEIN_STORE_DIR a store goes to
// the platform's per-user data folder, here pointed at a temporary folder.
func TestIndexLinePlatformFolder(t *testing.T) {
	storeEnv(t)
	t.Setenv(storeloc.EnvStoreDir, "")
	data := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LocalAppData", data)
	case "darwin", "ios":
		t.Setenv("HOME", data)
		data = filepath.Join(data, "Library", "Application Support")
	default:
		t.Setenv("XDG_DATA_HOME", data)
	}
	root := fixtureFolder(t, "demo")
	code, out, errOut := runCLI("index", root, "--repository", "fixture")
	if code != 0 {
		t.Fatalf("index = %d\n%s", code, errOut)
	}
	store := filepath.Join(data, "graphvein", "stores", storeloc.Key(root), "index.db")
	if got := lines(out)[0]; got != "Index: "+store+" (new; outside the repository)" {
		t.Fatalf("first line %q, want the platform folder %s", got, store)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatal(err)
	}
}

// TestIndexLineInFolderAndEnv: a v0.1.1 store inside the folder keeps
// working where it is, with a note; GRAPHVEIN_SQLITE_PATH says so on every
// run and gets no hints.
func TestIndexLineInFolderAndEnv(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "legacy")
	inFolder := filepath.Join(root, ".graphvein", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", inFolder)
	for i := 0; i < 2; i++ {
		code, out, errOut := runCLI("index", root, "--repository", "fixture")
		if code != 0 {
			t.Fatalf("index = %d\n%s", code, errOut)
		}
		want := "Index: " + inFolder + " (from GRAPHVEIN_SQLITE_PATH, for this folder only)"
		if i == 0 {
			want = "Index: " + inFolder + " (new; from GRAPHVEIN_SQLITE_PATH, for this folder only)"
		}
		if got := lines(out); got[0] != want || len(got) != 4 {
			t.Fatalf("run %d: %q, want first line %q and no hints", i, got, want)
		}
	}
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	code, out, errOut := runCLI("index", root, "--repository", "fixture")
	if code != 0 {
		t.Fatalf("index = %d\n%s", code, errOut)
	}
	got := lines(out)
	if got[0] != "Index: "+inFolder+" (inside the repository)" || len(got) != 5 || got[4] != "Note: this index is inside the repository, where Git lists .graphvein/ as untracked unless an ignore file covers it. To keep it out of the repository: graphvein store move "+quoteArg(root) {
		t.Fatalf("in-folder store: %q", got)
	}
	if _, err := os.Stat(stores); !os.IsNotExist(err) {
		t.Fatalf("an in-folder run created the stores folder: %v", err)
	}
}

// TestIndexRefusesTwoIndexes: with an in-folder store and a per-user store
// for one folder, index, status and query are refused naming both, and
// neither store changes.
func TestIndexRefusesTwoIndexes(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "demo")
	if code, _, errOut := runCLI("index", root, "--repository", "fixture"); code != 0 {
		t.Fatal(errOut)
	}
	perUser := filepath.Join(stores, storeloc.Key(root), "index.db")
	inFolder := filepath.Join(root, ".graphvein", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", inFolder)
	if code, _, errOut := runCLI("index", root, "--repository", "fixture"); code != 0 {
		t.Fatal(errOut)
	}
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	before := runCount(t, perUser) + runCount(t, inFolder)
	want := "two indexes for " + root + ": " + inFolder + " and " + perUser + ". Graphvein uses neither until one is removed (see graphvein store list); nothing was read or written\n"
	for _, args := range [][]string{{"index", root, "--repository", "fixture"}, {"status", root}, {"query", "main", root}, {"store", "where", root}} {
		code, out, errOut := runCLI(args...)
		if code != 1 || errOut != want || out != "" {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q; want %q", args, code, out, errOut, want)
		}
	}
	if after := runCount(t, perUser) + runCount(t, inFolder); after != before {
		t.Fatalf("runs changed while refused: %d, had %d", after, before)
	}
}

func runCount(t *testing.T, path string) int {
	t.Helper()
	s, err := sqlite.OpenRead(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runs, err := s.Runs(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return len(runs)
}

// TestStoreWhereMoveList walks graphvein store: where a store is, moving
// it after a confirmation (declined, then given), reading it from the new
// place, listing it, and listing it as "not found" once the folder is
// gone.
func TestStoreWhereMoveList(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "demo")
	if code, out, errOut := runCLI("store", "where", root); code != 0 || !strings.Contains(out, "not created yet") {
		t.Fatalf("where before indexing = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := runCLI("index", root); code != 0 {
		t.Fatal(errOut)
	}
	from := filepath.Join(stores, storeloc.Key(root), "index.db")
	code, out, errOut := runCLI("store", "where", root)
	got := lines(out)
	if code != 0 || len(got) != 3 || got[0] != from || got[1] != "  kept:     outside the repository (folder from GRAPHVEIN_STORE_DIR)" || !strings.HasPrefix(got[2], "  size:     ") || !strings.Contains(got[2], "; last run ") || !strings.HasSuffix(got[2], "(1 runs stored)") {
		t.Fatalf("where = %d %q %q", code, got, errOut)
	}

	dest := filepath.Join(t.TempDir(), "elsewhere")
	resolvedParent, _ := workspace.Resolve(filepath.Dir(dest))
	to := filepath.Join(resolvedParent, "elsewhere", storeloc.Key(root), "index.db")
	code, out, _ = storeMoveCLI("n\n", root, dest)
	if code != 1 || !strings.Contains(out, "Move it? [y/N] Nothing was moved.") {
		t.Fatalf("declined move = %d %q", code, out)
	}
	if _, err := os.Stat(from); err != nil {
		t.Fatal("declined move touched the store:", err)
	}
	code, out, errOut = storeMoveCLI("y\n", root, dest)
	got = lines(out)
	if code != 0 || got[0] != "Move the index of demo" || got[1] != "  from "+from || got[2] != "  to   "+to {
		t.Fatalf("move = %d %q %q", code, got, errOut)
	}
	if !strings.Contains(out, "Move it? [y/N] Copied ") || !strings.Contains(out, " and checked it (1 runs, same row counts).\n") {
		t.Fatalf("move report: %q", out)
	}
	if !strings.Contains(out, "Deleted the old index.db") || !strings.Contains(out, "index.db-lock.\n") {
		t.Fatalf("move deletions: %q", out)
	}
	storesFile := filepath.Join(filepath.Dir(os.Getenv(workspace.EnvFile)), storeloc.StoresFileName)
	if !strings.Contains(out, "Recorded in "+storesFile+".\n") {
		t.Fatalf("move record: %q", out)
	}
	if _, err := os.Stat(filepath.Dir(from)); !os.IsNotExist(err) {
		t.Fatalf("old store folder kept: %v", err)
	}
	// The moved store answers: status and index use it, with its run.
	if code, out, _ := runCLI("status", root); code != 0 || strings.Contains(out, "Not indexed") {
		t.Fatalf("status after the move = %d %q", code, out)
	}
	code, out, _ = runCLI("index", root)
	if code != 0 || lines(out)[0] != "Index: "+to+" (kept where stores.yaml says)" || len(lines(out)) != 4 {
		t.Fatalf("index after the move = %d %q", code, out)
	}
	if code, out, _ := runCLI("store", "where", root); code != 0 || !strings.Contains(out, "  kept:     outside the repository (recorded in "+storesFile+")") || !strings.Contains(out, "(2 runs stored)") {
		t.Fatalf("where after the move = %d %q", code, out)
	}
	// A destination inside the folder is refused before asking.
	if code, out, errOut := storeMoveCLI("", root, filepath.Join(root, "sub"), "--yes"); code != 1 || out != "" || !strings.Contains(errOut, "inside the indexed folder") {
		t.Fatalf("move into the folder = %d %q %q", code, out, errOut)
	}

	code, out, _ = runCLI("store", "list")
	got = lines(out)
	if code != 0 || len(got) != 2 || !strings.HasPrefix(got[0], "FOLDER") || !strings.HasPrefix(got[1], root+" ") || !strings.Contains(got[1], to) {
		t.Fatalf("list = %d %q", code, got)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runCLI("store", "list"); code != 0 || !strings.Contains(out, root+" (not found)") {
		t.Fatalf("list after removing the folder = %d %q", code, out)
	}
}

// TestStoreMoveInFolderToDefault: store move without a destination moves
// a v0.1.1 in-folder store to the stores folder (no stores.yaml entry) and
// removes the emptied .graphvein folder; GRAPHVEIN_SQLITE_PATH refuses a
// move.
func TestStoreMoveInFolderToDefault(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "legacy")
	inFolder := filepath.Join(root, ".graphvein", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", inFolder)
	if code, _, errOut := runCLI("index", root); code != 0 {
		t.Fatal(errOut)
	}
	if code, _, errOut := storeMoveCLI("", root, "--yes"); code != 1 || !strings.Contains(errOut, "GRAPHVEIN_SQLITE_PATH is set") {
		t.Fatalf("move with GRAPHVEIN_SQLITE_PATH = %d %q", code, errOut)
	}
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	to := filepath.Join(stores, storeloc.Key(root), "index.db")
	code, out, errOut := storeMoveCLI("", "--yes", root)
	if code != 0 || strings.Contains(out, "Move it?") || !strings.Contains(out, "  to   "+to+"\n") || strings.Contains(out, "Recorded in") {
		t.Fatalf("move = %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".graphvein")); !os.IsNotExist(err) {
		t.Fatalf(".graphvein kept: %v", err)
	}
	code, out, _ = runCLI("index", root)
	if code != 0 || lines(out)[0] != "Index: "+to+" (folder from GRAPHVEIN_STORE_DIR)" {
		t.Fatalf("index after the move = %d %q", code, out)
	}
}

// TestStorePostgres: in team mode the Index line names the database
// without its URL, store where says there is no local file, store move is
// refused and store list says the local stores are not used.
func TestStorePostgres(t *testing.T) {
	storeEnv(t)
	url := postgresSchemaURL(t, "graphvein_cli_storeloc_")
	t.Setenv("GRAPHVEIN_POSTGRES_URL", url)
	root := fixtureFolder(t, "demo")
	code, out, errOut := runCLI("index", root, "--repository", "fixture")
	if code != 0 {
		t.Fatalf("index = %d\n%s", code, errOut)
	}
	got := lines(out)
	if got[0] != "Index: Postgres database from GRAPHVEIN_POSTGRES_URL (repository fixture)" || len(got) != 4 || strings.Contains(out, url) {
		t.Fatalf("Postgres index output: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".graphvein")); !os.IsNotExist(err) {
		t.Fatalf("a Postgres run wrote into the folder: %v", err)
	}
	if code, out, _ := runCLI("store", "where", root); code != 0 || !strings.HasPrefix(out, "Postgres database from GRAPHVEIN_POSTGRES_URL (repository demo)\n  kept:     team mode") || strings.Contains(out, url) {
		t.Fatalf("where = %d %q", code, out)
	}
	if code, _, errOut := storeMoveCLI("", root, t.TempDir(), "--yes"); code != 1 || !strings.Contains(errOut, "team mode") {
		t.Fatalf("move = %d %q", code, errOut)
	}
	if code, out, _ := runCLI("store", "list"); code != 0 || !strings.HasPrefix(out, "Team mode: GRAPHVEIN_POSTGRES_URL is set") {
		t.Fatalf("list = %d %q", code, out)
	}
	if code, out, _ := runCLI("status", root, "--repository", "fixture"); code != 0 || strings.Contains(out, "Not indexed") {
		t.Fatalf("status = %d %q", code, out)
	}
}

// TestStoreBackends runs the move-independent store answers on both
// backends: an indexed folder is found and served from where the
// resolver put it.
func TestStoreBackends(t *testing.T) {
	check := func(t *testing.T) {
		root := fixtureFolder(t, "demo")
		if code, _, errOut := runCLI("index", root, "--repository", "fixture"); code != 0 {
			t.Fatal(errOut)
		}
		ix, err := openWorkspaceIndex(context.Background(), workspace.Entry{Kind: workspace.KindRepository, Name: "fixture", Path: root}, false)
		if err != nil {
			t.Fatal(err)
		}
		defer ix.Close()
		runs, err := ix.Options.Store.Runs(context.Background(), "fixture")
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs %d %v", len(runs), err)
		}
	}
	t.Run("sqlite", func(t *testing.T) {
		storeEnv(t)
		check(t)
	})
	t.Run("postgres", func(t *testing.T) {
		storeEnv(t)
		t.Setenv("GRAPHVEIN_POSTGRES_URL", postgresSchemaURL(t, "graphvein_cli_storeloc_"))
		check(t)
	})
}

// TestServeEntryAfterMove checks WS-81 and WS-70 on both backends' rule:
// a workspace entry opened by serve holds its store's place; after
// graphvein store move moved it, the entry's index lock and writer refuse
// (storeloc.ErrMoved) and nothing is created at the old place. A fresh
// open finds the new place.
func TestServeEntryAfterMove(t *testing.T) {
	stores := storeEnv(t)
	root := fixtureFolder(t, "moving")
	if code, _, errOut := runCLI("index", root, "--repository", "fixture"); code != 0 {
		t.Fatal(errOut)
	}
	e := workspace.Entry{Kind: workspace.KindRepository, Name: "fixture", Path: root}
	ix, err := openWorkspaceIndex(context.Background(), e, false)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(stores, storeloc.Key(root))
	dest := t.TempDir()
	ix.Close() // the CLI move needs no open file on Windows
	if code, out, errOut := storeMoveCLI("", root, dest, "--yes"); code != 0 {
		t.Fatalf("move = %d %q %q", code, out, errOut)
	}
	if _, err := ix.Options.Locker.Acquire(context.Background(), indexrun.LockWait); !errors.Is(err, storeloc.ErrMoved) {
		t.Fatalf("lock after the move: %v", err)
	}
	if _, err := ix.Options.OpenWriter(context.Background()); !errors.Is(err, storeloc.ErrMoved) {
		t.Fatalf("writer after the move: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("the old store folder came back: %v", err)
	}
	again, err := openWorkspaceIndex(context.Background(), e, false)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	destResolved, _ := workspace.Resolve(dest)
	if again.Options.StoreDir != filepath.Join(destResolved, storeloc.Key(root)) {
		t.Fatalf("reopened at %s", again.Options.StoreDir)
	}
}
