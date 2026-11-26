package dart

import (
	"path"
	"regexp"
	"strings"
)

// packageName is the shape of a pub package name that PackageNames accepts
// and Link reads from `package:` URIs.
var packageName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// nameLine matches an unindented `name:` line with a plain or quoted value
// and an optional trailing comment.
var nameLine = regexp.MustCompile(`^name:[ \t]*(?:([A-Za-z_][A-Za-z0-9_]*)|'([A-Za-z_][A-Za-z0-9_]*)'|"([A-Za-z_][A-Za-z0-9_]*)")[ \t]*(?:#.*)?$`)

// PackageNames reads the package name of every scanned pubspec.yaml, keyed
// by its directory ("." at the repository root). Build files are out of
// scope for the best-effort tier, so it reads one top-level scalar only:
// the first unindented `name:` line (T021 open point O10 (a), like Go's
// go.mod module line). A pubspec without such a line, or with a value that
// is not a package name, maps to "": its package is unknown, and Link then
// claims nothing for `package:` URIs that could name it. Relinking reads it
// anew, so editing a pubspec.yaml does not require reparsing Dart files.
func PackageNames(sources map[string][]byte) map[string]string {
	out := map[string]string{}
	for p, source := range sources {
		if path.Base(p) != "pubspec.yaml" {
			continue
		}
		out[path.Dir(p)] = readName(source)
	}
	return out
}

func readName(source []byte) string {
	text := strings.TrimPrefix(string(source), "\uFEFF")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, "name:") {
			continue
		}
		m := nameLine.FindStringSubmatch(line)
		if m == nil {
			return ""
		}
		return m[1] + m[2] + m[3]
	}
	return ""
}
