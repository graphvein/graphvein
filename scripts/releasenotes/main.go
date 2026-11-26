// Command releasenotes prints a release notes file from docs/releases/ as
// the body of a GitHub release. The release workflow
// (.github/workflows/release.yml) passes its output to `gh release create`.
//
//	go run ./scripts/releasenotes docs/releases/v0.1.0.md > notes.md
//
// The files in docs/releases/ are wrapped at about 75 columns so they read
// well in the repository. A GitHub release body keeps every line break as
// a hard break, so the same text would show sentences broken in the
// middle. This command joins the soft-wrapped lines of each paragraph,
// list item and block quote into one line. It keeps headings, blank lines,
// fenced code blocks, indented code blocks, tables, HTML lines, thematic
// breaks and explicit hard breaks (two trailing spaces or a backslash) as
// they are. It removes HTML comments, which are notes for the maintainer,
// and drops a leading level-1 heading, because the release title repeats
// it.
//
// It covers the Markdown the release notes use, not all of CommonMark:
// a comment marker inside a code span is removed like any other.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: releasenotes FILE")
		os.Exit(2)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		slog.Error("reading release notes", "err", err)
		os.Exit(1)
	}
	body, err := Unwrap(string(src))
	if err != nil {
		slog.Error("converting release notes", "file", os.Args[1], "err", err)
		os.Exit(1)
	}
	if strings.TrimSpace(body) == "" {
		slog.Error("release notes are empty", "file", os.Args[1])
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString(body); err != nil {
		slog.Error("writing release notes", "err", err)
		os.Exit(1)
	}
}

var (
	headingRe  = regexp.MustCompile(`^ {0,3}#{1,6}(\s|$)`)
	h1Re       = regexp.MustCompile(`^ {0,3}#(\s|$)`)
	listItemRe = regexp.MustCompile(`^\s*([-*+]|\d{1,9}[.)])(\s|$)`)
	quoteRe    = regexp.MustCompile(`^ {0,3}>`)
	// A thematic break, or the underline of a setext heading: kept on its
	// own line, after the joined paragraph it may underline.
	ruleRe  = regexp.MustCompile(`^ {0,3}(-[ \t]*){3,}$|^ {0,3}(\*[ \t]*){3,}$|^ {0,3}(_[ \t]*){3,}$|^ {0,3}=+[ \t]*$`)
	fenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
)

// block kinds of the line being built.
const (
	kindNone = iota
	kindText
	kindQuote
)

// Unwrap returns the release body for the Markdown text src, as the
// package comment describes. It fails on an unterminated code fence or
// HTML comment, which would otherwise swallow the rest of the notes.
func Unwrap(src string) (string, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []string
	cur, kind := "", kindNone // the paragraph, list item or quote being joined
	inList := false           // the last block was a list item or its continuation
	fence := ""               // the opening fence while inside a fenced code block
	inComment := false
	seenContent := false

	flush := func() {
		if kind != kindNone {
			out = append(out, cur)
		}
		cur, kind = "", kindNone
	}
	blank := func() {
		flush()
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}

	for _, line := range strings.Split(src, "\n") {
		if fence != "" {
			out = append(out, line)
			if t := strings.TrimSpace(line); strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}

		// HTML comments are removed; a line that held only a comment goes.
		var removed bool
		line, inComment, removed = stripComments(line, inComment)
		if removed && strings.TrimSpace(line) == "" {
			continue
		}

		switch {
		case strings.TrimSpace(line) == "":
			blank()
			continue
		case !seenContent && h1Re.MatchString(line):
			// The release title repeats the leading heading.
			seenContent = true
			continue
		}
		seenContent = true

		switch {
		case fenceRe.MatchString(line):
			flush()
			m := fenceRe.FindStringSubmatch(line)
			fence = m[1]
			out = append(out, line)
		case kind == kindNone && !inList && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")):
			// Indented code block: kept line by line.
			out = append(out, line)
		case headingRe.MatchString(line), ruleRe.MatchString(line),
			kind == kindNone && (strings.HasPrefix(strings.TrimSpace(line), "|") || strings.HasPrefix(strings.TrimSpace(line), "<")):
			// Headings, breaks, table rows and HTML lines stay as they are.
			flush()
			out = append(out, line)
			inList = false
		case listItemRe.MatchString(line):
			flush()
			cur, kind = line, kindText
			inList = true
		case quoteRe.MatchString(line):
			text := strings.TrimPrefix(strings.TrimLeft(line, " "), ">")
			if kind == kindQuote && !hardBreak(cur) {
				cur = joinLine(cur, text)
			} else {
				flush()
				cur, kind = line, kindQuote
			}
			inList = false
		case kind != kindNone && !hardBreak(cur):
			cur = joinLine(cur, line)
		default:
			// A new paragraph, or the line after a hard break.
			flush()
			cur, kind = line, kindText
			if !startsIndented(line) {
				inList = false
			}
		}
	}
	flush()
	if fence != "" {
		return "", fmt.Errorf("code fence %s is not closed", fence)
	}
	if inComment {
		return "", fmt.Errorf("HTML comment is not closed")
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", nil
	}
	return strings.Join(out, "\n") + "\n", nil
}

// stripComments removes the HTML comments of one line. inComment says
// whether the line starts inside a comment; the results are the rest of
// the line, whether it ends inside a comment, and whether anything was
// removed (so a line that held only a comment is not taken for a blank
// line, which would end a paragraph).
func stripComments(line string, inComment bool) (rest string, open, removed bool) {
	var b strings.Builder
	for {
		if inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return b.String(), true, true
			}
			line = line[end+len("-->"):]
			inComment = false
			removed = true
			continue
		}
		start := strings.Index(line, "<!--")
		if start < 0 {
			b.WriteString(line)
			return b.String(), false, removed
		}
		b.WriteString(line[:start])
		line = line[start+len("<!--"):]
		inComment = true
	}
}

// hardBreak reports whether a line ends with a Markdown hard line break,
// which must stay a line break.
func hardBreak(line string) bool {
	return strings.HasSuffix(line, "  ") || strings.HasSuffix(line, "\\")
}

// joinLine appends a soft-wrapped continuation line to the line being
// built, with one space between them.
func joinLine(cur, next string) string {
	return strings.TrimRight(cur, " \t") + " " + strings.TrimLeft(next, " \t")
}

func startsIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}
