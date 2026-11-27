// Package storeloc decides where the index of a folder is stored (ADR
// 0093). Every caller that opens a folder's store, the CLI commands, the
// server's workspace entries and project runs, asks Resolve, so one rule
// says which file is read and written:
//
//  1. GRAPHVEIN_POSTGRES_URL set: the Postgres database (team mode).
//  2. GRAPHVEIN_SQLITE_PATH set and the folder is the one named on the
//     command line: that file.
//  3. <folder>/.graphvein/index.db exists: that file (a v0.1.1 store).
//  4. stores.yaml maps the folder to a store folder: index.db there.
//  5. Otherwise <stores>/<name>-<h8>/index.db in the per-user data folder.
//
// When more than one of 3-5 holds an index.db, Resolve refuses: nothing is
// read or written until one is removed. Resolve itself never writes;
// Location.Prepare creates a new store's folder when a run is about to
// create the store.
package storeloc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/graphvein/graphvein/internal/workspace"
)

// Environment variables the resolver reads.
const (
	// EnvPostgres selects team mode: every folder's index is in that
	// database.
	EnvPostgres = "GRAPHVEIN_POSTGRES_URL"
	// EnvSQLitePath names one SQLite file for the folder named on the
	// command line only.
	EnvSQLitePath = "GRAPHVEIN_SQLITE_PATH"
	// EnvStoreDir replaces the per-user stores folder for every folder
	// without an index elsewhere.
	EnvStoreDir = "GRAPHVEIN_STORE_DIR"
)

// StoreFile is the SQLite file name inside a store folder.
const StoreFile = "index.db"

// InFolderDir is the folder inside an indexed folder that holds a v0.1.1
// store (and, later, committed rationale; ARCHITECTURE "Rationale").
const InFolderDir = ".graphvein"

// FolderFile is the small text file in a store folder that names the
// indexed folder (its resolved path), so a store whose folder is gone can
// still be named, and a store folder is never taken for another folder's.
const FolderFile = "folder.txt"

// Origin says which lookup step chose a Location.
type Origin string

const (
	// OriginPostgres is step 1: GRAPHVEIN_POSTGRES_URL.
	OriginPostgres Origin = "postgres"
	// OriginEnvFile is step 2: GRAPHVEIN_SQLITE_PATH.
	OriginEnvFile Origin = "sqlite_path_env"
	// OriginInFolder is step 3: <folder>/.graphvein/index.db.
	OriginInFolder Origin = "in_folder"
	// OriginMapped is step 4: an entry of stores.yaml.
	OriginMapped Origin = "stores_file"
	// OriginDefault is step 5: the stores folder.
	OriginDefault Origin = "default"
)

// DirSource says where the stores folder of step 5 came from.
type DirSource string

const (
	// DirPlatform is the platform's per-user data folder.
	DirPlatform DirSource = "platform"
	// DirEnv is GRAPHVEIN_STORE_DIR.
	DirEnv DirSource = "env"
	// DirStoresFile is default_dir in stores.yaml.
	DirStoresFile DirSource = "stores_file"
)

// Scope says which folder Resolve is asked about.
type Scope struct {
	// CommandLine is true for the folder named on this command line
	// (graphvein index <path>, serve <path>, serve --project); only that
	// folder uses GRAPHVEIN_SQLITE_PATH. The server's other workspace
	// entries resolve with CommandLine false.
	CommandLine bool
}

// Location is where one folder's index is stored.
type Location struct {
	// Folder is the indexed folder, resolved (workspace.Resolve).
	Folder string
	// Origin is the lookup step that chose the location.
	Origin Origin
	// Path is the absolute SQLite file; empty for Postgres.
	Path string
	// Exists is true when Path holds a file now (always false for
	// Postgres, which has no local file).
	Exists bool
	// StoresDir is the stores folder new indexes go to, and DirSource
	// where it came from. Set for every SQLite origin, so a caller can
	// say where a move without a destination would go.
	StoresDir string
	DirSource DirSource
	// StoresFile is the stores.yaml this location was read with.
	StoresFile string
}

// Inside reports whether the store file lies inside the indexed folder
// (Git and the scanner see it there).
func (l Location) Inside() bool {
	return l.Path != "" && workspace.Inside(l.Path, l.Folder)
}

// Dir is the folder holding the store file; empty for Postgres.
func (l Location) Dir() string {
	if l.Path == "" {
		return ""
	}
	return filepath.Dir(l.Path)
}

// TwoIndexesError is Resolve's refusal when a folder has more than one
// candidate store (lookup steps 3-5): Graphvein uses neither, because
// either could be the one the user means.
type TwoIndexesError struct {
	Folder string
	Paths  []string
}

func (e *TwoIndexesError) Error() string {
	return fmt.Sprintf("two indexes for %s: %s. Graphvein uses neither until one is removed (see graphvein store list); nothing was read or written", e.Folder, strings.Join(e.Paths, " and "))
}

// ErrMoved reports a store that no longer resolves where a process found
// it when it opened the folder's index: a move finished meanwhile. A run
// that waited for the index lock during a move stops with it instead of
// creating an empty store at the old place (ADR 0087 revision 5).
var ErrMoved = errors.New("the index moved")

// Moved checks that folder's store still resolves where want says, with
// the same scope; otherwise it returns an error wrapping ErrMoved that
// names both places.
func (r *Resolver) Moved(want Location, scope Scope) error {
	now, err := r.Resolve(want.Folder, scope)
	if err != nil {
		return err
	}
	if now.Origin == want.Origin && now.Path == want.Path {
		return nil
	}
	where := now.Path
	if now.Origin == OriginPostgres {
		where = "the Postgres database from " + EnvPostgres
	}
	return fmt.Errorf("%w: the index of %s is now at %s, not at %s where this process found it; open the folder again", ErrMoved, want.Folder, where, want.Path)
}

// ForeignStoreError reports a store folder whose folder.txt names another
// folder: two folders whose keys collide, or a folder copied by hand. It
// is never opened for this folder.
type ForeignStoreError struct {
	Folder, Store, Owner string
}

func (e *ForeignStoreError) Error() string {
	return fmt.Sprintf("the store folder %s belongs to %s, not %s; Graphvein does not open it for this folder", e.Store, e.Owner, e.Folder)
}

// Resolver resolves folders against one environment, one stores.yaml and
// one per-user data folder. Tests build one with temporary paths; Default
// builds the one the binary uses.
type Resolver struct {
	// Getenv reads the environment; nil means os.Getenv.
	Getenv func(string) string
	// StoresFile is the stores.yaml path.
	StoresFile string
	// DataDir is the platform's per-user data folder (Graphvein's own
	// folder goes under it); empty when the platform has none, which
	// makes step 5 fail unless GRAPHVEIN_STORE_DIR or default_dir is set.
	DataDir string
}

// Default returns the resolver of this process: the real environment,
// stores.yaml beside the workspace registry (so GRAPHVEIN_WORKSPACE_FILE
// moves both), and the platform's per-user data folder.
func Default() (*Resolver, error) {
	registry, err := workspace.DefaultPath()
	if err != nil {
		return nil, err
	}
	data, _ := UserDataDir(runtime.GOOS, os.Getenv)
	return &Resolver{StoresFile: filepath.Join(filepath.Dir(registry), StoresFileName), DataDir: data}, nil
}

// Env reads a variable as this resolver does (tests replace the
// environment through Getenv).
func (r *Resolver) Env(key string) string { return r.getenv(key) }

func (r *Resolver) getenv(key string) string {
	if r.Getenv != nil {
		return r.Getenv(key)
	}
	return os.Getenv(key)
}

// StoresDir is the folder new indexes go to and where that came from:
// GRAPHVEIN_STORE_DIR, then stores.yaml default_dir, then the platform's
// data folder.
func (r *Resolver) StoresDir() (string, DirSource, error) {
	f, err := r.readStores()
	if err != nil {
		return "", "", err
	}
	return r.storesDir(f)
}

func (r *Resolver) storesDir(f storesFile) (string, DirSource, error) {
	if dir := r.getenv(EnvStoreDir); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", "", fmt.Errorf("resolving %s: %w", EnvStoreDir, err)
		}
		return abs, DirEnv, nil
	}
	if f.DefaultDir != "" {
		return f.DefaultDir, DirStoresFile, nil
	}
	if r.DataDir == "" {
		return "", "", fmt.Errorf("no per-user data folder is known on this system; set %s to the folder for Graphvein's indexes", EnvStoreDir)
	}
	return filepath.Join(r.DataDir, "graphvein", "stores"), DirPlatform, nil
}

// Resolve returns where folder's index is stored (lookup steps 1-5). It
// reads the environment, stores.yaml and the file system, and writes
// nothing. folder must exist: its key is its resolved path.
func (r *Resolver) Resolve(folder string, scope Scope) (Location, error) {
	resolved, err := workspace.Resolve(folder)
	if err != nil {
		return Location{}, fmt.Errorf("resolving the folder %s: %w", folder, err)
	}
	loc := Location{Folder: resolved, StoresFile: r.StoresFile}
	if r.getenv(EnvPostgres) != "" {
		loc.Origin = OriginPostgres
		return loc, nil
	}
	f, err := r.readStores()
	if err != nil {
		return Location{}, err
	}
	// The stores folder is only needed for step 5; an unknown one fails
	// there and nowhere else.
	storesDir, source, dirErr := r.storesDir(f)
	loc.StoresDir, loc.DirSource = storesDir, source
	if p := r.getenv(EnvSQLitePath); p != "" && scope.CommandLine {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Location{}, fmt.Errorf("resolving %s: %w", EnvSQLitePath, err)
		}
		loc.Origin, loc.Path, loc.Exists = OriginEnvFile, abs, fileExists(abs)
		return loc, nil
	}
	type candidate struct {
		origin Origin
		path   string
	}
	var candidates []candidate
	candidates = append(candidates, candidate{OriginInFolder, filepath.Join(resolved, InFolderDir, StoreFile)})
	mapped := ""
	if e, ok := f.entry(resolved); ok {
		mapped = filepath.Join(e.Store, StoreFile)
		candidates = append(candidates, candidate{OriginMapped, mapped})
	}
	defaultPath := ""
	if dirErr == nil {
		defaultPath = filepath.Join(storesDir, Key(resolved), StoreFile)
		candidates = append(candidates, candidate{OriginDefault, defaultPath})
	}
	var found []candidate
	for _, c := range candidates {
		if !fileExists(c.path) {
			continue
		}
		dup := false
		for _, have := range found {
			if workspace.SamePath(have.path, c.path) {
				dup = true
			}
		}
		if !dup {
			found = append(found, c)
		}
	}
	switch {
	case len(found) > 1:
		paths := make([]string, len(found))
		for i, c := range found {
			paths[i] = c.path
		}
		return Location{}, &TwoIndexesError{Folder: resolved, Paths: paths}
	case len(found) == 1:
		loc.Origin, loc.Path, loc.Exists = found[0].origin, found[0].path, true
	case mapped != "":
		loc.Origin, loc.Path = OriginMapped, mapped
	case dirErr != nil:
		return Location{}, dirErr
	default:
		loc.Origin, loc.Path = OriginDefault, defaultPath
	}
	if loc.Origin == OriginDefault || loc.Origin == OriginMapped {
		if err := checkOwner(loc.Dir(), resolved); err != nil {
			return Location{}, err
		}
	}
	return loc, nil
}

// Prepare makes the store folder of a new store ready before a run
// creates the store in it: owner-only (0700) folders for the per-user
// stores folder and the store's own folder, and folder.txt naming the
// indexed folder. It does nothing for Postgres, GRAPHVEIN_SQLITE_PATH and
// an in-folder store, whose places the user chose.
func (l Location) Prepare() error {
	if l.Origin != OriginDefault && l.Origin != OriginMapped {
		return nil
	}
	dir := l.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating the store folder %s: %w", dir, err)
	}
	if err := checkOwner(dir, l.Folder); err != nil {
		return err
	}
	owner := filepath.Join(dir, FolderFile)
	if _, err := os.Stat(owner); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(owner, []byte(l.Folder+"\n"), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", owner, err)
		}
	}
	return nil
}

// checkOwner refuses a store folder whose folder.txt names another
// folder. A folder without folder.txt (not created yet, or made by hand)
// is accepted.
func checkOwner(dir, folder string) error {
	owner, err := readOwner(dir)
	if err != nil || owner == "" {
		return err
	}
	if owner != folder && !workspace.SamePath(owner, folder) {
		return &ForeignStoreError{Folder: folder, Store: dir, Owner: owner}
	}
	return nil
}

// readOwner returns the folder that dir's folder.txt names, "" without
// one.
func readOwner(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, FolderFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filepath.Join(dir, FolderFile), err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Key is the store folder name of a resolved folder in the stores folder:
// a readable name from the folder's base name and the first eight hex
// digits of the SHA-256 of the resolved path, so two folders with one
// base name get two stores and the same folder always gets the same one.
func Key(resolved string) string {
	sum := sha256.Sum256([]byte(resolved))
	return safeName(filepath.Base(resolved)) + "-" + hex.EncodeToString(sum[:4])
}

// safeName keeps letters, digits, dot, dash and underscore of a base name
// (others become a dash), at most 40 bytes, so the store folder name is
// valid on every file system; an empty result is "folder".
func safeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "folder"
	}
	return out
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
