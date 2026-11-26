// Package testscope classifies paths without changing scanner visibility.
package testscope

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Scope is omitted for production so existing graph attributes stay compatible.
type Scope string

// Test identifies code intended for tests rather than production serving.
const Test Scope = "test"

// Rules holds validated patterns; its zero value applies only built-in rules.
type Rules struct{ include, exclude []pattern }

// Match exposes explicit pattern matches without applying built-in test names,
// so discovery configuration can share exactly the test-scope pattern syntax.
func (r Rules) Match(file string) (included, excluded bool) {
	parts := strings.Split(file, "/")
	for _, p := range r.include {
		included = included || p.matches(parts)
	}
	for _, p := range r.exclude {
		excluded = excluded || p.matches(parts)
	}
	return included, excluded
}

type pattern struct {
	segments            []string
	anchored, directory bool
}

var testBasename = regexp.MustCompile(`^(?:test_.*\.py|.*_test\.py|conftest\.py|.*_test\.go|(?:.*\.)?(?:spec|test)+\.[mc]?[jt]sx?|.*\.cy\.(?:js|jsx|ts|tsx))$`)

// Compile validates configuration once, keeping classification independent of I/O.
func Compile(include, exclude []string) (Rules, error) {
	r := Rules{}
	for i, list := range [][]string{include, exclude} {
		for _, text := range list {
			p, err := compile(text)
			if err != nil {
				return Rules{}, err
			}
			if i == 0 {
				r.include = append(r.include, p)
			} else {
				r.exclude = append(r.exclude, p)
			}
		}
	}
	return r, nil
}

func compile(text string) (pattern, error) {
	if text == "" || strings.ContainsAny(text, "\x00\\") || strings.HasPrefix(text, "!") {
		return pattern{}, fmt.Errorf("invalid test-scope pattern %q: empty, NUL, leading ! and backslash escapes are forbidden", text)
	}
	p := pattern{directory: strings.HasSuffix(text, "/")}
	trimmed := strings.TrimSuffix(text, "/")
	p.anchored = strings.Contains(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	p.segments = strings.Split(trimmed, "/")
	for i, s := range p.segments {
		// Go's glob syntax uses ^ for class negation; gitignore also accepts !.
		s = strings.ReplaceAll(s, "[!", "[^")
		p.segments[i] = s
		if _, err := path.Match(s, ""); err != nil {
			return pattern{}, fmt.Errorf("invalid test-scope pattern %q: %w", text, err)
		}
	}
	return p, nil
}

// Classify is the single pure path/configuration rule shared by graph consumers.
// Paths are repository-relative and case-sensitive, with forward-slash separators.
func Classify(file string, rules Rules) Scope {
	parts := strings.Split(file, "/")
	builtin := testBasename.MatchString(parts[len(parts)-1])
	for _, s := range parts[:len(parts)-1] {
		switch s {
		case "test", "tests", "__tests__", "__mocks__", "testdata":
			builtin = true
		}
	}
	for _, p := range rules.exclude {
		if p.matches(parts) {
			return ""
		}
	}
	if builtin {
		return Test
	}
	for _, p := range rules.include {
		if p.matches(parts) {
			return Test
		}
	}
	return ""
}

func (p pattern) matches(parts []string) bool {
	end := len(parts)
	if p.directory {
		end--
	}
	for n := 1; n <= end; n++ {
		if p.anchored {
			if matchSegments(p.segments, parts[:n]) {
				return true
			}
		} else {
			for start := 0; start < n; start++ {
				if matchSegments(p.segments, parts[start:n]) {
					return true
				}
			}
		}
	}
	return false
}

func matchSegments(patterns, parts []string) bool {
	if len(patterns) == 0 {
		return len(parts) == 0
	}
	if patterns[0] == "**" {
		for n := 0; n <= len(parts); n++ {
			if matchSegments(patterns[1:], parts[n:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, _ := path.Match(patterns[0], parts[0])
	return ok && matchSegments(patterns[1:], parts[1:])
}
