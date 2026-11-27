// Package scan discovers repository files without following checkout boundaries.
package scan

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/precision"
)

// File retains normalized source so parser spans and hashes describe the same bytes.
type File struct {
	Record contract.File
	// Source is the file's text with CRLF and lone CR read as LF (contract
	// v1); Record.ContentHash covers it.
	Source []byte
	// LoneCR reports that the raw bytes hold a CR not followed by LF, which
	// Source and the content hash cannot show. Go reads such a CR as white
	// space, not a line end, so the Go extractor refuses the file (row
	// GS-F3 of docs/lang/go-structure.md).
	LoneCR bool
}

// Result keeps walk failures visible alongside successfully discovered files.
type Result struct {
	Files       []File
	Diagnostics []contract.Diagnostic
	// Skipped lists the directory entries the walk saw but did not read
	// (default and ignored names, files with NUL bytes or invalid UTF-8,
	// files it failed to read, unsupported paths, symlinks, non-regular
	// files, nested repositories), directories with a trailing "/" (a
	// symlink to a directory included). The Python linker reads it to
	// refuse an import a file it cannot see would satisfy first (row PS-I1
	// of docs/lang/python-structure.md); nothing is stored.
	Skipped []string
	// Dirs lists every directory the walk entered (not the root), so a
	// directory holding no scanned file still exists for the Python
	// linker, as it does for Python's import system (a namespace package,
	// docs/lang/python-structure.md); nothing is stored.
	Dirs []string
}
type rule struct {
	base string
	// prefix is base+"/" (empty for the root), precomputed for each check.
	prefix string
	// pattern is the full gitignore translation; it defines the decision and
	// is what the test-only reference matcher evaluates.
	pattern           *regexp.Regexp
	negate, directory bool
	// match decides like pattern.MatchString but cheaper (see compileMatch).
	match func(string) bool
}

// compileMatch returns a matcher that decides exactly like the full regular
// expression. A pattern without '/' is translated with a "(^|/)" prefix and,
// when its body cannot match '/', can only match the whole last path segment,
// so it is tested against that segment alone. Bodies with no metacharacters
// become string comparisons. Anything else keeps the regular expression,
// behind a check for its leading literal when it is anchored.
//
// Byte comparisons agree with the regular expression only for valid UTF-8:
// the expression reads each invalid byte as U+FFFD, so a path that is not
// valid UTF-8 always takes the full expression.
func compileMatch(body string, anchored, crossesSlash bool, literal string, glob []string, full *regexp.Regexp) (func(string) bool, error) {
	// bytewise guards a byte-comparing fast path.
	bytewise := func(fast func(string) bool) func(string) bool {
		return func(p string) bool {
			if !utf8.ValidString(p) {
				return full.MatchString(p)
			}
			return fast(p)
		}
	}
	if !anchored && !crossesSlash {
		last := func(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }
		switch {
		case len(glob) == 1:
			lit := glob[0]
			return bytewise(func(p string) bool { return last(p) == lit }), nil
		case len(glob) == 2 && glob[0] == "":
			lit := glob[1]
			return bytewise(func(p string) bool { return strings.HasSuffix(last(p), lit) }), nil
		case len(glob) == 2 && glob[1] == "":
			lit := glob[0]
			return bytewise(func(p string) bool { return strings.HasPrefix(last(p), lit) }), nil
		}
		segment, err := regexp.Compile("^" + body + "$")
		if err != nil {
			return nil, err
		}
		return func(p string) bool { return segment.MatchString(last(p)) }, nil
	}
	if anchored && len(glob) == 1 {
		lit := glob[0]
		return bytewise(func(p string) bool { return p == lit }), nil
	}
	if anchored && literal != "" {
		return bytewise(func(p string) bool { return strings.HasPrefix(p, literal) && full.MatchString(p) }), nil
	}
	return full.MatchString, nil
}

var defaults = map[string]bool{".git": true, ".graphvein": true, ".kilo": true, ".opencode": true, ".claude": true, ".codex": true, ".agents": true, ".cursor": true, ".worktrees": true, "worktrees": true, "node_modules": true, "target": true, "dist": true, "build": true, "vendor": true, "vendored": true, ".venv": true, "venv": true, "__pycache__": true}

// diagnostic records something the walk did not read as a warning.
func diagnostic(file, code string, err error) contract.Diagnostic {
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: code, Message: err.Error()}
}

// SourceWithNUL reports whether a file holding NUL bytes is still read as
// text: a Rust file (`.rs`) that is valid UTF-8 and holds few NUL bytes
// (at most 8, or one per KiB of the file). The Rust reference reads source
// as UTF-8 and allows a raw NUL inside string, byte-string and character
// literals and comments (rustc 1.97 accepts them; a NUL anywhere else is a
// syntax error, which the Rust structure rules then refuse), so such a file
// is ordinary source, typically a test with binary data in a byte string.
// Other languages keep the binary rule: Go and Python reject NUL in source,
// and the rest are not checked. The count bound keeps a binary file that
// happens to carry the extension out.
func SourceWithNUL(p string, raw []byte) bool {
	if Language(p, nil) != "rust" || !utf8.Valid(raw) {
		return false
	}
	n := bytes.Count(raw, []byte{0})
	return n <= 8 || n <= len(raw)/1024
}

// skipCodes are the codes the walk records for an entry it deliberately
// does not read (a symlink, a FIFO or other non-regular file, a binary or
// non-UTF-8 file). They describe the repository, not a failed walk, so a
// freshness check over a walk that holds only these is complete. This is a
// positive list kept next to the walk that emits the codes, not a severity
// rule: file_not_utf8 is a warning yet a deliberate skip, while a read or
// listing failure (file_read, scan_read, ignore_read, invalid_path) is
// never in it. A new skip code is added here when the walk starts emitting
// it.
var skipCodes = map[string]bool{
	"symlink":          true,
	"file_not_regular": true,
	"file_binary":      true,
	"file_not_utf8":    true,
}

// DeliberateSkip reports whether code is one the walk records for an entry
// it chose not to read (see skipCodes), as opposed to a walk failure that
// leaves the scan incomplete. Freshness checks use it so a repository
// holding an image or a FIFO still reads fresh.
func DeliberateSkip(code string) bool { return skipCodes[code] }

// notice records an entry the walk did not read where skipping it is
// expected (an image, a FIFO): the same record as diagnostic, at info
// severity, so the couldn't-read page can still list the path (row CR-34
// of docs/contract/claims/couldnt-read.md).
func notice(file, code string, err error) contract.Diagnostic {
	d := diagnostic(file, code, err)
	d.Severity = "info"
	return d
}

func readRules(root, base, name string) ([]rule, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(base), name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rules []rule
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" || line[0] == '#' {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		if neg {
			line = line[1:]
		}
		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}
		dir := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		anchored := strings.HasPrefix(line, "/") || strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		// body is the regular expression for the pattern itself. glob holds
		// the literal runs between single '*' wildcards while the pattern has
		// no other metacharacter (nil otherwise); literal is the leading
		// literal run, which every match of an anchored pattern starts with.
		var body strings.Builder
		glob, run := []string{}, strings.Builder{}
		simple, crossesSlash, leading := true, false, true
		literal := ""
		for i := 0; i < len(line); i++ {
			switch line[i] {
			case '*':
				if i+1 < len(line) && line[i+1] == '*' {
					simple, crossesSlash = false, true
					i++
					if i+1 < len(line) && line[i+1] == '/' {
						i++
						body.WriteString("(?:.*/)?")
					} else {
						body.WriteString(".*")
					}
				} else {
					glob = append(glob, run.String())
					run.Reset()
					body.WriteString("[^/]*")
				}
				leading = false
			case '?':
				simple, leading = false, false
				body.WriteString("[^/]")
			case '[':
				end := strings.IndexByte(line[i+1:], ']')
				if end >= 0 {
					// A bracket expression may match '/', so it never takes
					// the segment path.
					simple, crossesSlash, leading = false, true, false
					segment := line[i : i+end+2]
					if strings.HasPrefix(segment, "[!") {
						segment = "[^" + segment[2:]
					}
					body.WriteString(segment)
					i += end + 1
				} else {
					body.WriteString(`\[`)
					run.WriteByte('[')
					if leading {
						literal += "["
					}
				}
			case '\\':
				if i+1 < len(line) {
					i++
					body.WriteString(regexp.QuoteMeta(line[i : i+1]))
					run.WriteByte(line[i])
					if leading {
						literal += line[i : i+1]
					}
				}
			default:
				body.WriteString(regexp.QuoteMeta(line[i : i+1]))
				run.WriteByte(line[i])
				if leading {
					literal += line[i : i+1]
				}
			}
		}
		glob = append(glob, run.String())
		if !simple {
			glob = nil
		}
		prefix := "(^|/)"
		if anchored {
			prefix = "^"
		}
		re, err := regexp.Compile(prefix + body.String() + "$")
		if err != nil {
			return rules, fmt.Errorf("ignore pattern %q: %w", line, err)
		}
		match, err := compileMatch(body.String(), anchored, crossesSlash, literal, glob, re)
		if err != nil {
			return rules, fmt.Errorf("ignore pattern %q: %w", line, err)
		}
		r := rule{base: base, pattern: re, negate: neg, directory: dir, match: match}
		if base != "" {
			r.prefix = base + "/"
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// ignored applies gitignore precedence: the last matching rule decides, so
// the rules are searched from the end and the first match returns.
func ignored(p string, directory bool, rules []rule) bool {
	for i := len(rules) - 1; i >= 0; i-- {
		r := &rules[i]
		if r.directory && !directory {
			continue
		}
		rel := p
		if r.prefix != "" {
			if !strings.HasPrefix(p, r.prefix) {
				continue
			}
			rel = p[len(r.prefix):]
		}
		if r.match(rel) {
			return !r.negate
		}
	}
	return false
}

// Language records recognized text formats independently of extractor availability.
func Language(p string, source []byte) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".py", ".pyi":
		return "python"
	case ".java":
		return "java"
	case ".cs":
		return "c_sharp"
	case ".dart":
		return "dart"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".sql":
		return "sql"
	case ".sh":
		return "shell"
	}
	first, _, _ := bytes.Cut(source, []byte("\n"))
	if bytes.HasPrefix(first, []byte("#!")) && (bytes.Contains(first, []byte("node")) || bytes.Contains(first, []byte("deno"))) {
		return "javascript"
	}
	if bytes.HasPrefix(first, []byte("#!")) {
		if bytes.Contains(first, []byte("python")) {
			return "python"
		}
		for _, word := range strings.Fields(string(first[2:])) {
			if path.Base(word) == "sh" || path.Base(word) == "bash" {
				return "shell"
			}
		}
	}
	return "unknown"
}

// Walk isolates unreadable files and applies nested Git ignores before project overrides.
func Walk(ctx context.Context, root string) (Result, error) {
	var out Result
	absolute, err := filepath.Abs(root)
	if err != nil {
		return out, err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return out, fmt.Errorf("repository directory %s is unavailable", root)
	}
	gitFormat := strings.TrimSpace(string(func() []byte {
		b, _ := GitCommand(ctx, absolute, "rev-parse", "--show-object-format").Output()
		return b
	}()))
	overrides, err := readRules(absolute, "", ".graphveinignore")
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diagnostic(".graphveinignore", "ignore_read", err))
	}
	var walk func(string, []rule) error
	walk = func(base string, inherited []rule) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		local, e := readRules(absolute, base, ".gitignore")
		if e != nil {
			out.Diagnostics = append(out.Diagnostics, diagnostic(path.Join(base, ".gitignore"), "ignore_read", e))
		}
		rules := append(append([]rule{}, inherited...), local...)
		// Project overrides follow every Git rule so they take precedence.
		effective := append(append([]rule{}, rules...), overrides...)
		entries, e := os.ReadDir(filepath.Join(absolute, filepath.FromSlash(base)))
		if e != nil {
			out.Diagnostics = append(out.Diagnostics, diagnostic(base, "scan_read", e))
			return nil
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := path.Join(base, entry.Name())
			skip := func() {
				dir := entry.IsDir()
				if entry.Type()&fs.ModeSymlink != 0 {
					// A symlink to a directory is a directory to Python's
					// import system, which follows it.
					if info, e := os.Stat(filepath.Join(absolute, filepath.FromSlash(p))); e == nil {
						dir = info.IsDir()
					}
				}
				if dir {
					out.Skipped = append(out.Skipped, p+"/")
				} else {
					out.Skipped = append(out.Skipped, p)
				}
			}
			if defaults[entry.Name()] || ignored(p, entry.IsDir(), effective) {
				skip()
				continue
			}
			if !contract.ValidPath(p, false) {
				skip()
				out.Diagnostics = append(out.Diagnostics, diagnostic("", "invalid_path", fmt.Errorf("unsupported repository path %q", p)))
				continue
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				skip()
				out.Diagnostics = append(out.Diagnostics, diagnostic(p, "symlink", fmt.Errorf("symlink is not followed")))
				continue
			}
			full := filepath.Join(absolute, filepath.FromSlash(p))
			if entry.IsDir() {
				if _, e := os.Lstat(filepath.Join(full, ".git")); e == nil {
					skip()
					continue
				}
				out.Dirs = append(out.Dirs, p)
				if e := walk(p, rules); e != nil {
					return e
				}
				continue
			}
			if !entry.Type().IsRegular() {
				skip()
				out.Diagnostics = append(out.Diagnostics, notice(p, "file_not_regular", fmt.Errorf("not a regular file (%s); not read", entry.Type())))
				continue
			}
			raw, e := os.ReadFile(full)
			if e != nil {
				skip()
				out.Diagnostics = append(out.Diagnostics, diagnostic(p, "file_read", e))
				continue
			}
			// Ignored and default names never reach this point, so only a
			// file the rules keep gets a record (row CR-35).
			if bytes.Contains(raw, []byte{0}) && !SourceWithNUL(p, raw) {
				skip()
				out.Diagnostics = append(out.Diagnostics, notice(p, "file_binary", fmt.Errorf("file holds a NUL byte (binary); content not read")))
				continue
			}
			if !utf8.Valid(raw) {
				skip()
				out.Diagnostics = append(out.Diagnostics, diagnostic(p, "file_not_utf8", fmt.Errorf("file is not valid UTF-8; content not read")))
				continue
			}
			source := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
			language := Language(p, source)
			f := contract.File{Type: "file", Path: p, Language: language, Precision: precision.FileLabel(language), Size: int64(len(raw)), ContentHash: contract.ContentHash(source)}
			blob := append([]byte(fmt.Sprintf("blob %d%c", len(raw), 0)), raw...)
			if gitFormat == "sha1" {
				f.GitBlobOID = fmt.Sprintf("%x", sha1.Sum(blob))
			} else if gitFormat == "sha256" {
				f.GitBlobOID = fmt.Sprintf("%x", sha256.Sum256(blob))
			}
			out.Files = append(out.Files, File{Record: f, Source: source, LoneCR: bytes.Count(raw, []byte("\r")) != bytes.Count(raw, []byte("\r\n"))})
		}
		return nil
	}
	err = walk("", nil)
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Record.Path < out.Files[j].Record.Path })
	sort.Strings(out.Skipped)
	sort.Strings(out.Dirs)
	return out, err
}
