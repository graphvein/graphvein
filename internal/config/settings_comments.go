package config

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Comment is one YAML comment of a text: its 1-based line and its text
// from the # to the end of the line, without trailing blanks. Line is 0
// when the comment's line could not be found in the text.
type Comment struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// utf8BOM is the byte order mark yaml.v3 accepts at the start of a text.
const utf8BOM = "\xEF\xBB\xBF"

// Comments lists the comments of a YAML text in order. When the text
// parses, the comments are the ones yaml.v3's parser attaches to the node
// tree (HeadComment, LineComment and FootComment of every node): its
// scanner decides what is a comment, so a quote inside a plain scalar
// (`repository: rock 'n roll`), a # inside a quoted or block scalar and a
// byte order mark are read as YAML reads them. Each comment's line is
// then found in the text. A text that does not parse has no node tree;
// its comments come from scanComments, a line scanner.
func Comments(data []byte) []Comment {
	texts, ok := nodeComments(data)
	if !ok {
		return scanComments(data)
	}
	return commentLines(data, texts)
}

// nodeComments are the comment lines of every document of data, from the
// parsed node trees; false when data does not parse.
func nodeComments(data []byte) ([]string, bool) {
	var out []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			for _, line := range strings.Split(c, "\n") {
				if line = strings.TrimSpace(line); strings.HasPrefix(line, "#") {
					out = append(out, line)
				}
			}
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		err := d.Decode(&doc)
		if err == io.EOF {
			return out, true
		}
		if err != nil {
			return nil, false
		}
		walk(&doc)
	}
}

// commentLines places each comment text on its line: candidates are the
// places a # follows the start of a line or a blank, in text order; a
// candidate whose rest of the line is one of the texts not yet placed
// takes it. Texts are returned in line order; one with no candidate left
// keeps line 0 and comes last (the multiset of texts is what LostComments
// compares, so a line not found never hides a loss).
func commentLines(data []byte, texts []string) []Comment {
	left := map[string]int{}
	for _, t := range texts {
		left[t]++
	}
	var out []Comment
	for n, raw := range strings.Split(strings.TrimPrefix(string(data), utf8BOM), "\n") {
		line := strings.TrimRight(raw, "\r")
		for i := 0; i < len(line); i++ {
			if line[i] != '#' || i > 0 && line[i-1] != ' ' && line[i-1] != '\t' {
				continue
			}
			if t := strings.TrimRight(line[i:], " \t"); left[t] > 0 {
				left[t]--
				out = append(out, Comment{Line: n + 1, Text: t})
				break
			}
		}
	}
	for _, t := range texts {
		if left[t] > 0 {
			left[t]--
			out = append(out, Comment{Text: t})
		}
	}
	return out
}

// blockScalarHeader matches the end of a line that opens a literal or
// folded block scalar (`key: |`, `- >-`, `key: |2+`).
var blockScalarHeader = regexp.MustCompile(`(^|[\s:-])[|>][-+]?[1-9]?[-+]?$`)

// quoteStart reports whether a quote at line[i] opens a quoted scalar: a
// quote starts a scalar only as its first character, at the start of the
// line's content, after `- `, `: `, `? ` or a flow indicator (`[`, `{`,
// `,`). A quote inside a plain scalar (`rock 'n roll`) is text.
func quoteStart(line string, i int) bool {
	before := strings.TrimRight(line[:i], " \t")
	if before == "" {
		return true
	}
	if strings.IndexByte("[{,", before[len(before)-1]) >= 0 {
		return true
	}
	if len(before) == i {
		return false // no blank before the quote
	}
	return strings.HasSuffix(before, ":") || before == "-" || strings.HasSuffix(before, " -") || before == "?" || strings.HasSuffix(before, " ?")
}

// scanComments is the line scanner for a text that does not parse: a #
// starts a comment at the start of a line or after a blank, outside a
// quoted scalar and outside the lines of a block scalar (YAML 1.2, 6.6).
// A leading byte order mark is skipped.
func scanComments(data []byte) []Comment {
	var out []Comment
	quote := byte(0)
	block, blockIndent := false, 0
	for n, raw := range strings.Split(strings.TrimPrefix(string(data), utf8BOM), "\n") {
		line := strings.TrimRight(raw, "\r")
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block {
			if strings.TrimSpace(line) == "" || indent > blockIndent {
				continue
			}
			block = false
		}
		code := line
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case quote == '\'':
				if c == '\'' {
					if i+1 < len(line) && line[i+1] == '\'' {
						i++
					} else {
						quote = 0
					}
				}
			case quote == '"':
				if c == '\\' {
					i++
				} else if c == '"' {
					quote = 0
				}
			case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
				out = append(out, Comment{Line: n + 1, Text: strings.TrimRight(line[i:], " \t")})
				code = line[:i]
				i = len(line)
			case (c == '\'' || c == '"') && quoteStart(line, i):
				quote = c
			}
		}
		if quote == 0 && blockScalarHeader.MatchString(strings.TrimRight(code, " \t")) {
			block, blockIndent = true, indent
		}
	}
	return out
}

// LostComments are the comments of before that after no longer has, in
// the order of before: each comment text of after accounts for one
// comment of before with the same text. Moving a comment is not a loss.
// Both texts are read the same way (the parser's comments when both
// parse, else the line scanner for both), so one text's reading never
// differs from the other's only by method.
func LostComments(before, after []byte) []Comment {
	var b, a []Comment
	bt, bok := nodeComments(before)
	at, aok := nodeComments(after)
	if bok && aok {
		b, a = commentLines(before, bt), commentLines(after, at)
	} else {
		b, a = scanComments(before), scanComments(after)
	}
	left := map[string]int{}
	for _, c := range a {
		left[c.Text]++
	}
	out := []Comment{}
	for _, c := range b {
		if left[c.Text] > 0 {
			left[c.Text]--
			continue
		}
		out = append(out, c)
	}
	return out
}
