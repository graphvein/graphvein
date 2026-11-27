package storeloc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/workspace"
)

// MovePlan is a checked move of one folder's SQLite store: what PlanMove
// showed and Move does. A caller that asks the user first (the CLI, the
// server's confirm value, ADR 0093) binds both paths, and Move
// refuses when the folder no longer resolves to From.
type MovePlan struct {
	From Location
	// To is the destination index.db.
	To string
	// ToDefault is true when To is the folder's place in the stores
	// folder, which needs no stores.yaml entry.
	ToDefault bool
}

// MoveResult says what Move did.
type MoveResult struct {
	// Bytes is the size of the copied index.db, and Runs the number of
	// index runs it holds (checked equal to the original's).
	Bytes int64
	Runs  int
	// Deleted lists the base names of the original's files that were
	// deleted (index.db, index.db-wal, index.db-shm, index.db-lock).
	Deleted []string
	// Recorded is true when stores.yaml changed.
	Recorded bool
}

// MoveError is a refusal or failure of a move. Moved is true when the
// copy is in place and recorded but the original could not be deleted
// completely: the folder then has two indexes, and Resolve refuses it
// until the files Remaining lists are deleted.
type MoveError struct {
	Err       error
	Moved     bool
	Remaining []string
}

func (e *MoveError) Error() string { return e.Err.Error() }
func (e *MoveError) Unwrap() error { return e.Err }

// ErrLocked wraps a move refused because an index run holds the store's
// index lock for longer than the wait.
var ErrLocked = errors.New("an index run holds the index lock")

// PlanMove checks a move of folder's store to destDir, the folder that
// will hold the store's own folder (<destDir>/<name>-<h8>/index.db); an
// empty destDir is the stores folder. registered lists the workspace
// registry's folders: a destination inside one of them is refused, because
// that folder's scan and Git would see the store. Nothing is written.
func (r *Resolver) PlanMove(folder, destDir string, registered []string) (MovePlan, error) {
	from, err := r.Resolve(folder, Scope{})
	if err != nil {
		return MovePlan{}, err
	}
	switch {
	case from.Origin == OriginPostgres:
		return MovePlan{}, refuse("team_mode", "the index of %s is in the Postgres database from %s (team mode); there is no local file to move", from.Folder, EnvPostgres)
	case !from.Exists:
		return MovePlan{}, refuse("no_index", "%s has no index yet (it would be created at %s); run graphvein index first", from.Folder, from.Path)
	}
	plan := MovePlan{From: from}
	key := Key(from.Folder)
	if destDir == "" {
		if from.StoresDir == "" {
			_, _, err := r.StoresDir()
			return MovePlan{}, err
		}
		plan.To, plan.ToDefault = filepath.Join(from.StoresDir, key, StoreFile), true
	} else {
		dest, err := ResolveDest(destDir)
		if err != nil {
			return MovePlan{}, err
		}
		if err := CheckDestination(from.Folder, dest, registered); err != nil {
			return MovePlan{}, err
		}
		plan.To = filepath.Join(dest, key, StoreFile)
		plan.ToDefault = from.StoresDir != "" && workspace.SamePath(filepath.Dir(plan.To), filepath.Join(from.StoresDir, key))
	}
	if workspace.SamePath(plan.To, from.Path) || plan.To == from.Path {
		return MovePlan{}, refuse("same_place", "the index of %s is already at %s", from.Folder, from.Path)
	}
	if fileExists(plan.To) {
		return MovePlan{}, refuse("index_exists", "%s already exists; Graphvein never overwrites an index", plan.To)
	}
	if err := checkOwner(filepath.Dir(plan.To), from.Folder); err != nil {
		return MovePlan{}, &RefusalError{Code: "foreign_store", Message: err.Error()}
	}
	return plan, nil
}

// ResolveDest makes dest absolute and resolves the symbolic links of the
// part of it that exists, so the checks see (and a confirmation names) the
// real place.
func ResolveDest(dest string) (string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dest, err)
	}
	abs = filepath.Clean(abs)
	rest := ""
	for p := abs; ; p = filepath.Dir(p) {
		if resolved, err := workspace.Resolve(p); err == nil {
			return filepath.Join(resolved, rest), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// RefusalError is a destination (or plan) PlanMove, CheckDestination or
// CheckStoresDir refuses, with a stable code the server answers with
// (ADR 0041 revision 2): not_absolute, network_path, filesystem_root,
// home_directory, inside_git, inside_folder, inside_registered,
// not_a_directory, not_writable, unreadable, same_place, index_exists,
// foreign_store, no_index, team_mode.
type RefusalError struct {
	Code    string
	Message string
}

func (e *RefusalError) Error() string { return e.Message }

func refuse(code, format string, args ...any) error {
	return &RefusalError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CheckDestination refuses a destination folder (absolute, links
// resolved) that a store of folder must not be put in: a network path on
// Windows (a UNC path or a drive letter mapped to a network share), the
// file system root, the home folder itself, anything inside a .git
// folder, the indexed folder or anything inside it (an index inside the
// folder is kept in its .graphvein folder), a registered folder or
// anything inside one, a file, or a folder that cannot be written. The
// server's checks (ADR 0093, ADR 0041 revision 2) apply the same list.
// Every refusal is a *RefusalError.
func CheckDestination(folder, dest string, registered []string) error {
	if err := checkDir(dest, folder, registered); err != nil {
		return err
	}
	return checkWritable(dest)
}

// CheckStoresDir refuses a folder for new indexes (stores.yaml
// default_dir, "New indexes are kept in") on the rules of
// CheckDestination that do not depend on one indexed folder.
func CheckStoresDir(dest string, registered []string) error {
	if err := checkDir(dest, "", registered); err != nil {
		return err
	}
	return checkWritable(dest)
}

// checkDir applies the refusals that need no write; folder, when not
// empty, is the indexed folder, refused before the registered folders so
// the message names it.
func checkDir(dest, folder string, registered []string) error {
	if !filepath.IsAbs(dest) {
		return refuse("not_absolute", "the destination %q is not an absolute path", dest)
	}
	if networkPath(dest) {
		return refuse("network_path", "the destination %s is on a network share; SQLite's WAL needs a local disk", dest)
	}
	if filepath.Dir(dest) == dest {
		return refuse("filesystem_root", "the destination %s is the root of a file system; choose a folder inside it", dest)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if resolved, err := workspace.Resolve(home); err == nil && workspace.SamePath(resolved, dest) {
			return refuse("home_directory", "the destination %s is the home folder itself; choose a folder inside it", dest)
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(dest), "/") {
		if strings.EqualFold(part, ".git") {
			return refuse("inside_git", "the destination %s is inside a .git folder", dest)
		}
	}
	if folder != "" && (workspace.SamePath(dest, folder) || workspace.Inside(dest, folder)) {
		return refuse("inside_folder", "the destination %s is inside the indexed folder %s, where its scan and Git would see the index", dest, folder)
	}
	for _, other := range registered {
		if workspace.SamePath(dest, other) || workspace.Inside(dest, other) {
			return refuse("inside_registered", "the destination %s is inside the workspace folder %s, where its scan and Git would see the index", dest, other)
		}
	}
	return nil
}

// checkWritable refuses a destination that is a file or a folder that
// cannot be written; a folder that does not exist yet is created by the
// move.
func checkWritable(dest string) error {
	info, err := os.Stat(dest)
	switch {
	case err == nil && !info.IsDir():
		return refuse("not_a_directory", "the destination %s is a file, not a folder", dest)
	case err == nil:
		probe, err := os.CreateTemp(dest, ".graphvein-write-*")
		if err != nil {
			return refuse("not_writable", "the destination %s is not writable: %v", dest, err)
		}
		name := probe.Name()
		probe.Close()
		os.Remove(name)
	case !errors.Is(err, fs.ErrNotExist):
		return refuse("unreadable", "checking the destination %s: %v", dest, err)
	}
	return nil
}

// networkPath reports a Windows network path: a UNC path (\\server\share)
// or a drive letter the system reports as remote (a mapped network drive).
// Elsewhere network file systems are mounted folders and are not detected.
func networkPath(dest string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if strings.HasPrefix(dest, `\\`) || strings.HasPrefix(dest, "//") {
		return true
	}
	return remoteDrive(dest)
}

// MoveOptions tunes Move.
type MoveOptions struct {
	// Wait is how long Move waits for a running index run to release the
	// store's index lock.
	Wait time.Duration
}

// Move moves the store of plan: under the store's index lock it copies
// the store with SQLite's VACUUM INTO (which folds in the -wal file),
// checks the copy (same run ids, same row count in every table), records
// the new place in stores.yaml (or removes the entry for the default
// place), and only then deletes the original's index.db, -wal, -shm and
// -lock files. Any failure before the record keeps the original and
// removes the partial copy. The caller must have closed its own
// connections to the store (on Windows an open file cannot be deleted).
func (r *Resolver) Move(ctx context.Context, plan MovePlan, opt MoveOptions) (MoveResult, error) {
	fail := func(err error) (MoveResult, error) { return MoveResult{}, &MoveError{Err: err} }
	if err := r.samePlace(plan); err != nil {
		return fail(err)
	}
	locker := sqlite.IndexLocker(plan.From.Path)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, opt.Wait)
	if errors.Is(err, store.ErrIndexLocked) {
		return fail(fmt.Errorf("%w of %s (%s); try again when it ends", ErrLocked, plan.From.Folder, sqlite.LockPath(plan.From.Path)))
	}
	if err != nil {
		return fail(fmt.Errorf("taking the index lock (%s): %w", sqlite.LockPath(plan.From.Path), err))
	}
	defer lease.Release()
	// A run that held the lock may have changed nothing about the place,
	// but another move may have: check again under the lock.
	if err := r.samePlace(plan); err != nil {
		return fail(err)
	}
	dir := filepath.Dir(plan.To)
	_, statErr := os.Stat(dir)
	createdDir := errors.Is(statErr, fs.ErrNotExist)
	cleanup := func() {
		os.Remove(plan.To + ".moving")
		if createdDir {
			os.Remove(filepath.Join(dir, FolderFile))
			os.Remove(dir)
		}
	}
	target := Location{Folder: plan.From.Folder, Origin: OriginMapped, Path: plan.To}
	if err := target.Prepare(); err != nil {
		cleanup()
		return fail(err)
	}
	tmp := plan.To + ".moving"
	os.Remove(tmp) // left by a move that was killed; never an index
	if err := sqlite.CopyInto(ctx, plan.From.Path, tmp); err != nil {
		cleanup()
		return fail(err)
	}
	want, err := sqlite.ReadCounts(ctx, plan.From.Path)
	if err != nil {
		cleanup()
		return fail(err)
	}
	got, err := sqlite.ReadCounts(ctx, tmp)
	if err != nil {
		cleanup()
		return fail(err)
	}
	if !want.Equal(got) {
		cleanup()
		return fail(fmt.Errorf("the copy of %s differs from the original (run ids or row counts); the original is kept", plan.From.Path))
	}
	if err := os.Chmod(tmp, 0o600); err != nil && runtime.GOOS != "windows" {
		cleanup()
		return fail(fmt.Errorf("restricting %s: %w", tmp, err))
	}
	if err := os.Rename(tmp, plan.To); err != nil {
		cleanup()
		return fail(fmt.Errorf("putting the copy in place at %s: %w", plan.To, err))
	}
	recorded, err := r.record(plan)
	if err != nil {
		os.Remove(plan.To)
		cleanup()
		return fail(err)
	}
	res := MoveResult{Bytes: fileSize(plan.To), Runs: len(got.RunIDs), Recorded: recorded}
	var remaining []string
	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := plan.From.Path + suffix
		if err := os.Remove(p); err == nil {
			res.Deleted = append(res.Deleted, filepath.Base(p))
		} else if !errors.Is(err, fs.ErrNotExist) {
			remaining = append(remaining, p)
			errs = append(errs, err)
		}
	}
	lease.Release()
	locker.Close()
	lock := sqlite.LockPath(plan.From.Path)
	if err := os.Remove(lock); err == nil {
		res.Deleted = append(res.Deleted, filepath.Base(lock))
	}
	os.Remove(lock + "-journal")
	removeEmptyStoreDir(plan.From)
	if len(remaining) > 0 {
		return res, &MoveError{Moved: true, Remaining: remaining, Err: fmt.Errorf("the index of %s was copied to %s and recorded, but the original could not be deleted (%w); delete %s by hand, until then Graphvein refuses to use either", plan.From.Folder, plan.To, errors.Join(errs...), strings.Join(remaining, ", "))}
	}
	return res, nil
}

// samePlace refuses a plan whose folder no longer resolves to the plan's
// original, or whose destination appeared meanwhile.
func (r *Resolver) samePlace(plan MovePlan) error {
	now, err := r.Resolve(plan.From.Folder, Scope{})
	if err != nil {
		return err
	}
	if now.Origin != plan.From.Origin || now.Path != plan.From.Path || !now.Exists {
		return fmt.Errorf("the index of %s changed place since the move was checked (now %s); check the move again", plan.From.Folder, now.Path)
	}
	if fileExists(plan.To) {
		return fmt.Errorf("%s already exists; Graphvein never overwrites an index", plan.To)
	}
	return nil
}

// record writes the new place to stores.yaml and reports whether the file
// changed: a move to the default place needs an entry only to drop one.
func (r *Resolver) record(plan MovePlan) (bool, error) {
	f, err := r.readStores()
	if err != nil {
		return false, err
	}
	_, had := f.entry(plan.From.Folder)
	if plan.ToDefault && !had {
		return false, nil
	}
	if err := r.Assign(plan.From.Folder, filepath.Dir(plan.To)); err != nil {
		return false, err
	}
	return true, nil
}

// removeEmptyStoreDir removes the original's folder when the move left it
// empty: a .graphvein folder with nothing else in it, or a store folder
// holding only its folder.txt. Anything else in it is kept.
func removeEmptyStoreDir(from Location) {
	dir := from.Dir()
	if from.Origin == OriginDefault || from.Origin == OriginMapped {
		names, err := os.ReadDir(dir)
		if err == nil && len(names) == 1 && names[0].Name() == FolderFile {
			os.Remove(filepath.Join(dir, FolderFile))
		}
	}
	if from.Origin != OriginEnvFile {
		os.Remove(dir) // fails, harmlessly, unless empty
	}
}
