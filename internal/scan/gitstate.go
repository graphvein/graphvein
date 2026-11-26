package scan

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// GitState is what a run records about the tree it indexed (ADR 0056): the
// commit HEAD names and whether the indexed files are exactly that commit's.
// It is run metadata only; nothing attaches to the commit.
type GitState struct {
	// Commit is HEAD's full commit id; empty when root is not a git
	// checkout or HEAD has no commit.
	Commit string
	// Dirty is nil when Commit is empty. Otherwise it is false only when
	// every indexed file is a blob of the commit with the same object id and
	// every regular file of the commit that was not indexed still exists in
	// the working tree (so the scanner skipped it by its own rules, which a
	// ref run applies the same way).
	Dirty *bool
}

// ReadGitState compares the indexed files (path to git blob object id, as
// the scanner computed them from the bytes it read) with HEAD's tree. Every
// git call is local; failures leave the state unknown rather than failing
// the run.
func ReadGitState(ctx context.Context, root string, indexed map[string]string) GitState {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return GitState{}
	}
	git := func(args ...string) ([]byte, error) {
		return GitCommand(ctx, absolute, args...).Output()
	}
	out, err := git("rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return GitState{}
	}
	state := GitState{Commit: strings.TrimSpace(string(out))}
	dirty := true
	state.Dirty = &dirty
	// A nested checkout's HEAD is not the indexed tree's commit.
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || !samePath(strings.TrimSpace(string(top)), absolute) {
		return GitState{}
	}
	tree, err := git("ls-tree", "-r", "-z", "--full-tree", state.Commit)
	if err != nil {
		return state
	}
	blobs, err := parseTree(tree)
	if err != nil {
		return state
	}
	for p, oid := range indexed {
		entry, ok := blobs[p]
		if !ok || oid == "" || entry.oid != oid {
			return state
		}
	}
	for p := range blobs {
		if _, ok := indexed[p]; ok {
			continue
		}
		if _, err := os.Lstat(filepath.Join(absolute, filepath.FromSlash(p))); err != nil {
			return state
		}
	}
	dirty = false
	return state
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}

// TreeEntry is one entry of `git ls-tree -r -z` output.
type TreeEntry struct {
	Mode, Type, OID, Path string
}

type blob struct{ mode, oid string }

// parseTree keeps the regular files (mode 100644 and 100755) of a tree
// listing.
func parseTree(data []byte) (map[string]blob, error) {
	entries, err := ParseTree(data)
	if err != nil {
		return nil, err
	}
	out := map[string]blob{}
	for _, e := range entries {
		if e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") {
			out[e.Path] = blob{e.Mode, e.OID}
		}
	}
	return out, nil
}

// ParseTree parses `git ls-tree -r -z` output: "<mode> <type> <oid>\t<path>"
// records separated by NUL. internal/gitref materialises refs from the same
// listing.
func ParseTree(data []byte) ([]TreeEntry, error) {
	var out []TreeEntry
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		meta, p, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 {
			return nil, errors.New("malformed git ls-tree record")
		}
		out = append(out, TreeEntry{Mode: fields[0], Type: fields[1], OID: fields[2], Path: string(p)})
	}
	return out, nil
}
