package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/storeloc"
)

// storeResolver is the store resolver of this process (ADR 0093); tests
// point it at temporary folders through GRAPHVEIN_WORKSPACE_FILE (which
// places stores.yaml) and GRAPHVEIN_STORE_DIR.
func storeResolver() (*storeloc.Resolver, error) {
	return storeloc.Default()
}

// locate resolves the store of root, the folder named on this command
// line, so GRAPHVEIN_SQLITE_PATH applies to it.
func locate(root string) (storeloc.Location, error) {
	return commandLine(root).locate()
}

// storeAt is how one caller finds one folder's store (ADR 0093): the scope
// it resolves with (the folder named on this command line, or a workspace
// entry the server opens, which never uses GRAPHVEIN_SQLITE_PATH) and,
// once the caller has opened or announced a store, that location. With
// want set, every writer, lock and folder taken later checks that the
// store is still there, so a run that waited for the index lock during a
// move stops with storeloc.ErrMoved instead of creating an empty store at
// the old place (ADR 0087 revision 5).
type storeAt struct {
	root  string
	scope storeloc.Scope
	want  *storeloc.Location
}

// commandLine is the store of the folder named on this command line.
func commandLine(root string) storeAt {
	return storeAt{root: root, scope: storeloc.Scope{CommandLine: true}}
}

// entryStore is the store of a workspace entry the server opens: resolved
// once, without GRAPHVEIN_SQLITE_PATH, and held to that place.
func entryStore(loc storeloc.Location) storeAt {
	return storeAt{root: loc.Folder, want: &loc}
}

// pinned holds a to loc, the store the caller opened or announced.
func (a storeAt) pinned(loc storeloc.Location) storeAt {
	a.want = &loc
	return a
}

// locate resolves the store; with want set, a store that moved since is
// an error wrapping storeloc.ErrMoved.
func (a storeAt) locate() (storeloc.Location, error) {
	r, err := storeResolver()
	if err != nil {
		return storeloc.Location{}, err
	}
	if a.want != nil {
		if err := r.Moved(*a.want, a.scope); err != nil {
			return storeloc.Location{}, err
		}
		return *a.want, nil
	}
	return r.Resolve(a.root, a.scope)
}

// openWriter opens the writer store: Postgres when GRAPHVEIN_POSTGRES_URL
// is set, otherwise the SQLite file, whose folder is created owner-only
// first when the store is new.
func (a storeAt) openWriter(ctx context.Context) (store.Store, error) {
	loc, err := a.locate()
	if err != nil {
		return nil, err
	}
	if loc.Origin == storeloc.OriginPostgres {
		return postgres.Open(ctx, os.Getenv(storeloc.EnvPostgres))
	}
	if err := loc.Prepare(); err != nil {
		return nil, err
	}
	return sqlite.Open(ctx, loc.Path)
}

// openRead opens the store read-only; it never creates one.
func (a storeAt) openRead(ctx context.Context) (store.Store, error) {
	loc, err := a.locate()
	if err != nil {
		return nil, err
	}
	if loc.Origin == storeloc.OriginPostgres {
		return postgres.OpenRead(ctx, os.Getenv(storeloc.EnvPostgres))
	}
	return sqlite.OpenRead(ctx, loc.Path)
}

// dir is the directory of the store's files, where an index run writes
// its temporary snapshot; "" for Postgres and for a store that cannot be
// located (its run then fails at the lock).
func (a storeAt) dir() string {
	loc, err := a.locate()
	if err != nil {
		return ""
	}
	return loc.Dir()
}

// name is the file name of the store's SQLite file in dir, which names an
// index run's temporary snapshot so the next run can remove one a killed
// run left (indexrun.Options.StoreName); "" for Postgres and for a store
// that cannot be located.
func (a storeAt) name() string {
	loc, err := a.locate()
	if err != nil || loc.Origin == storeloc.OriginPostgres {
		return ""
	}
	return filepath.Base(loc.Path)
}

// locker is the index lock of the store (a Postgres advisory lock keyed
// by repository, or the SQLite store's lock file), and how to name it.
func (a storeAt) locker(repository string) (store.IndexLocker, string) {
	if url := os.Getenv(storeloc.EnvPostgres); url != "" {
		return postgres.IndexLocker(url, repository), "Postgres advisory lock"
	}
	return a.sqliteLocker("", false)
}

// projectLocker is the project lock of the project store (ADR 0075
// "Locks"): a Postgres advisory lock keyed by schema and project, or the
// SQLite store's own index lock.
func (a storeAt) projectLocker(project string) (store.IndexLocker, string) {
	if url := os.Getenv(storeloc.EnvPostgres); url != "" {
		return postgres.ProjectLocker(url, project), "Postgres project lock"
	}
	return a.sqliteLocker(project, true)
}

func (a storeAt) sqliteLocker(project string, isProject bool) (store.IndexLocker, string) {
	loc, err := a.locate()
	if err != nil {
		return failedLocker{err: err}, "the index lock"
	}
	var l store.IndexLocker
	if isProject {
		l = sqlite.ProjectLocker(loc.Path, project)
	} else {
		l = sqlite.IndexLocker(loc.Path)
	}
	return preparingLocker{IndexLocker: l, loc: loc, at: a}, sqlite.LockPath(loc.Path)
}

// failedLocker is the index lock of a store that could not be located:
// every Acquire and State returns why, so nothing runs without a lock.
type failedLocker struct{ err error }

func (l failedLocker) Acquire(context.Context, time.Duration) (store.IndexLease, error) {
	return nil, l.err
}

func (l failedLocker) State(context.Context) (store.LockState, error) { return "", l.err }

func (l failedLocker) Close() error { return nil }

// preparingLocker creates a new store's owner-only folder (and its
// folder.txt) right before the lock file is created in it: every run takes
// the lock before it opens the writer store. When its storeAt is held to a
// place (want), it also checks that place before and after taking the
// lock: a move holds the same lock, so a run that waited for it finds the
// store gone and stops (storeloc.ErrMoved) instead of recreating the old
// place. (graphvein index, which holds no place, checks again itself.)
type preparingLocker struct {
	store.IndexLocker
	loc storeloc.Location
	at  storeAt
}

func (l preparingLocker) Acquire(ctx context.Context, wait time.Duration) (store.IndexLease, error) {
	held := l.at.want != nil
	if held {
		if _, err := l.at.locate(); err != nil {
			return nil, err
		}
	}
	if err := l.loc.Prepare(); err != nil {
		return nil, err
	}
	lease, err := l.IndexLocker.Acquire(ctx, wait)
	if err != nil || !held {
		return lease, err
	}
	if _, err := l.at.locate(); err != nil {
		lease.Release()
		return nil, err
	}
	return lease, nil
}

// indexLine is the first line graphvein index prints (ADR 0093): where
// the index is and why there; never the Postgres URL. name is the
// repository, or the project for index --project.
func indexLine(loc storeloc.Location, kind, name string) string {
	if loc.Origin == storeloc.OriginPostgres {
		return fmt.Sprintf("Index: Postgres database from %s (%s %s)", storeloc.EnvPostgres, kind, name)
	}
	var notes []string
	if !loc.Exists {
		notes = append(notes, "new")
	}
	switch {
	case loc.Origin == storeloc.OriginEnvFile:
		notes = append(notes, "from "+storeloc.EnvSQLitePath+", for this folder only")
	case loc.Inside():
		notes = append(notes, "inside the repository")
	case !loc.Exists:
		notes = append(notes, "outside the repository")
	}
	if loc.Origin == storeloc.OriginDefault {
		switch loc.DirSource {
		case storeloc.DirEnv:
			notes = append(notes, "folder from "+storeloc.EnvStoreDir)
		case storeloc.DirStoresFile:
			notes = append(notes, "folder from "+storeloc.StoresFileName)
		}
	}
	if loc.Origin == storeloc.OriginMapped {
		notes = append(notes, "kept where "+storeloc.StoresFileName+" says")
	}
	line := "Index: " + loc.Path
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, "; ") + ")"
	}
	return line
}

// indexTrailer is what graphvein index prints after a run's own lines: on
// a newly created store how to browse it and how to keep it elsewhere,
// and for a store inside the repository how to move it out. serve is the
// serve command for the folder.
func indexTrailer(loc storeloc.Location, serve string) []string {
	switch {
	case loc.Origin == storeloc.OriginInFolder:
		return []string{"Note: this index is inside the repository, where Git lists .graphvein/ as untracked unless an ignore file covers it. To keep it out of the repository: graphvein store move " + quoteArg(loc.Folder)}
	case !loc.Exists && (loc.Origin == storeloc.OriginDefault || loc.Origin == storeloc.OriginMapped):
		return []string{
			"Browse it:          " + serve,
			"Keep it elsewhere:  graphvein store move " + quoteArg(loc.Folder) + " <folder>",
		}
	}
	return nil
}

// quoteArg quotes a path for a command line a user copies: double quotes
// (which Git Bash, other POSIX shells, PowerShell and cmd.exe all accept
// around a plain path) when it holds a space or another character a shell
// splits or expands.
func quoteArg(p string) string {
	if p == "" || strings.ContainsAny(p, " \t'\"$`&|;<>()*?[]{}!#~") {
		return `"` + p + `"`
	}
	return p
}

// sameStore reports whether a location resolved again (after waiting for
// the lock) is still the store the run was announced with.
func sameStore(a, b storeloc.Location) bool {
	return a.Origin == b.Origin && a.Path == b.Path
}

// formatSize prints a store size the way graphvein store does: bytes,
// then KB, MB and GB (powers of 1024), one decimal below 10.
func formatSize(n int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%d bytes", n)
	case v < 10:
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// postgresSet reports team mode (GRAPHVEIN_POSTGRES_URL).
func postgresSet() bool { return os.Getenv(storeloc.EnvPostgres) != "" }
