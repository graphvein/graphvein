package storeloc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/graphvein/graphvein/internal/workspace"
)

// StoresFileName is stores.yaml, kept beside the workspace registry. It is
// a file of its own because the registry's decoder is strict: a key added
// to workspace.yaml would make a v0.1.1 binary refuse the registry.
const StoresFileName = "stores.yaml"

// storesVersion is the only stores.yaml format this binary reads and
// writes.
const storesVersion = 1

// maxStoresFileSize bounds stores.yaml as the registry is bounded.
const maxStoresFileSize = 4 << 20

// storesFile is stores.yaml: the folder new indexes go to, and the
// folders whose store was put somewhere else (graphvein store move, or
// "Change…" when a folder is added).
type storesFile struct {
	Version int `yaml:"version"`
	// DefaultDir replaces the platform's stores folder for new indexes
	// (GRAPHVEIN_STORE_DIR replaces both).
	DefaultDir string       `yaml:"default_dir,omitempty"`
	Entries    []StoreEntry `yaml:"entries"`
}

// StoreEntry maps an indexed folder to the folder holding its index.db.
type StoreEntry struct {
	// Path is the indexed folder, resolved.
	Path string `yaml:"path"`
	// Store is the folder holding index.db.
	Store string `yaml:"store"`
}

func (f storesFile) entry(folder string) (StoreEntry, bool) {
	for _, e := range f.Entries {
		if e.Path == folder || workspace.SamePath(e.Path, folder) {
			return e, true
		}
	}
	return StoreEntry{}, false
}

// readStores reads stores.yaml; a missing or empty file is an empty one,
// and a file that does not parse is an error (never rewritten).
func (r *Resolver) readStores() (storesFile, error) {
	empty := storesFile{Version: storesVersion}
	if r.StoresFile == "" {
		return empty, nil
	}
	info, err := os.Stat(r.StoresFile)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return storesFile{}, fmt.Errorf("reading %s: %w", r.StoresFile, err)
	}
	if info.Size() > maxStoresFileSize {
		return storesFile{}, fmt.Errorf("%s is larger than %d MiB", r.StoresFile, maxStoresFileSize>>20)
	}
	data, err := os.ReadFile(r.StoresFile)
	if err != nil {
		return storesFile{}, fmt.Errorf("reading %s: %w", r.StoresFile, err)
	}
	var f storesFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return empty, nil
		}
		return storesFile{}, fmt.Errorf("reading %s: %w", r.StoresFile, err)
	}
	if f.Version != storesVersion {
		return storesFile{}, fmt.Errorf("%s has version %d; this binary reads version %d", r.StoresFile, f.Version, storesVersion)
	}
	if f.DefaultDir != "" && !filepath.IsAbs(f.DefaultDir) {
		return storesFile{}, fmt.Errorf("%s: default_dir %q is not absolute", r.StoresFile, f.DefaultDir)
	}
	for i, e := range f.Entries {
		if !filepath.IsAbs(e.Path) || !filepath.IsAbs(e.Store) {
			return storesFile{}, fmt.Errorf("%s, entry %d: path and store must be absolute", r.StoresFile, i+1)
		}
	}
	return f, nil
}

// updateStores reads stores.yaml, applies change and writes the result
// atomically, all under the file's own lock (stores.yaml.lock), as the
// registry is written.
func (r *Resolver) updateStores(change func(*storesFile)) error {
	if r.StoresFile == "" {
		return errors.New("no stores.yaml location is known")
	}
	if err := os.MkdirAll(filepath.Dir(r.StoresFile), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(r.StoresFile), err)
	}
	unlock, err := workspace.LockFile(StoresFileName, r.StoresFile+workspace.LockSuffix)
	if err != nil {
		return err
	}
	f, err := r.readStores()
	if err == nil {
		change(&f)
		f.Version = storesVersion
		var data []byte
		data, err = yaml.Marshal(f)
		if err == nil {
			data = append([]byte("# Graphvein store locations (graphvein store). Each entry names the folder\n# holding a folder's index.db; folders without an entry use the default.\n"), data...)
			err = workspace.WriteFileAtomic(r.StoresFile, data)
		}
	}
	if uerr := unlock(); uerr != nil && err == nil {
		err = fmt.Errorf("unlocking %s: %w", r.StoresFile, uerr)
	}
	return err
}

// Assign records in stores.yaml that folder's index is kept in storeDir
// (the folder holding index.db). Assigning the folder's default place
// removes its entry instead, so stores.yaml lists only exceptions. It
// moves nothing: the server's Add with a chosen place (ADR 0093) and
// Move call it.
func (r *Resolver) Assign(folder, storeDir string) error {
	resolved, err := workspace.Resolve(folder)
	if err != nil {
		return fmt.Errorf("resolving the folder %s: %w", folder, err)
	}
	if !filepath.IsAbs(storeDir) {
		return fmt.Errorf("the store folder %q is not absolute", storeDir)
	}
	storeDir = filepath.Clean(storeDir)
	return r.updateStores(func(f *storesFile) {
		kept := f.Entries[:0:0]
		for _, e := range f.Entries {
			if e.Path != resolved && !workspace.SamePath(e.Path, resolved) {
				kept = append(kept, e)
			}
		}
		if dir, _, err := r.storesDir(*f); err != nil || !SamePath(storeDir, filepath.Join(dir, Key(resolved))) {
			kept = append(kept, StoreEntry{Path: resolved, Store: storeDir})
		}
		f.Entries = kept
	})
}

// SetDefaultDir sets stores.yaml's default_dir, the folder new indexes go
// to ("" restores the platform's folder). Existing stores stay where they
// are: a folder already indexed in the old default folder gets an entry
// for it, so it keeps resolving to its store.
func (r *Resolver) SetDefaultDir(dir string) error {
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("the folder %q is not absolute", dir)
		}
		dir = filepath.Clean(dir)
	}
	stores, err := r.List()
	if err != nil {
		return err
	}
	return r.updateStores(func(f *storesFile) {
		old, _, oldErr := r.storesDir(*f)
		f.DefaultDir = dir
		if oldErr != nil {
			return
		}
		for _, s := range stores {
			if s.Origin != OriginDefault || !SamePath(filepath.Dir(s.Dir()), old) {
				continue
			}
			if _, ok := f.entry(s.Folder); !ok {
				f.Entries = append(f.Entries, StoreEntry{Path: s.Folder, Store: s.Dir()})
			}
		}
	})
}
