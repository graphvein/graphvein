package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnwrap covers each Markdown form the release notes use: what is
// joined, what is kept line by line and what is removed.
func TestUnwrap(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"paragraph joined", "One two\nthree four\n  five.\n", "One two three four five.\n"},
		{"leading H1 dropped", "# Tool v1\n\nText.\n", "Text.\n"},
		{"only the leading H1 dropped", "Intro.\n\n# Later\n\nText.\n", "Intro.\n\n# Later\n\nText.\n"},
		{"headings kept", "# T\n\n## Part\nText\nmore.\n### Sub\n", "## Part\nText more.\n### Sub\n"},
		{"list items joined one by one",
			"- first item\n  wraps here\n- second\n* third\n  wraps\n1. one\n   wraps\n2) two\n",
			"- first item wraps here\n- second\n* third wraps\n1. one wraps\n2) two\n"},
		{"nested list and continuation paragraph",
			"- outer\n  wraps\n  - inner\n    wraps\n\n  more of outer\n  wraps\n",
			"- outer wraps\n  - inner wraps\n\n  more of outer wraps\n"},
		{"block quote joined", "> quoted\n> wraps\nlazily\n", "> quoted wraps lazily\n"},
		{"fenced code kept",
			"Run:\n\n```bash\ngraphvein index\ngraphvein serve .\n```\n\nThen\nmore.\n",
			"Run:\n\n```bash\ngraphvein index\ngraphvein serve .\n```\n\nThen more.\n"},
		{"tilde fence with inner backticks", "~~~\n```\nx\n~~~\n", "~~~\n```\nx\n~~~\n"},
		{"indented code kept", "Text\nmore.\n\n    code one\n    code two\n", "Text more.\n\n    code one\n    code two\n"},
		{"hard breaks kept", "Line one  \nline two\\\nline three\nwraps.\n", "Line one  \nline two\\\nline three wraps.\n"},
		{"comment block removed",
			"# T\n\n<!-- a note\nfor the maintainer -->\n\nText\nwraps.\n",
			"Text wraps.\n"},
		{"comment inside a paragraph", "Text <!-- hidden -->\n<!-- whole line -->\nwraps.\n", "Text wraps.\n"},
		{"table kept", "| a | b |\n| - | - |\n| 1 | 2 |\n", "| a | b |\n| - | - |\n| 1 | 2 |\n"},
		{"html line kept", "<p>\n<img src=x>\n</p>\n", "<p>\n<img src=x>\n</p>\n"},
		{"autolink continuation joined", "See\n<https://example.com>\nnow.\n", "See <https://example.com> now.\n"},
		{"thematic break and setext", "Title\n===\nText\n\n---\n\nEnd\n", "Title\n===\nText\n\n---\n\nEnd\n"},
		{"blank lines collapsed and trimmed", "\n\nA\n\n\n\nB\n\n\n", "A\n\nB\n"},
		{"CRLF", "A\r\nb\r\n\r\nC\r\n", "A b\n\nC\n"},
		{"empty", "# Only a title\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Unwrap(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("Unwrap(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// TestUnwrapErrors: an unterminated fence or comment would hide the rest
// of the notes, so the release fails instead.
func TestUnwrapErrors(t *testing.T) {
	for _, in := range []string{"Text\n\n```\ncode\n", "Text\n<!-- note\n\nMore.\n"} {
		if _, err := Unwrap(in); err == nil {
			t.Errorf("Unwrap(%q): no error", in)
		}
	}
}

// TestReleaseNotes converts every committed release notes file: each one
// must give a non-empty body without comments or a leading title, in which
// no line of a paragraph or list item was left wrapped, and converting the
// body again changes nothing.
func TestReleaseNotes(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "docs", "releases", "v*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no docs/releases/v*.md")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			body, err := Unwrap(string(src))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(body) == "" {
				t.Fatal("empty body")
			}
			if strings.Contains(body, "<!--") || strings.Contains(body, "-->") {
				t.Error("body keeps an HTML comment")
			}
			if strings.HasPrefix(body, "# ") {
				t.Error("body keeps the leading title")
			}
			again, err := Unwrap(body)
			if err != nil || again != body {
				t.Errorf("converting the body again changed it (err %v)", err)
			}
		})
	}
}

// TestV010 pins a few lines of the first release's body, which was the
// one published with lines broken mid-sentence.
func TestV010(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "docs", "releases", "v0.1.0.md"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := Unwrap(string(src))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(body, "\n")
	if lines[0] != "The first public release of Graphvein: a local tool that maps a codebase from its architecture overview down to the exact function or line, and labels every fact with how it was found." {
		t.Errorf("first line %q", lines[0])
	}
	var headings, items []string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "#"):
			headings = append(headings, l)
		case strings.HasPrefix(l, "- "):
			items = append(items, l)
		}
	}
	want := []string{"## What is in it", "## Install", "## Known gaps", "## Reporting problems"}
	if strings.Join(headings, "|") != strings.Join(want, "|") {
		t.Errorf("headings %q, want %q", headings, want)
	}
	if len(items) != 4 || !strings.HasSuffix(items[3], "[MCP benchmark](https://github.com/graphvein/graphvein/blob/v0.1.0/docs/evidence/benchmark/README.md).") {
		t.Errorf("list items %q", items)
	}
}
