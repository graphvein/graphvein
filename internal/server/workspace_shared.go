package server

import (
	"fmt"
	"net/http"
	"os"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/workspace"
)

// This file holds the shared-store rule of the workspace (claim row WS-57,
// ADR 0087 revision 3). Under GRAPHVEIN_POSTGRES_URL every entry's index
// lives in one database, where a repository's facts are kept by its
// repository ID and a project's links by its name, not by folder. Two
// registered entries in different folders that use one such name would read
// whichever folder was indexed last, so neither is answered: the list says
// which entry holds the name, every per-index request is refused with 409
// shared_store_conflict, the background check records nothing, and a typed
// path that would add a second user of the name is refused.

// Kinds of a StoreClaim.
const (
	claimRepository = "repository"
	claimProject    = "project"
)

// StoreClaim is one name an entry reads from a store every entry shares:
// a repository ID or a project name, with the folder whose index writes
// it. A project member the manifest gives no path claims its repository
// with Folder "": it reads whatever the store holds under the name (ADR
// 0075), so it conflicts with any entry that claims the name from a
// folder, whose facts it would serve as its own (T038 re-check W4); two
// members without a path share the name by design.
type StoreClaim struct {
	Kind   string
	Name   string
	Folder string
}

// StoreConflict is a name two entries in different folders read from the
// shared store.
type StoreConflict struct {
	// Claim is this entry's claim; Other holds the same name for
	// OtherFolder.
	Claim       StoreClaim
	Other       workspace.Entry
	OtherFolder string
}

// RepositoryClaims are what a repository index reads from a shared store.
func RepositoryClaims(repository, folder string) []StoreClaim {
	return []StoreClaim{{Kind: claimRepository, Name: repository, Folder: folder}}
}

// ProjectClaims are what a project index reads from a shared store: the
// project's links by its name, and each member by its repository ID, with
// the path the manifest gives (whether or not the checkout is there), or
// "" when it gives none.
func ProjectClaims(p config.Project) []StoreClaim {
	out := []StoreClaim{{Kind: claimProject, Name: p.Name, Folder: p.Dir}}
	for _, m := range p.Members {
		out = append(out, StoreClaim{Kind: claimRepository, Name: m.Repository, Folder: m.Path})
	}
	return out
}

// EntryClaims reads an entry's claims from its folder as opening it would:
// the repository graphvein.yaml names (also from a file that does not
// load, when its key can be read), else the registered name; a project's
// manifest. When the configuration cannot be read (or the folder
// is gone) the registered name is claimed, since facts indexed under it
// may still be stored.
func EntryClaims(e workspace.Entry) []StoreClaim {
	if e.Kind == workspace.KindProject {
		if p, err := config.LoadProject(e.Path); err == nil {
			return ProjectClaims(p)
		}
		return []StoreClaim{{Kind: claimProject, Name: e.Name, Folder: e.Path}}
	}
	// A graphvein.yaml that does not load still names its repository when
	// its key can be read, as when the entry opens (ADR 0088).
	name, _, err := config.ResolveRegisteredRepository(e.Path, e.Name)
	if err != nil {
		name = e.Name
	}
	return RepositoryClaims(name, e.Path)
}

// FindStoreConflict returns the first entry of others, in another folder
// than self, that claims one of claims for another folder; nil when none.
// others maps each entry to its claims.
func FindStoreConflict(self string, claims []StoreClaim, others []workspace.Entry, claimsOf func(workspace.Entry) []StoreClaim) *StoreConflict {
	for _, o := range others {
		if workspace.SamePath(o.Path, self) {
			continue
		}
		for _, oc := range claimsOf(o) {
			for _, c := range claims {
				if c.Kind == oc.Kind && c.Name == oc.Name && !shareable(c.Folder, oc.Folder) {
					return &StoreConflict{Claim: c, Other: o, OtherFolder: oc.Folder}
				}
			}
		}
	}
	return nil
}

// shareable reports two claims of one name that read the same facts: both
// without a folder (members without a path, which share the store's facts
// by design), or both from one folder. A claim without a folder and one
// with a folder are not: the first would serve the second folder's facts.
func shareable(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return sameFolder(a, b)
}

// sameFolder reports two spellings of one folder: equal as SamePath
// compares them, or one folder on disk (a symbolic link in a member path).
// Two folders that cannot both be read are different, which refuses
// rather than answers.
func sameFolder(a, b string) bool {
	if workspace.SamePath(a, b) {
		return true
	}
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// Message words a conflict of the entry name for the user: which entry
// holds the name and how to give one of them another.
func (c *StoreConflict) Message(name string) string {
	how := "set another repository: in one folder's graphvein.yaml"
	if c.Claim.Kind == claimProject {
		how = "set another name: in one project's graphvein-project.yaml"
	}
	if c.Claim.Folder == "" || c.OtherFolder == "" {
		// One of them is a project member without a path, which reads
		// whatever is stored under the name.
		return fmt.Sprintf("%s and %s (%s) both use the %s name %q in the one database GRAPHVEIN_POSTGRES_URL names, and the project member without a path would show the facts the other folder (%s) indexed as its own; give the member its path in graphvein-project.yaml, rename one (repository: in graphvein.yaml or the member's repository in graphvein-project.yaml), then index both again, or remove the entry %s (%s) from the workspace",
			name, c.Other.Name, c.Other.Path, c.Claim.Kind, c.Claim.Name, c.Claim.Folder+c.OtherFolder, c.Other.Name, c.Other.Path)
	}
	return fmt.Sprintf("%s and %s (%s) both use the %s name %q in the one database GRAPHVEIN_POSTGRES_URL names, from different folders (%s and %s), so either could show the other's facts; %s, then index both folders again (the facts stored under the name may be either folder's), or remove the entry %s (%s) from the workspace",
		name, c.Other.Name, c.Other.Path, c.Claim.Kind, c.Claim.Name, c.Claim.Folder, c.OtherFolder, how, c.Other.Name, c.Other.Path)
}

// conflictDoc is an entry's shared-store conflict in GET /api/v0/workspace.
type conflictDoc struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Kind and Name are the name both entries use; Entry, EntryName and
	// EntryPath the other entry.
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Entry     string `json:"entry"`
	EntryName string `json:"entry_name"`
	EntryPath string `json:"entry_path"`
}

const codeSharedStore = "shared_store_conflict"

func (c *StoreConflict) doc(name string) *conflictDoc {
	return &conflictDoc{Code: codeSharedStore, Message: c.Message(name), Kind: c.Claim.Kind, Name: c.Claim.Name, Entry: c.Other.ID, EntryName: c.Other.Name, EntryPath: c.Other.Path}
}

// claimsOf is what entry e reads from the shared store now: an open
// index's options (its names as opened, the command-line --repository
// included), else its folder's configuration.
func (s *workspaceServer) claimsOf(e workspace.Entry) []StoreClaim {
	if oi, _ := s.hold(e.ID, false); oi != nil {
		defer s.release(oi)
		o := oi.handler.opt
		if o.Project != nil {
			return ProjectClaims(o.Project.current())
		}
		return RepositoryClaims(o.Repository, o.Root)
	}
	return EntryClaims(e)
}

// conflicts maps each entry in conflict to its first conflict; empty
// when the store is not shared.
func (s *workspaceServer) conflicts(entries []workspace.Entry) map[string]*StoreConflict {
	out := map[string]*StoreConflict{}
	if !s.ws.SharedStore {
		return out
	}
	claims := make(map[string][]StoreClaim, len(entries))
	for _, e := range entries {
		claims[e.ID] = s.claimsOf(e)
	}
	of := func(e workspace.Entry) []StoreClaim { return claims[e.ID] }
	for _, e := range entries {
		if c := FindStoreConflict(e.Path, claims[e.ID], entries, of); c != nil {
			out[e.ID] = c
		}
	}
	return out
}

// sharedConflict refuses entry id when it is in a shared-store conflict
// (WS-57); nil otherwise, also for an id the workspace does not have (the
// caller reports that).
func (s *workspaceServer) sharedConflict(id string) error {
	if !s.ws.SharedStore {
		return nil
	}
	entries, _ := s.entries()
	for _, e := range entries {
		if e.ID != id {
			continue
		}
		if c := FindStoreConflict(e.Path, s.claimsOf(e), entries, s.claimsOf); c != nil {
			return &apiError{http.StatusConflict, codeSharedStore, c.Message(e.Name)}
		}
	}
	return nil
}
