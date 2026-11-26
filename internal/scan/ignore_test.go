package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// referenceIgnored is the matcher used before T016a, kept to prove that the
// faster matcher takes exactly the same decisions.
func referenceIgnored(p string, directory bool, rules []rule) bool {
	result := false
	for _, r := range rules {
		rel := p
		if r.base != "" {
			if !strings.HasPrefix(p, r.base+"/") {
				continue
			}
			rel = strings.TrimPrefix(p, r.base+"/")
		}
		if (!r.directory || directory) && r.pattern.MatchString(rel) {
			result = !r.negate
		}
	}
	return result
}

// syntheticIgnores covers gitignore syntax the fixture files do not use.
const syntheticIgnores = `*.pyc
*.py[cod]
build/
/dist
docs/_build/
**/node_modules
foo/**
a/**/b
**/x?y
\#hash
\!bang
!keep.ts
*~
.env*
*.egg-info/
[!a]b
x[!a]b
[abc]
unclosed[
lit\*star
*mid*
pre*suf
?
q?r
/**
abc/
trailing
esc\
sub/anchored.txt
/root-only
*.min.*
!*.min.keep.js
**
*
`

// nonASCIIIgnores covers literals the regular expression reads as runes while
// the fast paths compare bytes: U+FFFD (which an invalid UTF-8 byte in a path
// decodes to), accented letters and astral characters.
const nonASCIIIgnores = "*\uFFFD\n\uFFFD*\n\uFFFD\n/\uFFFD\n/\uFFFD*\na\uFFFDb\n*\uFFFD*\n" +
	"*\u00e9\n\u00e9*\ncaf\u00e9\n/caf\u00e9*\nna\u00efve/\n" +
	"*\U0001F600\n\U0001F600*\n/dir/\U0001F600*\nx\U0001F600y\n?\uFFFD\n\u00e9?*\n!keep\uFFFD\n"

// ignoreSources returns every ignore file under testdata (stored under inert
// names), the repository's own .gitignore and the synthetic patterns.
func ignoreSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{"synthetic": syntheticIgnores, "non-ASCII": nonASCIIIgnores}
	err := filepath.WalkDir("../../testdata", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch d.Name() {
		case "_gitignore", "_graphveinignore", ".gitignore", ".graphveinignore":
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sources[filepath.ToSlash(p)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	sources["repository .gitignore"] = string(data)
	if len(sources) < 4 {
		t.Fatalf("found only %d ignore sources", len(sources))
	}
	return sources
}

// candidatePaths derives paths from the patterns themselves: segments with and
// without each pattern's literals, wildcards expanded across '/', nested under
// several directories including the rule base.
func candidatePaths(content string) []string {
	segments := map[string]bool{"a": true, "keep.ts": true, "x.ts": true, "README.md": true}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		line = strings.Trim(line, "/")
		if line == "" {
			continue
		}
		for _, star := range []string{"", "z", "z/z"} {
			for _, question := range []string{"q", "/"} {
				for _, bracket := range []string{"c", "a", "/"} {
					p := strings.NewReplacer("**", star+"/"+star, "*", star, "?", question, `\`, "", "[!a]", bracket, "[abc]", bracket, "[cod]", bracket).Replace(line)
					p = strings.Trim(strings.ReplaceAll(p, "//", "/"), "/")
					if p != "" {
						segments[p] = true
					}
				}
			}
		}
		literal := strings.Trim(strings.NewReplacer("*", "", "?", "", "[", "", "]", "", "!", "", `\`, "").Replace(line), "/")
		if literal != "" {
			for _, s := range []string{literal, "x" + literal, literal + "x", strings.ToUpper(literal), "other"} {
				segments[s] = true
			}
		}
	}
	// Paths need not be valid UTF-8: add invalid bytes around each segment,
	// in place of U+FFFD and in place of a truncated multi-byte character.
	// Iterate a sorted copy of the keys: adding to a map while ranging over
	// it may or may not visit the new keys, which made the set (and a
	// slice of an empty segment) depend on iteration order.
	base := make([]string, 0, len(segments))
	for s := range segments {
		base = append(base, s)
	}
	sort.Strings(base)
	for _, s := range base {
		segments["\xff"+s] = true
		segments[s+"\xff"] = true
		segments[strings.ReplaceAll(s, "\uFFFD", "\xff")] = true
		segments[strings.ReplaceAll(s, "\uFFFD", "\x80\x80")] = true
		if r, size := utf8.DecodeLastRuneInString(s); size > 0 && r >= utf8.RuneSelf {
			if len(s) > 1 {
				segments[s[:len(s)-1]] = true
			}
			segments[s[:len(s)-size]+"\xff"] = true
		}
	}
	delete(segments, "")
	var paths []string
	for s := range segments {
		for _, dir := range []string{"", "a/", "a/b/", "sub/", "sub/a/", "x/sub/", "docs/"} {
			paths = append(paths, dir+s, dir+s+"/child.ts")
		}
	}
	return paths
}

func TestIgnoreMatcherMatchesReference(t *testing.T) {
	root := t.TempDir()
	compared := 0
	for name, content := range ignoreSources(t) {
		var sets, full [][]rule
		for _, base := range []string{"", "sub"} {
			write(t, root, filepath.Join(base, "rules"), content)
			rules, err := readRules(root, base, "rules")
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			sets = append(sets, rules)
			full = append(full, rules)
			for i := range rules {
				sets = append(sets, rules[i:i+1])
			}
		}
		// Nested files: the root's rules, then the subdirectory's.
		sets = append(sets, append(append([]rule{}, full[0]...), full[1]...))
		paths := candidatePaths(content)
		for _, rules := range sets {
			for _, p := range paths {
				for _, directory := range []bool{false, true} {
					compared++
					if got, want := ignored(p, directory, rules), referenceIgnored(p, directory, rules); got != want {
						t.Fatalf("%s: ignored(%q, directory=%v) = %v, reference %v (first rule %q)", name, p, directory, got, want, rules[0].pattern)
					}
				}
			}
		}
	}
	t.Logf("compared %d decisions", compared)
}
