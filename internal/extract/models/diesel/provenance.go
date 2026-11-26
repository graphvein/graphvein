package diesel

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file holds row DI-G1: which lines of a schema file diesel
// print-schema wrote (generated) and which a developer wrote
// (hand-written). diesel_cli 2.3.14 writes Header as the first line
// (print_schema.rs:12, :203) and then applies the configured patch_file
// with the diffy crate (print_schema.rs:312-331; Cargo.toml requires
// diffy 0.4.0, so 0.4.x, read at 0.4.2). The patch reader below follows
// diffy's parser (src/patch/parse.rs) and its hunk search (src/apply.rs:
// 136-197, GNU patch's search without fuzz), run backwards: each hunk's
// new side (context and added lines) is located in the committed file.

// Header is the first line print-schema writes.
const Header = "// @generated automatically by Diesel CLI."

// noNewline is diffy's marker for a last line without a line break.
const noNewline = `\ No newline at end of file`

// PatchLine is one line of a hunk: Kind is ' ' (context), '-' (removed)
// or '+' (added); Text keeps its line break, as diffy compares lines.
type PatchLine struct {
	Kind byte
	Text string
}

// Hunk is one hunk of a unified diff.
type Hunk struct {
	OldStart, OldLen, NewStart, NewLen int
	// Line is the hunk header's line in the patch file.
	Line  int
	Lines []PatchLine
}

// Patch is a single-file unified diff as diffy 0.4 reads it.
type Patch struct {
	Hunks []Hunk
}

// splitLines splits text after every line break, keeping the breaks;
// the last line has none when the text does not end with one (diffy's
// LineIter).
func splitLines(s string) []string {
	var out []string
	for len(s) > 0 {
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

// normalizeBreaks folds CRLF and lone CR to LF, as the scanner does for
// every retained source, so a patch and a schema file checked out with
// Windows line endings read like the committed text.
func normalizeBreaks(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// ParsePatch reads a unified diff the way diffy 0.4's Patch::from_str
// does: preamble lines are skipped up to the first "--- ", "+++ " or
// "@@ " line, at most one "---" and one "+++" filename line follow, and
// every remaining line belongs to a hunk. Any line diffy refuses (a
// second file's header, a hunk whose counts disagree with its header,
// hunks out of order) is an error: diesel_cli refuses such a patch.
func ParsePatch(src string) (*Patch, error) {
	lines := splitLines(normalizeBreaks(src))
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "--- ") && !strings.HasPrefix(lines[i], "+++ ") && !strings.HasPrefix(lines[i], "@@ ") {
		i++
	}
	seenOld, seenNew := false, false
	for i < len(lines) {
		var seen *bool
		var prefix string
		switch {
		case strings.HasPrefix(lines[i], "--- "):
			seen, prefix = &seenOld, "--- "
		case strings.HasPrefix(lines[i], "+++ "):
			seen, prefix = &seenNew, "+++ "
		}
		if seen == nil {
			break
		}
		if *seen {
			return nil, fmt.Errorf("line %d: multiple '%s' lines", i+1, strings.TrimSpace(prefix))
		}
		*seen = true
		if err := checkFilename(lines[i][len(prefix):]); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		i++
	}
	p := &Patch{}
	for i < len(lines) {
		h, next, err := readHunk(lines, i)
		if err != nil {
			return nil, err
		}
		p.Hunks = append(p.Hunks, h)
		i = next
	}
	for k := 1; k < len(p.Hunks); k++ {
		a, b := p.Hunks[k-1], p.Hunks[k]
		if a.OldStart+a.OldLen > b.OldStart || a.NewStart+a.NewLen > b.NewStart {
			return nil, fmt.Errorf("line %d: hunks not in order or overlap", b.Line)
		}
	}
	return p, nil
}

// checkFilename applies diffy's filename rules (parse_filename): the name
// ends at a tab or the line break, and is either quoted with the escapes
// \n \t \0 \r \" \\ or free of those characters.
func checkFilename(rest string) error {
	name, _, tab := strings.Cut(rest, "\t")
	if !tab {
		var ok bool
		name, ok = strings.CutSuffix(rest, "\n")
		if !ok {
			return fmt.Errorf("filename unterminated")
		}
	}
	const special = "\n\t\x00\r\"\\"
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		q := name[1 : len(name)-1]
		for j := 0; j < len(q); j++ {
			switch {
			case q[j] == '\\':
				j++
				if j >= len(q) || !strings.ContainsRune(`ntr0"\`, rune(q[j])) {
					return fmt.Errorf("invalid escaped character in filename")
				}
			case strings.IndexByte(special, q[j]) >= 0:
				return fmt.Errorf("invalid unescaped character in filename")
			}
		}
		return nil
	}
	if strings.ContainsAny(name, special) {
		return fmt.Errorf("invalid char in unquoted filename")
	}
	return nil
}

// readHunk reads the hunk whose header is lines[i] and returns the index
// of the line after it.
func readHunk(lines []string, i int) (Hunk, int, error) {
	h := Hunk{Line: i + 1}
	header, ok := strings.CutPrefix(lines[i], "@@ ")
	if !ok {
		return h, 0, fmt.Errorf("line %d: unable to parse hunk header", i+1)
	}
	ranges, _, ok := strings.Cut(header, " @@")
	if !ok {
		return h, 0, fmt.Errorf("line %d: hunk header unterminated", i+1)
	}
	oldRange, newRange, ok := strings.Cut(ranges, " ")
	if !ok || !strings.HasPrefix(oldRange, "-") || !strings.HasPrefix(newRange, "+") {
		return h, 0, fmt.Errorf("line %d: unable to parse hunk header", i+1)
	}
	var err error
	if h.OldStart, h.OldLen, err = hunkRange(oldRange[1:]); err != nil {
		return h, 0, fmt.Errorf("line %d: %w", i+1, err)
	}
	if h.NewStart, h.NewLen, err = hunkRange(newRange[1:]); err != nil {
		return h, 0, fmt.Errorf("line %d: %w", i+1, err)
	}
	i++
	var endContext, endRemoved, endAdded bool
	for ; i < len(lines); i++ {
		line := lines[i]
		var pl PatchLine
		switch {
		case strings.HasPrefix(line, "@"):
			return finishHunk(h, i)
		case endContext:
			return h, 0, fmt.Errorf("line %d: expected end of hunk", i+1)
		case line[0] == ' ':
			pl = PatchLine{' ', line[1:]}
		case line == "\n":
			pl = PatchLine{' ', line}
		case line[0] == '-':
			if endRemoved {
				return h, 0, fmt.Errorf("line %d: expected no more deleted lines", i+1)
			}
			pl = PatchLine{'-', line[1:]}
		case line[0] == '+':
			if endAdded {
				return h, 0, fmt.Errorf("line %d: expected no more inserted lines", i+1)
			}
			pl = PatchLine{'+', line[1:]}
		case strings.HasPrefix(line, noNewline):
			if len(h.Lines) == 0 {
				return h, 0, fmt.Errorf("line %d: unexpected 'No newline at end of file' line", i+1)
			}
			pl = h.Lines[len(h.Lines)-1]
			h.Lines = h.Lines[:len(h.Lines)-1]
			text, ok := strings.CutSuffix(pl.Text, "\n")
			if !ok {
				return h, 0, fmt.Errorf("line %d: the line before 'No newline at end of file' has no line break", i+1)
			}
			pl.Text = text
			switch pl.Kind {
			case ' ':
				endContext = true
			case '-':
				endRemoved = true
			default:
				endAdded = true
			}
		default:
			return h, 0, fmt.Errorf("line %d: unexpected line in hunk body", i+1)
		}
		h.Lines = append(h.Lines, pl)
	}
	return finishHunk(h, i)
}

func finishHunk(h Hunk, next int) (Hunk, int, error) {
	oldLen, newLen := 0, 0
	for _, l := range h.Lines {
		if l.Kind != '+' {
			oldLen++
		}
		if l.Kind != '-' {
			newLen++
		}
	}
	if oldLen != h.OldLen || newLen != h.NewLen {
		return h, 0, fmt.Errorf("line %d: hunk header does not match hunk", h.Line)
	}
	return h, next, nil
}

// hunkRange reads "start[,len]" (len 1 when absent) with Rust's usize
// parsing (an optional '+', then decimal digits).
func hunkRange(s string) (int, int, error) {
	startText, lenText, hasLen := strings.Cut(s, ",")
	start, ok := parseUsize(startText)
	n := 1
	if hasLen {
		var okLen bool
		n, okLen = parseUsize(lenText)
		ok = ok && okLen
	}
	if !ok {
		return 0, 0, fmt.Errorf("can't parse range %q", s)
	}
	return start, n, nil
}

func parseUsize(s string) (int, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func (h Hunk) side(keep byte) []string {
	var out []string
	for _, l := range h.Lines {
		if l.Kind == ' ' || l.Kind == keep {
			out = append(out, l.Text)
		}
	}
	return out
}

// newSide is the hunk's context and added lines; oldSide its context and
// removed lines.
func (h Hunk) newSide() []string { return h.side('+') }
func (h Hunk) oldSide() []string { return h.side('-') }

// searchOrder is diffy's find_position order: the stated position
// (clamped to the image), then one line back, one forward, and so on.
func searchOrder(pos, n int, try func(int) bool) {
	if try(pos) {
		return
	}
	back, fwd := pos-1, pos+1
	for flag := true; back >= 0 || fwd < n; flag = !flag {
		if flag && back >= 0 || fwd >= n {
			if try(back) {
				return
			}
			back--
		} else {
			if try(fwd) {
				return
			}
			fwd++
		}
	}
}

// statedPos is the 0-based position diffy starts searching at.
func statedPos(newStart, n int) int {
	return min(max(newStart-1, 0), n)
}

// Located is where one hunk's new side sits in the committed file.
type Located struct {
	// Hunk is the hunk's index in the patch (0-based); Stated the new
	// start its header states and Line the 1-based line it starts at.
	Hunk, Stated, Line int
}

// Offset is Line minus Stated, as patch(1) reports it.
func (l Located) Offset() int { return l.Line - l.Stated }

// LocateHunks finds every hunk's new side in the file's text: the only
// position whose lines equal the new side exactly (no fuzz), among lines
// no earlier hunk took. A hunk whose new side is empty, matches nowhere
// or matches at more than one position is not located (an error): which
// lines it added is then not proven. The result is checked by applying
// the patch forward, as diffy does, to the text the located hunks imply
// before patching: it must give the file back with every hunk where it
// was located.
func LocateHunks(text string, p *Patch) ([]Located, error) {
	file := splitLines(normalizeBreaks(text))
	taken := make([]bool, len(file))
	located := make([]Located, len(p.Hunks))
	for k, h := range p.Hunks {
		side := h.newSide()
		if len(side) == 0 {
			return nil, fmt.Errorf("hunk %d (patch line %d) has no context or added lines, so its place cannot be found", k+1, h.Line)
		}
		var found []int
		searchOrder(statedPos(h.NewStart, len(file)), len(file), func(pos int) bool {
			if matchAt(file, taken, side, pos) {
				found = append(found, pos)
			}
			return false
		})
		switch len(found) {
		case 0:
			return nil, fmt.Errorf("hunk %d (patch line %d, +%d) does not match the file", k+1, h.Line, h.NewStart)
		case 1:
		default:
			sort.Ints(found)
			return nil, fmt.Errorf("hunk %d (patch line %d, +%d) matches the file at lines %d and %d", k+1, h.Line, h.NewStart, found[0]+1, found[1]+1)
		}
		for j := range side {
			taken[found[0]+j] = true
		}
		located[k] = Located{Hunk: k, Stated: h.NewStart, Line: found[0] + 1}
	}
	if !replays(file, p, located) {
		return nil, fmt.Errorf("applying the patch to the text before it does not place its hunks where they were found")
	}
	return located, nil
}

func matchAt(file []string, taken []bool, side []string, pos int) bool {
	if pos < 0 || pos+len(side) > len(file) {
		return false
	}
	for j, s := range side {
		if taken[pos+j] || file[pos+j] != s {
			return false
		}
	}
	return true
}

// replays rebuilds the text before patching from the located hunks and
// applies the patch to it with diffy's algorithm (apply.rs:120-197); it
// reports whether that gives the file back with the patched lines
// exactly where the hunks were located.
func replays(file []string, p *Patch, located []Located) bool {
	order := make([]int, len(located))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return located[order[a]].Line < located[order[b]].Line })
	type imageLine struct {
		text    string
		patched bool
	}
	var image []imageLine
	at := 0
	for _, k := range order {
		start := located[k].Line - 1
		for ; at < start; at++ {
			image = append(image, imageLine{text: file[at]})
		}
		for _, s := range p.Hunks[k].oldSide() {
			image = append(image, imageLine{text: s})
		}
		at = start + len(p.Hunks[k].newSide())
	}
	for ; at < len(file); at++ {
		image = append(image, imageLine{text: file[at]})
	}
	for _, h := range p.Hunks {
		old := h.oldSide()
		pos := -1
		searchOrder(statedPos(h.NewStart, len(image)), len(image), func(i int) bool {
			if i < 0 || i+len(old) > len(image) {
				return false
			}
			for j, s := range old {
				if image[i+j].patched || image[i+j].text != s {
					return false
				}
			}
			pos = i
			return true
		})
		if pos < 0 {
			return false
		}
		var post []imageLine
		for _, s := range h.newSide() {
			post = append(post, imageLine{text: s, patched: true})
		}
		image = append(image[:pos], append(post, image[pos+len(old):]...)...)
	}
	if len(image) != len(file) {
		return false
	}
	want := make([]bool, len(file))
	for k, l := range located {
		for j := range p.Hunks[k].newSide() {
			want[l.Line-1+j] = true
		}
	}
	for i := range image {
		if image[i].text != file[i] || image[i].patched != want[i] {
			return false
		}
	}
	return true
}

// Removal is a run of lines a hunk removes from print-schema's output.
type Removal struct {
	// After is the committed file's line the run was removed after (0:
	// before the first line).
	After int
	// Lines are the removed lines.
	Lines []string
	// Benign is true when every removed line is blank, a // comment (doc
	// comments included), or a column definition that the same hunk adds
	// back under the same column name: such a run removes no column.
	Benign bool
	// Hunk is the hunk's index in the patch.
	Hunk int
}

// columnLine matches a column definition line "name -> Type" and
// captures the Rust name (r# stripped).
var columnLine = regexp.MustCompile(`^\s*(?:r#)?([A-Za-z_][A-Za-z0-9_]*)\s*->`)

// removals lists the removed runs of a located hunk.
func removals(h Hunk, k int, l Located) []Removal {
	added := map[string]bool{}
	for _, pl := range h.Lines {
		if pl.Kind == '+' {
			if m := columnLine.FindStringSubmatch(pl.Text); m != nil {
				added[m[1]] = true
			}
		}
	}
	var out []Removal
	line := l.Line - 1
	var cur *Removal
	for _, pl := range h.Lines {
		if pl.Kind != '-' {
			line++
			cur = nil
			continue
		}
		if cur == nil {
			out = append(out, Removal{After: line, Benign: true, Hunk: k})
			cur = &out[len(out)-1]
		}
		cur.Lines = append(cur.Lines, pl.Text)
		t := strings.TrimSpace(pl.Text)
		m := columnLine.FindStringSubmatch(pl.Text)
		if !(t == "" || strings.HasPrefix(t, "//") || m != nil && added[m[1]]) {
			cur.Benign = false
		}
	}
	return out
}

// Provenance says which lines of a schema file print-schema wrote (row
// DI-G1). Database facts come only from generated lines; a hand-written
// line claims names only.
type Provenance struct {
	// Generated is true when the file starts with Header, its patch (if
	// any) was read and every hunk located; otherwise every line is
	// hand-written and Reason says why.
	Generated bool
	Reason    string
	// Added are the 1-based lines the patch added (hand-written lines of
	// a generated file).
	Added map[int]bool
	// Removals are the runs the patch removed from print-schema's output.
	Removals []Removal
	// Hunks are the located hunks, in patch order.
	Hunks []Located
}

// HandWritten reports whether a line was written by hand.
func (p *Provenance) HandWritten(line int) bool {
	return !p.Generated || p.Added[line]
}

// HandWrittenSpan reports whether any of the lines first..last was
// written by hand. A table, a column or a sql_types struct is hand-written
// when any line from its first attribute line to its name line is
// (Revision 2, A3): a patch that adds `#[max_length = 10]` above a
// generated column, or rewrites a struct's postgres_type name, changes
// what the item states. The macro line of a table does not count (a hunk
// that adds a table ends with the next table's "diesel::table! {" line,
// as crates.io's does).
func (p *Provenance) HandWrittenSpan(first, last int) bool {
	if !p.Generated {
		return true
	}
	for l := first; l <= last; l++ {
		if p.Added[l] {
			return true
		}
	}
	return false
}

// RemovedWithin reports a removal that is not benign at a point inside
// the lines first..last (after line first and before line last), such as
// a table! item's span: that table's column set is then not complete.
func (p *Provenance) RemovedWithin(first, last int) bool {
	for _, r := range p.Removals {
		if !r.Benign && r.After >= first && r.After < last {
			return true
		}
	}
	return false
}

// RemovesTable reports a removed line that holds "table!": the patch may
// remove a whole table, so the group's table list is not complete.
func (p *Provenance) RemovesTable() bool {
	for _, r := range p.Removals {
		for _, l := range r.Lines {
			if strings.Contains(l, "table!") {
				return true
			}
		}
	}
	return false
}

// handWritten returns the provenance of a file read as hand-written
// throughout.
func handWritten(reason string) *Provenance {
	return &Provenance{Reason: reason, Added: map[int]bool{}}
}

// ReadProvenance decides the provenance of a discovered schema file
// (row DI-G1). An unconfigured file (O2) and a configured file without
// Header on its first line are hand-written. A configured patch_file
// that names no file, is not UTF-8 text or not a unified diff diffy
// reads, or has a hunk that cannot be located, makes the whole file
// hand-written, with an info diagnostic naming the reason.
func ReadProvenance(in Input, f *File, source []byte) (*Provenance, []contract.Diagnostic) {
	in.tag("DI-G1")
	if !f.Configured {
		return handWritten("the file is not configured by diesel.toml"), nil
	}
	text := normalizeBreaks(string(source))
	first, _, _ := strings.Cut(text, "\n")
	if first != Header {
		return handWritten("the file does not start with print-schema's header (" + Header + ")"), nil
	}
	var patchFile, patchPath string
	if f.Section != nil {
		patchFile, patchPath = f.Section.PatchFile, f.Section.PatchPath
	}
	if patchFile == "" {
		return &Provenance{Generated: true, Added: map[int]bool{}}, nil
	}
	fail := func(why string) (*Provenance, []contract.Diagnostic) {
		reason := "the patch_file " + why
		cfg := f.Path
		if f.Config != nil {
			cfg = f.Config.Path
		}
		return handWritten(reason), []contract.Diagnostic{{Type: "diagnostic", File: f.Path, Severity: "info", Code: codeNotProven,
			Message: fmt.Sprintf("%s: %s (%s); every line is read as hand-written, names only (DI-G1)", f.Path, reason, cfg)}}
	}
	switch {
	case patchPath == "":
		return fail(fmt.Sprintf("%q cannot be placed in the repository", patchFile))
	case in.Sources[patchPath] == nil:
		return fail(patchPath + " does not exist")
	case !utf8.Valid(in.Sources[patchPath]):
		return fail(patchPath + " is not UTF-8 text")
	}
	patch, err := ParsePatch(string(in.Sources[patchPath]))
	if err != nil {
		return fail(patchPath + " is not a unified diff diesel_cli reads (" + err.Error() + ")")
	}
	located, err := LocateHunks(text, patch)
	if err != nil {
		return fail(patchPath + ": " + err.Error())
	}
	p := &Provenance{Generated: true, Added: map[int]bool{}, Hunks: located}
	for k, h := range patch.Hunks {
		line := located[k].Line
		for _, pl := range h.Lines {
			switch pl.Kind {
			case '+':
				p.Added[line] = true
				line++
			case ' ':
				line++
			}
		}
		p.Removals = append(p.Removals, removals(h, k, located[k])...)
	}
	if p.Added[1] {
		return fail(patchPath + " adds the header line itself")
	}
	return p, nil
}
