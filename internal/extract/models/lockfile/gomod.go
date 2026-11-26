package lockfile

import (
	"strconv"
	"strings"
)

// GoMod is what the Go model front end (GORM, T018g5) reads from a go.mod:
// the module path and the require and replace directives. Since Go 1.17 a
// go.mod lists every module the build selects, so its require lines are
// the pins (assumption GO-V1); there is no separate lockfile to read.
type GoMod struct {
	// Module is the module path ("" when the file declares none); Line its
	// 1-based line.
	Module string
	Line   int
	// Requires are the require directives in file order, single-line and
	// block forms alike.
	Requires []GoRequire
	// Replaces are the replace directives in file order.
	Replaces []GoReplace
}

// GoRequire is one required module version.
type GoRequire struct {
	Path, Version string
	// Indirect marks a `// indirect` comment on the line.
	Indirect bool
	Line     int
}

// GoReplace is one replace directive: Path (and Version when the directive
// names one) is replaced by NewPath at NewVersion; NewVersion is empty for
// a directory replacement.
type GoReplace struct {
	Path, Version, NewPath, NewVersion string
	Line                               int
}

// ParseGoMod reads a go.mod. It is deliberately small: the directives GORM
// discovery needs (module, require, replace) in their single-line and block
// forms, with quoted or bare paths; every other directive is skipped, and a
// line it cannot read is skipped rather than failing the file, so a damaged
// go.mod gives fewer pins (each then reported missing), never wrong ones.
func ParseGoMod(data []byte) GoMod {
	var out GoMod
	block := ""
	for i, raw := range strings.Split(string(data), "\n") {
		n := i + 1
		text, comment, _ := strings.Cut(raw, "//")
		words := goModWords(text)
		indirect := strings.TrimSpace(comment) == "indirect" || strings.HasPrefix(strings.TrimSpace(comment), "indirect;")
		if block != "" {
			if len(words) == 1 && words[0] == ")" {
				block = ""
				continue
			}
			out.directive(block, words, indirect, n)
			continue
		}
		if len(words) == 0 {
			continue
		}
		if len(words) == 2 && words[1] == "(" {
			block = words[0]
			continue
		}
		if words[0] == "module" && len(words) == 2 {
			out.Module, out.Line = words[1], n
			continue
		}
		out.directive(words[0], words[1:], indirect, n)
	}
	return out
}

// directive records one require or replace entry (the words after the verb).
func (m *GoMod) directive(verb string, words []string, indirect bool, line int) {
	switch verb {
	case "require":
		if len(words) == 2 {
			m.Requires = append(m.Requires, GoRequire{Path: words[0], Version: words[1], Indirect: indirect, Line: line})
		}
	case "replace":
		left, right, ok := splitArrow(words)
		if !ok || len(left) < 1 || len(left) > 2 || len(right) < 1 || len(right) > 2 {
			return
		}
		r := GoReplace{Path: left[0], NewPath: right[0], Line: line}
		if len(left) == 2 {
			r.Version = left[1]
		}
		if len(right) == 2 {
			r.NewVersion = right[1]
		}
		m.Replaces = append(m.Replaces, r)
	}
}

// splitArrow splits a replace directive's words at "=>".
func splitArrow(words []string) ([]string, []string, bool) {
	for i, w := range words {
		if w == "=>" {
			return words[:i], words[i+1:], true
		}
	}
	return nil, nil, false
}

// goModWords splits a go.mod line into words, unquoting "..." and `...`
// paths as the go command does.
func goModWords(line string) []string {
	var out []string
	for _, w := range strings.Fields(line) {
		if len(w) >= 2 && (w[0] == '"' || w[0] == '`') {
			if s, err := strconv.Unquote(w); err == nil {
				w = s
			}
		}
		out = append(out, w)
	}
	return out
}
