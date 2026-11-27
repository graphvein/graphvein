package storeloc

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/graphvein/graphvein/internal/workspace"
)

// Place is where the new store of a folder that is being added is put
// (ADR 0093; the Add dialog's "Change…", ADR 0041 revision 2): only for
// this folder; new indexes keep using the default.
type Place string

const (
	// PlaceDefault is the folder's place in the stores folder.
	PlaceDefault Place = "default"
	// PlaceInside is <folder>/.graphvein/index.db, where Git lists the
	// folder unless an ignore file covers it.
	PlaceInside Place = "inside"
	// PlaceFolder is <dir>/<name>-<h8>/index.db in a folder the user typed.
	PlaceFolder Place = "folder"
)

// PlanPlace checks putting the new store of folder at place (dir is the
// typed folder of PlaceFolder, absolute) and returns the Location the
// store would have. Nothing is written. Refusals are *RefusalError:
// team_mode (Postgres), store_fixed (the folder already has an index,
// which stays where it is; moving it is Settings' Move), the
// CheckDestination list for PlaceFolder, index_exists and foreign_store.
func (r *Resolver) PlanPlace(folder string, place Place, dir string, registered []string) (Location, error) {
	cur, err := r.Resolve(folder, Scope{})
	if err != nil {
		return Location{}, err
	}
	if cur.Origin == OriginPostgres {
		return Location{}, refuse("team_mode", "every index is in the Postgres database from %s (team mode); there is no local place to choose", EnvPostgres)
	}
	out := Location{Folder: cur.Folder, StoresDir: cur.StoresDir, DirSource: cur.DirSource, StoresFile: cur.StoresFile}
	key := Key(cur.Folder)
	switch place {
	case PlaceDefault:
		if cur.StoresDir == "" {
			_, _, err := r.StoresDir()
			if err == nil {
				err = errors.New("no stores folder is known")
			}
			return Location{}, err
		}
		out.Origin, out.Path = OriginDefault, filepath.Join(cur.StoresDir, key, StoreFile)
	case PlaceInside:
		out.Origin, out.Path = OriginInFolder, filepath.Join(cur.Folder, InFolderDir, StoreFile)
	case PlaceFolder:
		dest, err := ResolveDest(dir)
		if err != nil {
			return Location{}, err
		}
		if err := CheckDestination(cur.Folder, dest, registered); err != nil {
			return Location{}, err
		}
		out.Origin, out.Path = OriginMapped, filepath.Join(dest, key, StoreFile)
		if cur.StoresDir != "" && workspace.SamePath(filepath.Dir(out.Path), filepath.Join(cur.StoresDir, key)) {
			out.Origin = OriginDefault
		}
	default:
		return Location{}, fmt.Errorf("unknown place %q", place)
	}
	if cur.Exists {
		if workspace.SamePath(cur.Path, out.Path) {
			return cur, nil
		}
		return Location{}, refuse("store_fixed", "%s already has an index at %s; it stays there (moving it is Settings' Move or graphvein store move)", cur.Folder, cur.Path)
	}
	if fileExists(out.Path) {
		return Location{}, refuse("index_exists", "%s already exists; Graphvein never uses another folder's index", out.Path)
	}
	if out.Origin != OriginInFolder {
		if err := checkOwner(out.Dir(), cur.Folder); err != nil {
			return Location{}, &RefusalError{Code: "foreign_store", Message: err.Error()}
		}
	}
	return out, nil
}

// RecordPlace records a place PlanPlace returned, so Resolve finds it: a
// stores.yaml entry for a typed folder; for the default place or the
// folder's own .graphvein, the removal of any entry (an in-folder store is
// found by its file, which the caller creates).
func (r *Resolver) RecordPlace(loc Location) error {
	switch loc.Origin {
	case OriginMapped:
		return r.Assign(loc.Folder, loc.Dir())
	case OriginDefault, OriginInFolder:
		f, err := r.readStores()
		if err != nil {
			return err
		}
		if _, ok := f.entry(loc.Folder); !ok {
			return nil
		}
		return r.updateStores(func(f *storesFile) {
			kept := f.Entries[:0:0]
			for _, e := range f.Entries {
				if e.Path != loc.Folder && !workspace.SamePath(e.Path, loc.Folder) {
					kept = append(kept, e)
				}
			}
			f.Entries = kept
		})
	}
	return fmt.Errorf("a store from %s is not a place to record", loc.Origin)
}
