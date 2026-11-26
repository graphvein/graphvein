package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// digestTree hashes the sorted relative file list and every file's content,
// so any change an agent makes to the workspace changes the digest.
func digestTree(root string) (string, int, error) {
	type entry struct{ rel, sum string }
	var entries []entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			if rel != "." {
				entries = append(entries, entry{rel + "/", "dir"})
			}
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, entry{rel, "link:" + target})
			return nil
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel, sum})
		return nil
	})
	if err != nil {
		return "", 0, fmt.Errorf("digesting %s: %w", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	files := 0
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", e.rel, e.sum)
		if e.sum != "dir" {
			files++
		}
	}
	return hex.EncodeToString(h.Sum(nil)), files, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// makeReadOnly removes write permission from every file and directory. On
// Windows os.Chmod sets the read-only attribute of files, which is the
// closest equivalent; directories there stay writable, which the digest
// check after every run covers.
func makeReadOnly(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()&^0o222)
	})
	if err != nil {
		return fmt.Errorf("making %s read-only: %w", root, err)
	}
	// Deepest directories first, so the walk above never needed write access.
	for i := len(dirs) - 1; i >= 0; i-- {
		info, err := os.Stat(dirs[i])
		if err != nil {
			return err
		}
		if err := os.Chmod(dirs[i], info.Mode().Perm()&^0o222); err != nil {
			return fmt.Errorf("making %s read-only: %w", dirs[i], err)
		}
	}
	return nil
}
