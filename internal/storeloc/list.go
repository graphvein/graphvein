package storeloc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/graphvein/graphvein/internal/workspace"
)

// Store is one SQLite store List found.
type Store struct {
	Location
	// FolderFound is false when the indexed folder no longer exists (it
	// was moved, renamed or deleted); the store stays until the user
	// removes it (graphvein store list says "not found").
	FolderFound bool
	// Size is the bytes of index.db and its -wal file.
	Size int64
}

// List returns every SQLite store this resolver knows of: the stores in
// the stores folders (the current one, the platform's and stores.yaml's
// default_dir), the stores stores.yaml maps, and the in-folder stores of
// folders (the workspace registry's entries; an in-folder store of a
// folder Graphvein was never told about cannot be found). Stores whose
// folder is gone are listed with FolderFound false; a store folder
// without folder.txt is listed with an empty Folder. Sorted by folder,
// then path.
func (r *Resolver) List(folders ...string) ([]Store, error) {
	f, err := r.readStores()
	if err != nil {
		return nil, err
	}
	var out []Store
	seen := map[string]bool{}
	add := func(folder string, origin Origin, path string) {
		if seen[path] || !fileExists(path) {
			return
		}
		seen[path] = true
		s := Store{Location: Location{Folder: folder, Origin: origin, Path: path, Exists: true, StoresFile: r.StoresFile}}
		if folder != "" {
			_, err := os.Stat(folder)
			s.FolderFound = !errors.Is(err, fs.ErrNotExist)
		}
		s.Size = fileSize(path) + fileSize(path+"-wal")
		out = append(out, s)
	}
	for _, e := range f.Entries {
		add(e.Path, OriginMapped, filepath.Join(e.Store, StoreFile))
	}
	var dirs []string
	if dir, _, err := r.storesDir(f); err == nil {
		dirs = append(dirs, dir)
	}
	if f.DefaultDir != "" {
		dirs = append(dirs, f.DefaultDir)
	}
	if r.DataDir != "" {
		dirs = append(dirs, filepath.Join(r.DataDir, "graphvein", "stores"))
	}
	for _, dir := range dirs {
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, n := range names {
			if !n.IsDir() {
				continue
			}
			storeDir := filepath.Join(dir, n.Name())
			owner, err := readOwner(storeDir)
			if err != nil {
				owner = ""
			}
			add(owner, OriginDefault, filepath.Join(storeDir, StoreFile))
		}
	}
	for _, folder := range folders {
		resolved, err := workspace.Resolve(folder)
		if err != nil {
			resolved = filepath.Clean(folder)
		}
		add(resolved, OriginInFolder, filepath.Join(resolved, InFolderDir, StoreFile))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func fileSize(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size()
}
