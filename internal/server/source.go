package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds GET /api/v0/source (ADR 0044, docs/cli/serve.md): the
// current lines around one stored node's span, with the span highlighted
// only when Graphvein can prove the lines are the indexed ones. The request
// names a node, never a path; the only file read is the node's own stored
// file, through a confined read that follows no symlink.

// Source caps (ADR 0044). The exported ones are read by the MCP source
// tool, so its bounds and argument limits never restate them (T030 R10).
const (
	// maxSourceBytes is the largest file the endpoint reads.
	maxSourceBytes = 4 << 20
	// MaxSourceLines bounds the returned window (truncated).
	MaxSourceLines = 400
	// MaxLineBytes bounds one returned line; longer lines are cut
	// (cut_lines).
	MaxLineBytes = 4096
	// DefaultSourceContext and MaxSourceContext bound the lines around
	// the span (the context parameter).
	DefaultSourceContext = 8
	MaxSourceContext     = 50
)

// Source states (source.state). Clients treat an unknown state as "no
// highlight, show the reason".
const (
	sourceExact       = "exact"
	sourceTextMatch   = "text_match"
	sourceChanged     = "changed"
	sourceSpanInvalid = "span_invalid"
	sourceMissing     = "missing"
	sourceNotRegular  = "not_regular"
	sourceTooLarge    = "too_large"
	sourceBinary      = "binary"
	sourceUnreadable  = "unreadable"
	sourceNoLocation  = "no_location"
	sourceNotIndexed  = "not_indexed"
)

// Reasons of the changed state.
const (
	reasonOutOfRange  = "span_out_of_range"
	reasonTextDiffers = "span_text_differs"
	reasonNotUnique   = "span_not_unique"
	reasonEmptySpan   = "empty_span"
)

type sourceResponse struct {
	APIVersion int           `json:"api_version"`
	Repository string        `json:"repository"`
	Index      *lastRun      `json:"index"`
	IDE        ideStatus     `json:"ide"`
	Node       presentedNode `json:"node"`
	Source     sourceView    `json:"source"`
	// CodeCallsPrecision is the code `calls` label (familyCodeCalls) for a
	// function or method, whose calls the views show next to this file
	// label; null for every other node.
	CodeCallsPrecision *scopeLabel `json:"code_calls_precision"`
}

// sourceView is the source of one node as the file is now.
type sourceView struct {
	State     string         `json:"state"`
	Reason    *string        `json:"reason"`
	File      sourceFileInfo `json:"file"`
	FirstLine *int           `json:"first_line"`
	Lines     []string       `json:"lines"`
	CutLines  []int          `json:"cut_lines"`
	Truncated bool           `json:"truncated"`
	Highlight *highlight     `json:"highlight"`
}

type sourceFileInfo struct {
	Path      string `json:"path"`
	Size      *int64 `json:"size"`
	LineCount *int   `json:"line_count"`
	// fileLabel is the stored file record's language and Graphvein's
	// label for it (T020, ADR 0066); null fields without a record.
	fileLabel
}

// highlight is a range of the returned lines: 1-based lines, 0-based UTF-16
// code unit columns within the returned line texts, end exclusive.
type highlight struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

// newSourceHandler builds the source endpoint. warned remembers the nodes
// already logged as producer errors, so each is logged once per server.
func newSourceHandler(opt Options) *apiHandler {
	warned := &sync.Map{}
	return &apiHandler{opt: opt, compute: func(h *apiHandler, q url.Values) (readFunc, error) {
		return computeSource(h, q, warned)
	}}
}

func computeSource(h *apiHandler, q url.Values, warned *sync.Map) (readFunc, error) {
	id, present, err := param(q, "id")
	if err != nil {
		return nil, err
	}
	if !present || id == "" {
		return nil, badRequest("parameter id is required")
	}
	if _, _, _, err := contract.ParseIdentity(id); err != nil {
		return nil, badRequest("id %q is not a canonical node identity", id)
	}
	around, err := contextParam(q)
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		n, ok := found[id]
		if !ok {
			return nil, &apiError{http.StatusNotFound, "not_found", "no node with identity " + id}
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		files := &confinedReader{root: h.opt.Root, hashes: hashes, reads: map[string]fileRead{}}
		p := h.newPresenterReading(hashes, editor, files.bytes)
		out := sourceResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), IDE: p.ide,
			Node: p.node(n), Source: sourceOf(n, hashes, files, around),
		}
		if out.Source.State == sourceSpanInvalid {
			// A producer error: logged once per node and file content per
			// server, so a repeated request does not flood the log.
			if _, logged := warned.LoadOrStore(n.Identity+"\x00"+hashes[n.File], true); !logged {
				h.opt.logger().Warn("stored span is not valid in its unchanged file", "node", n.Identity, "file", n.File)
			}
		}
		if n.File != "." {
			records, unrooted, err := rd.fileRecords(rd.repo, []string{n.File})
			if err != nil {
				return nil, err
			}
			run, _ := precisionRun(runs)
			out.Source.File.fileLabel = fileLabelOf(records[n.File], run, unrooted[n.File])
		}
		if codeCallKinds[n.Kind] {
			run, _ := precisionRun(runs)
			l := codeCallsLabel(run)
			out.CodeCallsPrecision = &l
		}
		return out, nil
	}, nil
}

// contextParam parses context: decimal digits only, 0..MaxSourceContext.
func contextParam(q url.Values) (int, error) {
	v, present, err := param(q, "context")
	if err != nil || !present {
		return DefaultSourceContext, err
	}
	n, convErr := strconv.Atoi(v)
	if v == "" || strings.Trim(v, "0123456789") != "" || convErr != nil || n > MaxSourceContext {
		return 0, badRequest("context must be an integer from 0 to %d, not %q", MaxSourceContext, v)
	}
	return n, nil
}

// fileRead is the outcome of one confined read. state is empty when data
// holds the file's text; binary keeps the bytes read.
type fileRead struct {
	state  string
	reason string
	data   []byte
	size   *int64
}

// confinedReader reads stored files below root at most once each. It is
// the only file access of the source endpoint, and the presenter reads
// through it too, so link exactness, file state and source agree.
type confinedReader struct {
	root   string
	hashes map[string]string
	reads  map[string]fileRead
}

// read returns the confined read of file. A path that is not a valid
// contract path or not a stored file is never opened.
func (c *confinedReader) read(file string) fileRead {
	if r, ok := c.reads[file]; ok {
		return r
	}
	var r fileRead
	if _, stored := c.hashes[file]; !stored || !contract.ValidPath(file, false) {
		r = fileRead{state: sourceNotIndexed}
	} else {
		r = readConfined(c.root, file)
	}
	c.reads[file] = r
	return r
}

// bytes adapts read to the presenter: the current bytes when the file was
// read (text or binary), else an error, which presents the file as missing.
func (c *confinedReader) bytes(file string) ([]byte, error) {
	r := c.read(file)
	if r.state == "" || r.state == sourceBinary {
		return r.data, nil
	}
	return nil, fmt.Errorf("source %s: %s", file, r.state)
}

// readConfined reads file (a valid contract path) below root the way the
// scanner would accept it: os.Root keeps every access inside root, and
// every path component is checked with Lstat so no symlink is followed,
// not even one that stays inside the root (the scanner follows none). The
// root itself may be a symlink; the user chose it.
func readConfined(root, file string) fileRead {
	r, err := os.OpenRoot(root)
	if err != nil {
		return classifyReadError(err)
	}
	defer r.Close()
	parts := strings.Split(file, "/")
	var last fs.FileInfo
	for i := range parts {
		name := filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		info, err := r.Lstat(name)
		if err != nil {
			return classifyReadError(err)
		}
		mode := info.Mode()
		final := i == len(parts)-1
		switch {
		case mode&fs.ModeSymlink != 0:
			return fileRead{state: sourceNotRegular}
		case !final && mode.IsRegular():
			// What opening through a file reports (ENOTDIR): no such file.
			return fileRead{state: sourceMissing}
		case !final && !mode.IsDir(), final && !mode.IsRegular():
			return fileRead{state: sourceNotRegular}
		}
		last = info
	}
	f, err := r.Open(filepath.FromSlash(file))
	if err != nil {
		return classifyReadError(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return classifyReadError(err)
	}
	// A swap between the Lstat walk and the open is not the checked file.
	if !info.Mode().IsRegular() || !os.SameFile(info, last) {
		return fileRead{state: sourceNotRegular}
	}
	size := info.Size()
	if size > maxSourceBytes {
		return fileRead{state: sourceTooLarge, size: &size}
	}
	// The limit catches a file that grew after Stat.
	data, err := io.ReadAll(io.LimitReader(f, maxSourceBytes+1))
	if err != nil {
		return classifyReadError(err)
	}
	size = int64(len(data))
	if size > maxSourceBytes {
		return fileRead{state: sourceTooLarge, size: &size}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return fileRead{state: sourceBinary, data: data, size: &size}
	}
	return fileRead{data: data, size: &size}
}

// classifyReadError maps an open, stat or read error to a state. os.Root
// reports an escape with an unexported error whose text is fixed; the Lstat
// walk makes it unreachable except through a concurrent symlink swap. The
// walk also finds a file used as a directory before the OS would report
// ENOTDIR (the syscall package is outside this package's allow-list, ADR
// 0040), so only a concurrent swap reaches that error, as unreadable.
func classifyReadError(err error) fileRead {
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fileRead{state: sourceMissing}
	case errors.As(err, &pe) && pe.Err.Error() == "path escapes from parent":
		return fileRead{state: sourceNotRegular}
	}
	return fileRead{state: sourceUnreadable, reason: err.Error()}
}

// sourceOf computes the source view of n. The checks run in the order of
// ADR 0044: no location, not indexed, the confined read, binary, then the
// span against the current text.
func sourceOf(n contract.Node, hashes map[string]string, files *confinedReader, around int) sourceView {
	out := sourceView{File: sourceFileInfo{Path: n.File}, Lines: []string{}, CutLines: []int{}}
	if n.File == "." || n.Span == nil {
		out.State = sourceNoLocation
		return out
	}
	r := files.read(n.File)
	out.File.Size = r.size
	if r.state != "" {
		out.State = r.state
		if r.reason != "" {
			out.Reason = &r.reason
		}
		return out
	}
	t := newSourceText(r.data)
	out.File.LineCount = &t.lineCount
	span := *n.Span
	trusted := false
	if contract.ContentHash(r.data) == hashes[n.File] {
		if t.inBounds(span) {
			out.State, trusted = sourceExact, true
		} else {
			out.State = sourceSpanInvalid
		}
	} else {
		reason := t.textMatch(span, n.ContentHash)
		if reason == "" {
			out.State, trusted = sourceTextMatch, true
		} else {
			out.State = sourceChanged
			out.Reason = &reason
		}
	}
	first, last := t.window(span, trusted, around)
	if last-first+1 > MaxSourceLines {
		last = first + MaxSourceLines - 1
		out.Truncated = true
	}
	out.FirstLine = &first
	for line := first; line <= last; line++ {
		text, cut := t.display(line)
		out.Lines = append(out.Lines, text)
		if cut {
			out.CutLines = append(out.CutLines, line)
		}
	}
	if trusted {
		sl, sc := t.position(span.StartLine, span.StartColumn, last)
		el, ec := t.position(span.EndLine, span.EndColumn, last)
		out.Highlight = &highlight{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
	}
	return out
}

// sourceText is the LF-normalised current file split into lines, the form
// contract spans are measured on.
type sourceText struct {
	text string
	// lines are the texts between line feeds; a final line feed leaves an
	// empty last element, which a span may end on (column 0).
	lines     []string
	starts    []int
	lineCount int
	bom       bool
}

// bom is the UTF-8 byte order mark, dropped from the displayed line 1.
const bom = "\uFEFF"

func newSourceText(data []byte) sourceText {
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	t := sourceText{text: text, lines: strings.Split(text, "\n"), bom: strings.HasPrefix(text, bom)}
	t.starts = make([]int, len(t.lines))
	offset := 0
	for i, line := range t.lines {
		t.starts[i] = offset
		offset += len(line) + 1
	}
	t.lineCount = len(t.lines)
	if strings.HasSuffix(text, "\n") {
		t.lineCount--
	}
	return t
}

// valid reports whether line and byte column name a position of the text
// on a UTF-8 sequence boundary.
func (t sourceText) valid(line, column int) bool {
	if line < 1 || line > len(t.lines) || column < 0 || column > len(t.lines[line-1]) {
		return false
	}
	s := t.lines[line-1]
	return column == len(s) || utf8.RuneStart(s[column])
}

func (t sourceText) inBounds(s contract.Span) bool {
	return t.valid(s.StartLine, s.StartColumn) && t.valid(s.EndLine, s.EndColumn)
}

func (t sourceText) offset(line, column int) int { return t.starts[line-1] + column }

// textMatch returns "" when the span's current bytes hash to the node's
// content hash and occur exactly once in the file (overlaps counted), else
// the reason the span cannot be trusted.
func (t sourceText) textMatch(s contract.Span, nodeHash string) string {
	if !t.inBounds(s) {
		return reasonOutOfRange
	}
	start, end := t.offset(s.StartLine, s.StartColumn), t.offset(s.EndLine, s.EndColumn)
	if start == end {
		return reasonEmptySpan
	}
	span := t.text[start:end]
	if contract.ContentHash([]byte(span)) != nodeHash {
		return reasonTextDiffers
	}
	count := 0
	for i := 0; i <= len(t.text)-len(span); {
		j := strings.Index(t.text[i:], span)
		if j < 0 {
			break
		}
		count++
		if count > 1 {
			return reasonNotUnique
		}
		i += j + 1
	}
	return ""
}

// window returns the first and last line to show: context lines around a
// trusted span, else around the stored start line, clamped to the file.
func (t sourceText) window(s contract.Span, trusted bool, around int) (int, int) {
	start, end := s.StartLine, s.EndLine
	if !trusted {
		start = min(max(start, 1), t.lineCount)
		end = start
	}
	return max(1, start-around), max(1, min(t.lineCount, end+around))
}

// display returns a line as sent: without a leading byte order mark on
// line 1, cut at a rune boundary when longer than MaxLineBytes.
func (t sourceText) display(line int) (string, bool) {
	s := t.lines[line-1]
	if line == 1 && t.bom {
		s = s[len(bom):]
	}
	if len(s) <= MaxLineBytes {
		return s, false
	}
	i := MaxLineBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i], true
}

// position converts a valid byte position into a highlight position on the
// returned lines: a position after the last returned line (a truncated
// window, or the end of a file after its final line feed) moves to the end
// of that line; columns drop a byte order mark, are clipped to a cut line
// and count UTF-16 code units.
func (t sourceText) position(line, column, last int) (int, int) {
	if line > last {
		text, _ := t.display(last)
		return last, utf16Len(text)
	}
	if line == 1 && t.bom {
		column = max(0, column-len(bom))
	}
	text, _ := t.display(line)
	return line, utf16Len(text[:min(column, len(text))])
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
