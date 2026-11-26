package golang

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Module-own import resolution: Link resolves calls through it, and the
// GORM model front end (internal/extract/models/gorm, T018g5) resolves
// model structs, receivers and migrate functions through the same rules,
// so a fix reaches both. Resolution is limited to the module's own
// packages: an import outside the module is never mapped to files.

// ModuleAt returns the directory and module path of the nearest go.mod at
// or above file's directory (modules comes from ModulePaths), or empty
// strings when no scanned go.mod governs the file.
func ModuleAt(file string, modules map[string]string) (string, string) {
	dir := path.Dir(file)
	for {
		if module := modules[dir]; module != "" {
			return dir, module
		}
		if dir == "." {
			break
		}
		dir = path.Dir(dir)
	}
	return "", ""
}

// ModuleImportDir maps an import path to its package directory when the
// path belongs to the module rooted at base with path module; ok is false
// for every other import (the standard library, dependencies).
//
// Both paths must be valid import paths (CheckImportPath): the go command
// rejects a malformed import path before it looks for a directory, and
// path.Join would otherwise clean "..", "." and "//" into a directory the
// go command never reads. A module path that is not valid (the block form
// `module (`, for one) resolves nothing; its go.mod still bounds the
// module (ModuleAt stops there).
func ModuleImportDir(base, module, importPath string) (dir string, ok bool) {
	if module == "" || importPath != module && !strings.HasPrefix(importPath, module+"/") {
		return "", false
	}
	if CheckImportPath(module) != nil || CheckImportPath(importPath) != nil {
		return "", false
	}
	return path.Join(base, strings.TrimPrefix(strings.TrimPrefix(importPath, module), "/")), true
}

// CheckImportPath reports whether p is a valid import path, with the rules
// of golang.org/x/mod/module.CheckImportPath (x/mod as vendored in Go
// 1.27.1, src/cmd/vendor/golang.org/x/mod/module/module.go: checkPath with
// kind importPath, checkElem, importPathOK, badWindowsNames), which the go
// command applies to every import before resolving it
// (cmd/go/internal/modload/import.go, importFromModules). They are
// transcribed here rather than imported: golang.org/x/mod is not a
// dependency of this module, and adding one needs an ADR. A path that
// fails claims nothing (contract row D, form malformed_import_path).
func CheckImportPath(p string) error {
	if !utf8.ValidString(p) {
		return fmt.Errorf("malformed import path %q: invalid UTF-8", p)
	}
	if p == "" {
		return fmt.Errorf("malformed import path %q: empty string", p)
	}
	if p[0] == '-' {
		return fmt.Errorf("malformed import path %q: leading dash", p)
	}
	if strings.Contains(p, "//") {
		return fmt.Errorf("malformed import path %q: double slash", p)
	}
	if p[len(p)-1] == '/' {
		return fmt.Errorf("malformed import path %q: trailing slash", p)
	}
	for _, elem := range strings.Split(p, "/") {
		if err := checkImportElem(elem); err != nil {
			return fmt.Errorf("malformed import path %q: %w", p, err)
		}
	}
	return nil
}

// badWindowsNames are x/mod's reserved Windows file names.
var badWindowsNames = []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}

// checkImportElem is x/mod's checkElem for an import path element.
func checkImportElem(elem string) error {
	if elem == "" {
		return fmt.Errorf("empty path element")
	}
	if strings.Count(elem, ".") == len(elem) {
		return fmt.Errorf("invalid path element %q", elem)
	}
	if elem[len(elem)-1] == '.' {
		return fmt.Errorf("trailing dot in path element")
	}
	for _, r := range elem {
		// importPathOK: ASCII letters and digits, - . _ ~ and +.
		ok := r == '-' || r == '.' || r == '_' || r == '~' || r == '+' || '0' <= r && r <= '9' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z'
		if !ok {
			return fmt.Errorf("invalid char %q", r)
		}
	}
	short := elem
	if i := strings.Index(short, "."); i >= 0 {
		short = short[:i]
	}
	for _, bad := range badWindowsNames {
		if strings.EqualFold(bad, short) {
			return fmt.Errorf("%q disallowed as path element component on Windows", short)
		}
	}
	// Windows short names: a tilde followed by one or more digits.
	if tilde := strings.LastIndexByte(short, '~'); tilde >= 0 && tilde < len(short)-1 {
		digits := true
		for _, r := range short[tilde+1:] {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if digits {
			return fmt.Errorf("trailing tilde and digits in path element")
		}
	}
	return nil
}

// OwnPackageFiles lists the files whose package-level declarations file
// sees without an import (Package): the package's non-test files for one
// of them, plus its internal test files for an internal test file, the
// external test files for an external test file. A file the decided
// package does not hold (one no configuration builds into it) sees only
// itself: the go command builds it alone or not at all. In a directory
// Package refuses no build decides it, so the reading before the review
// fixes stays: the directory's files with the file's package clause, test
// files only for a test file (calls have no contract and stay Best effort).
func OwnPackageFiles(file string, pkg GoPackage, files map[string]facts.Facts) []string {
	if pkg.Refusal != "" {
		var out []string
		test := strings.HasSuffix(file, "_test.go")
		for _, p := range dirGoFiles(path.Dir(file), files) {
			if files[p].Package == files[file].Package && (test || !strings.HasSuffix(p, "_test.go")) {
				out = append(out, p)
			}
		}
		return out
	}
	has := func(list []string) bool {
		for _, p := range list {
			if p == file {
				return true
			}
		}
		return false
	}
	switch {
	case has(pkg.Files):
		return pkg.Files
	case has(pkg.Tests):
		return append(append([]string(nil), pkg.Files...), pkg.Tests...)
	case has(pkg.XTests):
		return pkg.XTests
	}
	return []string{file}
}
