package server

import (
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines a hunk shows around a change,
// as in `diff -u`.
const diffContext = 3

// diffCells bounds the longest-common-subsequence table of the changed
// middle part. Edits from the UI change a few lines, so the bound is only
// reached by a manifest rewritten from scratch; the diff is then one hunk
// that replaces the middle, still exact, just not minimal.
const diffCells = 1 << 20

// unifiedDiff returns the unified diff (3 lines of context) that turns old
// into new, with name in the --- and +++ lines, or "" when they are equal.
// It is shown to the user before a manifest edit is written (T022 B10), so
// it must be exact: applying it to old gives new.
func unifiedDiff(name, old, new string) string {
	if old == new {
		return ""
	}
	a, b := splitLines(old), splitLines(new)
	ops := diffLines(a, b)
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", name, name)
	// Walk the edit script and emit hunks around changed lines.
	type line struct {
		kind byte // ' ', '-', '+'
		text string
		ai   int // position in a before this line
		bi   int // position in b before this line
	}
	var lines []line
	ai, bi := 0, 0
	for _, k := range ops {
		switch k {
		case ' ':
			lines = append(lines, line{' ', a[ai], ai, bi})
			ai, bi = ai+1, bi+1
		case '-':
			lines = append(lines, line{'-', a[ai], ai, bi})
			ai++
		case '+':
			lines = append(lines, line{'+', b[bi], ai, bi})
			bi++
		}
	}
	for i := 0; i < len(lines); {
		if lines[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-diffContext, 0)
		end := i
		// Extend the hunk while the next change is within 2*context lines.
		for j := i; j < len(lines); j++ {
			if lines[j].kind != ' ' {
				end = j
				continue
			}
			if j-end > 2*diffContext {
				break
			}
		}
		stop := min(end+diffContext+1, len(lines))
		oldCount, newCount := 0, 0
		for _, l := range lines[start:stop] {
			if l.kind != '+' {
				oldCount++
			}
			if l.kind != '-' {
				newCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", hunkRange(lines[start].ai, oldCount), hunkRange(lines[start].bi, newCount))
		for _, l := range lines[start:stop] {
			out.WriteByte(l.kind)
			out.WriteString(strings.TrimSuffix(l.text, "\n"))
			out.WriteByte('\n')
			if !strings.HasSuffix(l.text, "\n") {
				out.WriteString("\\ No newline at end of file\n")
			}
		}
		i = stop
	}
	return out.String()
}

// hunkRange is a hunk header range: the first line (1-based) and the
// count, or the line before an empty range.
func hunkRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

// splitLines splits s after every newline; the last line has none when s
// does not end with one.
func splitLines(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// diffLines returns an edit script (' ' keep, '-' delete from a, '+' insert
// from b): the common prefix and suffix are kept, the middle is diffed by
// longest common subsequence when its table fits diffCells.
func diffLines(a, b []string) []byte {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	ops := make([]byte, 0, len(a)+len(b))
	for range pre {
		ops = append(ops, ' ')
	}
	if (len(ma)+1)*(len(mb)+1) > diffCells {
		for range ma {
			ops = append(ops, '-')
		}
		for range mb {
			ops = append(ops, '+')
		}
	} else {
		// lcs[i][j] is the LCS length of ma[i:] and mb[j:].
		w := len(mb) + 1
		lcs := make([]int32, (len(ma)+1)*w)
		for i := len(ma) - 1; i >= 0; i-- {
			for j := len(mb) - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
				} else {
					lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(ma) || j < len(mb) {
			switch {
			case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
				ops = append(ops, ' ')
				i, j = i+1, j+1
			case i < len(ma) && (j == len(mb) || lcs[(i+1)*w+j] >= lcs[i*w+j+1]):
				ops = append(ops, '-')
				i++
			default:
				ops = append(ops, '+')
				j++
			}
		}
	}
	for range suf {
		ops = append(ops, ' ')
	}
	return ops
}
