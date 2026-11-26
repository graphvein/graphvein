// Package workspace keeps the workspace registry: the list of repository
// and project folders that graphvein serve offers in its sidebar (ADR
// 0087). An entry is only a pointer to a folder; everything the server
// says about an index comes from its store, never from the registry.
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// EnvFile names the environment variable that overrides the registry
// file's location. Tests and scripts set it so they never write the
// user's own registry.
const EnvFile = "GRAPHVEIN_WORKSPACE_FILE"

// FileName is the registry's file name inside the graphvein folder of the
// user configuration directory.
const FileName = "workspace.yaml"

// version is the only registry format this binary reads and writes.
const version = 1

// maxFileSize bounds the registry file: a few hundred entries are a few
// tens of KiB, so a larger file is not one this binary wrote.
const maxFileSize = 4 << 20

// Kind says what an entry's folder holds.
type Kind string

const (
	// KindRepository is a repository folder indexed alone.
	KindRepository Kind = "repository"
	// KindProject is the folder holding graphvein-project.yaml.
	KindProject Kind = "project"
)

// Entry is one registered folder. ID is derived from the other fields,
// so it is the same on every start and in every link (D9).
type Entry struct {
	ID   string `yaml:"-"`
	Kind Kind   `yaml:"kind"`
	// Name is the repository ID or the project name the folder was
	// registered with.
	Name string `yaml:"name"`
	// Path is the absolute folder with symbolic links resolved.
	Path string `yaml:"path"`
	// AddedAt is the RFC 3339 time the entry was registered.
	AddedAt string `yaml:"added_at"`
}

// EntryID is the stable key of an entry: its name, a tilde and the first
// eight hex digits of a hash of kind, path and name. Two entries with the
// same name in different folders get different keys; the name keeps a
// link readable.
func EntryID(kind Kind, path, name string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + path + "\x00" + name))
	return name + "~" + hex.EncodeToString(sum[:4])
}

// DefaultPath is the registry file: $GRAPHVEIN_WORKSPACE_FILE, or
// workspace.yaml in the graphvein folder of os.UserConfigDir (%AppData%
// on Windows, ~/Library/Application Support on macOS, $XDG_CONFIG_HOME or
// ~/.config elsewhere).
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvFile); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", EnvFile, err)
		}
		return abs, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the user configuration directory for the workspace registry: %w", err)
	}
	return filepath.Join(dir, "graphvein", FileName), nil
}

// Registry reads and writes one registry file. Its methods are safe for
// concurrent use within one process, and every write is a read, change
// and atomic replace under an exclusive operating-system lock on the lock
// file next to the registry, so writers in other processes never lose
// each other's entries (ADR 0087 "Registry writes").
type Registry struct {
	path string
	// err is why the registry has no file (no configuration directory);
	// every method returns it.
	err error
	mu  sync.Mutex
	now func() time.Time
	// lockWait overrides lockTimeout in tests.
	lockWait time.Duration
}

// Open returns the registry stored at path. Nothing is read until a
// method is called; a missing file is an empty registry.
func Open(path string) *Registry {
	return &Registry{path: path, now: time.Now}
}

// Unavailable returns a registry that refuses every call with err: serve
// keeps serving its command-line index when the configuration directory
// cannot be found.
func Unavailable(err error) *Registry {
	return &Registry{err: err, now: time.Now}
}

// Path is the registry file, empty when the registry is unavailable.
func (r *Registry) Path() string { return r.path }

// file is the registry's YAML document.
type file struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

// Entries lists the registered entries in file order with their IDs. A
// file that does not parse is an error: it is never rewritten or
// reported as empty, so a damaged file loses nothing.
func (r *Registry) Entries() ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read()
}

func (r *Registry) read() ([]Entry, error) {
	if r.err != nil {
		return nil, r.err
	}
	data, err := readBounded(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("reading the workspace registry %s: %w", r.path, err)
	}
	if f.Version != version {
		return nil, fmt.Errorf("the workspace registry %s has version %d; this binary reads version %d", r.path, f.Version, version)
	}
	seen := map[string]bool{}
	out := make([]Entry, 0, len(f.Entries))
	for i, e := range f.Entries {
		if err := valid(e); err != nil {
			return nil, fmt.Errorf("the workspace registry %s, entry %d: %w", r.path, i+1, err)
		}
		e.ID = EntryID(e.Kind, e.Path, e.Name)
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out, nil
}

func valid(e Entry) error {
	if e.Kind != KindRepository && e.Kind != KindProject {
		return fmt.Errorf("kind %q is neither %s nor %s", e.Kind, KindRepository, KindProject)
	}
	if e.Name == "" || strings.ContainsRune(e.Name, 0) {
		return errors.New("name must be nonempty and contain no NUL")
	}
	if !filepath.IsAbs(e.Path) {
		return fmt.Errorf("path %q is not absolute", e.Path)
	}
	return nil
}

// Add registers e (Path absolute and resolved) unless the folder is
// already registered. One folder is one entry: identity is the folder
// (SamePath), so serving a folder again after its repository or project
// was renamed returns the stored entry, with its id, instead of adding a
// second one (the name shown is re-resolved when the index opens). It
// returns the stored entry and whether it was added.
func (r *Registry) Add(e Entry) (Entry, bool, error) {
	if err := valid(e); err != nil {
		return Entry{}, false, err
	}
	e.ID = EntryID(e.Kind, e.Path, e.Name)
	var stored Entry
	var added bool
	err := r.update(func(entries []Entry) ([]Entry, bool) {
		for _, have := range entries {
			// The same folder, also spelled with another case on a
			// case-insensitive file system, is the same entry whatever
			// its name or kind now.
			if have.ID == e.ID || SamePath(have.Path, e.Path) {
				stored = have
				return nil, false
			}
		}
		if e.AddedAt == "" {
			e.AddedAt = r.now().UTC().Format(time.RFC3339)
		}
		stored, added = e, true
		return append(entries, e), true
	})
	if err != nil {
		return Entry{}, false, err
	}
	return stored, added, nil
}

// Remove drops the entry with id from the registry; it never touches the
// folder or its index. It reports whether the entry existed.
func (r *Registry) Remove(id string) (bool, error) {
	removed := false
	err := r.update(func(entries []Entry) ([]Entry, bool) {
		kept := entries[:0:0]
		for _, e := range entries {
			if e.ID != id {
				kept = append(kept, e)
			}
		}
		removed = len(kept) != len(entries)
		return kept, removed
	})
	return removed && err == nil, err
}

// update reads the registry, lets change compute the new entries and
// writes them when change says so, all under the in-process mutex and the
// cross-process lock, so a write never replaces a file that changed after
// it was read.
func (r *Registry) update(change func([]Entry) ([]Entry, bool)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	lock, err := lockFile(r.path+LockSuffix, r.lockTimeout())
	if err != nil {
		return err
	}
	entries, err := r.read()
	if err == nil {
		if next, write := change(entries); write {
			err = r.write(next)
		}
	}
	if uerr := lock.unlock(); uerr != nil && err == nil {
		err = fmt.Errorf("unlocking the workspace registry: %w", uerr)
	}
	return err
}

func (r *Registry) lockTimeout() time.Duration {
	if r.lockWait > 0 {
		return r.lockWait
	}
	return lockTimeout
}

// write replaces the file atomically: a temporary file in the same folder
// renamed over it, so a reader or a crash never sees half a registry.
func (r *Registry) write(entries []Entry) error {
	data, err := yaml.Marshal(file{Version: version, Entries: entries})
	if err != nil {
		return fmt.Errorf("encoding the workspace registry: %w", err)
	}
	data = append([]byte("# Graphvein workspace registry (graphvein serve). Entries point at folders;\n# removing one here never deletes the folder or its index.\n"), data...)
	return writeFileAtomic(r.path, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func readBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("the workspace registry %s is larger than %d MiB", path, maxFileSize>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the workspace registry: %w", err)
	}
	return data, nil
}

// Sort orders entries as the sidebar lists them: projects first, then
// repositories, each by name and then by path, so positions never move
// under the pointer.
func Sort(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind == KindProject
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Path < b.Path
	})
}

// Resolve makes path absolute, clean, free of symbolic links and spelled
// with the file system's own casing, as an entry stores it: on a
// case-insensitive file system one folder typed two ways resolves to one
// path. On Windows filepath.EvalSymlinks already returns the stored
// casing of every component and an upper-case drive letter; elsewhere
// realCase reads it from the folder listings.
func Resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return realCase(filepath.Clean(resolved)), nil
}
