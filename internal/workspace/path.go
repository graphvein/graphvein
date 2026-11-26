package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SamePath reports whether a and b (absolute, clean) name the same
// folder. Equal strings do; strings that differ only in letter case do
// when both exist and the file system says they are one folder
// (os.SameFile), which is what a case-insensitive file system (Windows,
// macOS by default) makes of them. Two spellings are never called the
// same from their text alone: on a case-sensitive volume they are two
// folders, and a folder that is gone cannot be asked.
func SamePath(a, b string) bool {
	if a == b {
		return true
	}
	if !strings.EqualFold(a, b) {
		return false
	}
	return sameFile(a, b)
}

// Inside reports whether path lies strictly inside dir (both absolute and
// clean), with dir's spelling compared as SamePath compares it.
func Inside(path, dir string) bool {
	sep := string(filepath.Separator)
	prefix := strings.TrimSuffix(dir, sep) + sep
	if strings.HasPrefix(path, prefix) {
		return len(path) > len(prefix)
	}
	if len(path) <= len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
		return false
	}
	return sameFile(path[:len(prefix)-1], strings.TrimSuffix(dir, sep))
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// realCase returns path with every component spelled as its parent
// folder lists it. On a case-sensitive file system a path that exists is
// already spelled that way, so nothing changes; on a case-insensitive
// one, a component typed in another case is replaced by the listed name
// that os.SameFile confirms. A component that cannot be listed or
// matched is kept as typed. Windows needs none of this:
// filepath.EvalSymlinks already returns the stored casing there.
func realCase(path string) string {
	if runtime.GOOS == "windows" {
		return path
	}
	return realCaseWith(path, listNames, sameFile)
}

func listNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

// realCaseWith is realCase with the folder listing and the same-file test
// passed in, so the rule is tested on every platform.
func realCaseWith(path string, list func(string) ([]string, error), same func(a, b string) bool) string {
	vol := filepath.VolumeName(path)
	rest := path[len(vol):]
	sep := string(filepath.Separator)
	if !strings.HasPrefix(rest, sep) {
		return path
	}
	parts := strings.Split(strings.Trim(rest, sep), sep)
	out := vol + sep
	for _, part := range parts {
		if part == "" {
			return path
		}
		next := filepath.Join(out, part)
		if names, err := list(out); err == nil && !contains(names, part) {
			var match string
			count := 0
			for _, n := range names {
				if strings.EqualFold(n, part) && same(filepath.Join(out, n), next) {
					match = n
					count++
				}
			}
			if count == 1 {
				next = filepath.Join(out, match)
			}
		}
		out = next
	}
	return out
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
