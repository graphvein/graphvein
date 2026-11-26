package query

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
)

// Node keeps presentation metadata outside the indexer contract.
type Node struct {
	contract.Node
	IDE *IDELink `json:"ide"`
}

// IDELink reports whether current source justified the column conversion.
// Exact is true only when every step of the conversion is proven for the
// editor (ADR 0050, Review fixes R2). LineOnly marks a link that names the
// line without a column, because the editor's column cannot be vouched for
// and it has a line-only form (IntelliJ IDEA: a tab before the node, which
// it expands to its own tab size, or a file that changed since indexing):
// Exact is false then and Column is the configured base, the start of the
// line where the editor lands. Caveat says why a link converted from
// unchanged source is still not exact (config.CaveatTabSize,
// config.CaveatColumnUnitAssumed); an inexact link without one comes from a
// changed file or a position that could not be checked. Both are omitted
// when empty, so other links are unchanged.
type IDELink struct {
	Link     string `json:"link"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Exact    bool   `json:"exact"`
	LineOnly bool   `json:"line_only,omitempty"`
	Caveat   string `json:"caveat,omitempty"`
}

type sourceFile struct {
	lines []string
	hash  string
	// read is false when the file could not be read (deleted, renamed or
	// unreadable); hash is empty then.
	read bool
}
type links struct {
	root   string
	config config.IDE
	files  map[string]sourceFile
	// read returns the current bytes of a repository file; nil reads it
	// from root with os.ReadFile.
	read func(file string) ([]byte, error)
}

func encodePath(p string) string {
	const hex = "0123456789ABCDEF"
	p = strings.ReplaceAll(p, `\`, "/")
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) || c == ':' && i == 1 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// absURLPath is the {abs_url_path} value: abs encoded as for {abs_path}, with
// exactly one leading slash before a drive letter or POSIX root, which is
// the URL path form VS Code's handler expects (/home/u/a.ts, /C:/src/a.ts).
// A path that already starts with a slash (POSIX, or a Windows UNC path
// whose two slashes name the server) is kept as is.
func absURLPath(abs string) string {
	p := encodePath(abs)
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

// fillTemplate substitutes every placeholder of an IDE link template. abs
// is the absolute OS path of the file (either separator), rel its contract
// path; it touches no file system, so links for Windows paths can be
// checked on any OS.
func fillTemplate(template, abs, rel string, line, column, endLine, endColumn int) string {
	return strings.NewReplacer(
		"{abs_path}", encodePath(abs), "{abs_url_path}", absURLPath(abs), "{rel_path}", encodePath(rel),
		"{line}", strconv.Itoa(line), "{column}", strconv.Itoa(column),
		"{end_line}", strconv.Itoa(endLine), "{end_column}", strconv.Itoa(endColumn),
	).Replace(template)
}

// bom is the UTF-8 byte order mark. Contract columns count it on line 1,
// but VS Code, IntelliJ IDEA and Zed drop it from the document text, so
// convert subtracts it, as the source endpoint's highlight does.
const bom = "\uFEFF"

// convert turns a contract byte column into the editor's unit. ok is false
// when the position cannot be checked against the line; the column is then
// the byte column unchanged.
func convert(lines []string, line, column int, unit string) (int, bool) {
	if line < 1 || line > len(lines) || column < 0 || column > len(lines[line-1]) {
		return column, false
	}
	s := lines[line-1]
	at := column
	if line == 1 && strings.HasPrefix(s, bom) && column >= len(bom) {
		s, at = s[len(bom):], column-len(bom)
	}
	if !utf8.ValidString(s) || at < len(s) && !utf8.RuneStart(s[at]) {
		return column, false
	}
	switch unit {
	case "utf16":
		return len(utf16.Encode([]rune(s[:at]))), true
	case "codepoint":
		return utf8.RuneCountInString(s[:at]), true
	}
	return at, true
}

// source reads a repository file once per links value, so every node of a
// file is presented against the same bytes.
func (l *links) source(file string) sourceFile {
	f, ok := l.files[file]
	if !ok {
		read := l.read
		if read == nil {
			read = func(file string) ([]byte, error) {
				return os.ReadFile(filepath.Join(l.root, filepath.FromSlash(file)))
			}
		}
		data, err := read(file)
		if err == nil {
			normalized := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
			f = sourceFile{strings.Split(normalized, "\n"), contract.ContentHash(data), true}
		}
		l.files[file] = f
	}
	return f
}

func (l *links) node(n contract.Node, hash string) Node {
	out := Node{Node: n}
	if n.Span == nil || n.File == "." {
		return out
	}
	f := l.source(n.File)
	span := n.Span
	start, end := span.StartColumn, span.EndColumn
	exact := false
	if f.hash == hash {
		var a, b bool
		start, a = convert(f.lines, span.StartLine, start, l.config.ColumnUnit)
		end, b = convert(f.lines, span.EndLine, end, l.config.ColumnUnit)
		exact = a && b
		if !exact {
			start = span.StartColumn
			end = span.EndColumn
		}
	}
	abs := filepath.Join(l.root, filepath.FromSlash(n.File))
	// The link names the line only when the editor has a line-only form and
	// its column cannot be vouched for (rule 14): the file changed (the byte
	// column is not the editor's) or a tab precedes the node (the editor's
	// column depends on its tab size). The tab is checked only on a line the
	// conversion read.
	if lineTemplate := config.LineTemplate(l.config.LinkTemplate); lineTemplate != "" {
		tab := exact && strings.Contains(f.lines[span.StartLine-1][:span.StartColumn], "\t")
		if !exact || tab {
			url := fillTemplate(lineTemplate, abs, n.File, span.StartLine, l.config.ColumnBase, span.EndLine, l.config.ColumnBase)
			link := &IDELink{Link: url, Line: span.StartLine, Column: l.config.ColumnBase, LineOnly: true}
			if tab {
				link.Caveat = config.CaveatTabSize
			}
			out.IDE = link
			return out
		}
	}
	// A converted column in a unit only assumed for the editor is not exact.
	caveat := ""
	if c := config.ColumnCaveat(l.config.LinkTemplate); exact && c != "" {
		exact, caveat = false, c
	}
	start += l.config.ColumnBase
	end += l.config.ColumnBase
	url := fillTemplate(l.config.LinkTemplate, abs, n.File, span.StartLine, start, span.EndLine, end)
	out.IDE = &IDELink{Link: url, Line: span.StartLine, Column: start, Exact: exact, Caveat: caveat}
	return out
}

// Presenter adds IDE links and file states to stored nodes for one request
// or command. It reads each presented file at most once, so all nodes of a
// file agree on whether that file changed since the last index. The CLI
// query commands and the HTTP API share it, so a link means the same in
// both.
type Presenter struct {
	links links
}

// NewPresenter builds a presenter for the repository at root (absolute),
// using the IDE settings of its configuration.
func NewPresenter(root string, ide config.IDE) *Presenter {
	return &Presenter{links{root: root, config: ide, files: map[string]sourceFile{}}}
}

// NewPresenterWithReader is NewPresenter with the file reads done by read,
// which receives the contract-relative path. The source endpoint passes its
// confined read, so a presented node's link exactness and file state come
// from the same bytes as the source it returns; a read error presents the
// file as missing.
func NewPresenterWithReader(root string, ide config.IDE, read func(file string) ([]byte, error)) *Presenter {
	return &Presenter{links{root: root, config: ide, files: map[string]sourceFile{}, read: read}}
}

// Node presents n. storedHash is the stored content hash of n's file: the
// column conversion is exact only when the current file still has it.
// Synthetic nodes (file ".") and nodes without a span get no link.
func (p *Presenter) Node(n contract.Node, storedHash string) Node {
	return p.links.node(n, storedHash)
}

// FileState compares a repository file as it is now with its stored hash.
type FileState string

// File states. Missing covers every failed read (deleted, renamed or
// unreadable), because none of them lets a client trust stored positions.
const (
	FileUnchanged FileState = "unchanged"
	FileChanged   FileState = "changed"
	FileMissing   FileState = "missing"
)

// FileState reports whether file (repository-relative, not ".") still has
// storedHash. It uses the same single read as Node.
func (p *Presenter) FileState(file, storedHash string) FileState {
	f := p.links.source(file)
	switch {
	case !f.read:
		return FileMissing
	case f.hash == storedHash:
		return FileUnchanged
	default:
		return FileChanged
	}
}
