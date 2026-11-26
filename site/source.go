package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// excludeList is the repository's list of private paths. The generator
// reads it from the repository itself so that the site and the public
// snapshot can never disagree about what is private.
const excludeList = "scripts/public-exclude.txt"

// publicTree reads files of the repository, but only files that belong to
// the public snapshot: every path is checked against the private paths of
// scripts/public-exclude.txt before it is opened. The generator never
// reads the repository any other way, so nothing private can reach the
// site.
type publicTree struct {
	root     string
	excluded []string // entries of the exclude list; a trailing "/" marks a directory
	read     map[string]bool
}

// openPublicTree loads the exclude list of the repository at root.
func openPublicTree(root string) (*publicTree, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(excludeList)))
	if err != nil {
		return nil, fmt.Errorf("reading the private path list: %w", err)
	}
	t := &publicTree{root: root, read: map[string]bool{}}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") || strings.Contains(line, "..") {
			return nil, fmt.Errorf("%s: bad entry %q", excludeList, line)
		}
		t.excluded = append(t.excluded, line)
	}
	if len(t.excluded) == 0 {
		return nil, fmt.Errorf("%s lists no private paths", excludeList)
	}
	t.read[excludeList] = true
	return t, nil
}

// isPrivate reports whether rel (slash-separated, relative to the root)
// is a private path or lies inside one.
func (t *publicTree) isPrivate(rel string) bool {
	rel = path.Clean(rel)
	for _, e := range t.excluded {
		if strings.HasSuffix(e, "/") {
			dir := strings.TrimSuffix(e, "/")
			if rel == dir || strings.HasPrefix(rel, dir+"/") {
				return true
			}
		} else if rel == e {
			return true
		}
	}
	return false
}

// errPrivate is returned for any attempt to read a private path.
var errPrivate = errors.New("private path: not part of the public tree")

// ReadFile returns the content of a public file.
func (t *publicTree) ReadFile(rel string) ([]byte, error) {
	rel = path.Clean(rel)
	if strings.HasPrefix(rel, "../") || rel == ".." || path.IsAbs(rel) {
		return nil, fmt.Errorf("reading %s: outside the repository", rel)
	}
	if t.isPrivate(rel) {
		return nil, fmt.Errorf("reading %s: %w", rel, errPrivate)
	}
	data, err := os.ReadFile(filepath.Join(t.root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rel, err)
	}
	t.read[rel] = true
	return data, nil
}

// Exists reports whether a public file or directory exists at rel.
func (t *publicTree) Exists(rel string) (isDir, ok bool) {
	rel = path.Clean(rel)
	if strings.HasPrefix(rel, "../") || rel == ".." || t.isPrivate(rel) {
		return false, false
	}
	info, err := os.Stat(filepath.Join(t.root, filepath.FromSlash(rel)))
	if err != nil {
		return false, false
	}
	return info.IsDir(), true
}

// Markdown lists the public .md files under a directory, recursively, or
// the file itself when rel names a file. Private paths are skipped.
func (t *publicTree) Markdown(rel string) ([]string, error) {
	isDir, ok := t.Exists(rel)
	if !ok {
		return nil, fmt.Errorf("listing %s: not a public path", rel)
	}
	if !isDir {
		return []string{path.Clean(rel)}, nil
	}
	var files []string
	base := filepath.Join(t.root, filepath.FromSlash(rel))
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, err := filepath.Rel(t.root, p)
		if err != nil {
			return err
		}
		r = filepath.ToSlash(r)
		if t.isPrivate(r) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(r, ".md") {
			files = append(files, r)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", rel, err)
	}
	sort.Strings(files)
	return files, nil
}

// ReadPaths lists every repository path the generator has read, for the
// privacy check.
func (t *publicTree) ReadPaths() []string {
	var out []string
	for p := range t.read {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
