package main

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// heading is one entry of a page's "On this page" list.
type heading struct {
	Level int
	ID    string
	Text  string
}

// rendered is a Markdown document turned into HTML, with what the link
// check and the page layout need from it.
type rendered struct {
	Title    string    // text of the first level-1 heading, removed from HTML
	HTML     string    // body without the title heading
	Headings []heading // level-2 and level-3 headings, in order
	IDs      map[string]bool
}

// linkResolver decides what a link or image destination becomes. It
// returns the new destination, or keep=false to render the link's text
// without a link (a target the site cannot link to).
type linkResolver func(dest string) (newDest string, keep bool)

// markdown converts the public docs with goldmark: GitHub tables,
// strikethrough and autolinks, GitHub-style heading ids, and raw HTML shown
// as text (the docs use <placeholder> words in prose; no doc relies on
// raw HTML, and escaping it keeps every page free of injected markup).
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.Linkify),
	goldmark.WithRendererOptions(
		renderer.WithNodeRenderers(util.Prioritized(rawHTMLAsText{}, 100)),
	),
)

// renderMarkdown parses src, rewrites every link and image through
// resolve, assigns heading ids and renders the HTML.
func renderMarkdown(src []byte, resolve linkResolver) (rendered, error) {
	doc := markdown.Parser().Parse(text.NewReader(src))
	out := rendered{IDs: map[string]bool{}}
	slugs := map[string]int{}

	var unlink []ast.Node
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			t := plainText(n, src)
			id := githubSlug(t, slugs)
			n.SetAttributeString("id", []byte(id))
			out.IDs[id] = true
			if n.Level == 1 && out.Title == "" {
				out.Title = t
			} else if n.Level == 2 || n.Level == 3 {
				out.Headings = append(out.Headings, heading{Level: n.Level, ID: id, Text: t})
			}
		case *ast.Link:
			dest, keep := resolve(string(n.Destination))
			if !keep {
				unlink = append(unlink, n)
			} else {
				n.Destination = []byte(dest)
			}
		case *ast.AutoLink:
			if n.AutoLinkType == ast.AutoLinkURL {
				if _, keep := resolve(string(n.URL(src))); !keep {
					unlink = append(unlink, n)
				}
			}
		case *ast.Image:
			dest, keep := resolve(string(n.Destination))
			if !keep {
				unlink = append(unlink, n)
			} else {
				n.Destination = []byte(dest)
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return out, fmt.Errorf("walking markdown: %w", err)
	}
	for _, n := range unlink {
		replaceWithChildren(n, src)
	}
	// The page title is shown by the layout, so its heading is not rendered
	// twice.
	if first := doc.FirstChild(); first != nil {
		if h, ok := first.(*ast.Heading); ok && h.Level == 1 {
			doc.RemoveChild(doc, first)
		}
	}

	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, src, doc); err != nil {
		return out, fmt.Errorf("rendering markdown: %w", err)
	}
	// Wide tables scroll inside their own box instead of widening the page.
	h := strings.ReplaceAll(buf.String(), "<table>", `<div class="tscroll"><table>`)
	out.HTML = strings.ReplaceAll(h, "</table>", "</table></div>")
	return out, nil
}

// replaceWithChildren puts a link's children in its place, so the text
// stays and the link goes. An autolink or an image becomes plain text.
func replaceWithChildren(n ast.Node, src []byte) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	switch n := n.(type) {
	case *ast.AutoLink:
		parent.ReplaceChild(parent, n, ast.NewString(n.URL(src)))
		return
	case *ast.Image:
		parent.ReplaceChild(parent, n, ast.NewString([]byte(plainText(n, src))))
		return
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

// plainText is the visible text of an inline tree, as GitHub uses it for
// heading anchors.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch n := n.(type) {
		case *ast.Text:
			b.Write(n.Segment.Value(src))
			if n.SoftLineBreak() {
				b.WriteByte(' ')
			}
			return
		case *ast.String:
			b.Write(n.Value)
			return
		case *ast.AutoLink:
			b.Write(n.URL(src))
			return
		case *ast.RawHTML:
			return
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// githubSlug makes the anchor GitHub gives a heading: lower case, letters,
// digits, marks, "-" and "_" kept, spaces turned into "-", everything else
// dropped, and "-1", "-2" ... appended to repeats. The docs link to
// headings by these anchors, so the site must produce the same ones.
func githubSlug(s string, seen map[string]int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsMark(r), r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	base := b.String()
	n, ok := seen[base]
	seen[base] = n + 1
	if !ok {
		return base
	}
	for {
		id := base + "-" + strconv.Itoa(n)
		if _, taken := seen[id]; !taken {
			seen[id] = 1
			return id
		}
		n++
	}
}

// rawHTMLAsText renders raw HTML in the docs as visible text and drops
// HTML comments (the docs use comments only as markers for tests).
type rawHTMLAsText struct{}

func (rawHTMLAsText) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkSkipChildren, nil
		}
		raw := n.(*ast.RawHTML)
		var b bytes.Buffer
		for i := 0; i < raw.Segments.Len(); i++ {
			seg := raw.Segments.At(i)
			b.Write(seg.Value(src))
		}
		if !bytes.HasPrefix(b.Bytes(), []byte("<!--")) {
			_, _ = w.WriteString(html.EscapeString(b.String()))
		}
		return ast.WalkSkipChildren, nil
	})
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block := n.(*ast.HTMLBlock)
		var b bytes.Buffer
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			b.Write(line.Value(src))
		}
		if block.HasClosure() {
			b.Write(block.ClosureLine.Value(src))
		}
		if block.HTMLBlockType == ast.HTMLBlockType2 { // a comment
			return ast.WalkContinue, nil
		}
		_, _ = w.WriteString("<p>" + html.EscapeString(strings.TrimSpace(b.String())) + "</p>\n")
		return ast.WalkContinue, nil
	})
}
