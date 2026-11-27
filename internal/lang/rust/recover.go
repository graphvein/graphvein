package rust

import (
	"bytes"
	"context"

	"github.com/graphvein/graphvein/internal/parse"
)

// This file holds row RS-F4 of the Rust structure contract
// (docs/lang/rust-structure.md): a file whose only syntax errors are outer
// attributes on the fields of a struct pattern. Rust allows them (the
// reference's StructPatternField takes OuterAttribute*, so
// `let S { a, #[cfg(unix)] b } = s;` is valid), but the pinned
// tree-sitter-rust grammar (0.24.2, also the latest upstream release) has no
// rule for them and reports an error, which row RS-F2 turns into a file that
// claims nothing.
//
// The recovery never guesses. It replaces each candidate attribute with
// spaces (newlines kept, so every byte offset, line and column stays the
// same) and parses the file again. The file is read only when that second
// tree has no error at all and every replaced attribute sits directly
// before a field of a struct pattern, after its `{` or `,`, and parses as an
// attribute on its own. The original file is then that clean tree with
// attributes added in positions Rust allows them, so the rows read it as
// they read any valid file; attributes are not modelled (cfg is never
// evaluated), so the replaced bytes claim nothing. Any other outcome leaves
// the file to RS-F2.

// byteRange is a half-open byte range of a source.
type byteRange struct{ start, end int }

// Recovery is a file row RS-F4 may read: its source as scanned and the
// attributes to try replacing.
type Recovery struct {
	source     []byte
	candidates []byteRange
}

// PatternAttributes returns the RS-F4 candidate of a file tree-sitter
// reported a syntax error in, or nil when nothing could explain the error:
// the `#[...]` attributes inside the nodes that hold its ERROR nodes. A
// file whose error leaves no ERROR node (only MISSING tokens) has none.
func PatternAttributes(root *parse.Node, source []byte) *Recovery {
	var regions []byteRange
	var walk func(n *parse.Node)
	walk = func(n *parse.Node) {
		for _, c := range n.Children {
			if c.Kind == "ERROR" {
				regions = append(regions, byteRange{int(n.Start), int(n.End)})
			}
			walk(c)
		}
	}
	if root == nil {
		return nil
	}
	walk(root)
	seen := map[byteRange]bool{}
	var candidates []byteRange
	for _, r := range regions {
		for _, a := range attributesIn(source, r) {
			if !seen[a] {
				seen[a] = true
				candidates = append(candidates, a)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sortRanges(candidates)
	// Regions may nest: keep the outermost of overlapping ranges.
	var out []byteRange
	for _, c := range candidates {
		if len(out) > 0 && c.start < out[len(out)-1].end {
			continue
		}
		out = append(out, c)
	}
	return &Recovery{source: source, candidates: out}
}

// attributesIn lists the `#[...]` runs of source inside r: a `#` directly
// followed by `[`, up to its matching `]`, skipping `"..."` strings. A run
// that does not close inside r is no candidate. The scan is lexical, so a
// run may lie inside a string or comment of the real file; the checks of
// Recover then reject it.
func attributesIn(source []byte, r byteRange) []byteRange {
	var out []byteRange
	for i := r.start; i+1 < r.end; i++ {
		if source[i] != '#' || source[i+1] != '[' {
			continue
		}
		depth, j, end := 0, i+1, -1
		for ; j < r.end && end < 0; j++ {
			switch source[j] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					end = j + 1
				}
			case '"':
				for j++; j < r.end && source[j] != '"'; j++ {
					if source[j] == '\\' {
						j++
					}
				}
			}
		}
		if end < 0 {
			continue
		}
		out = append(out, byteRange{i, end})
		i = end - 1
	}
	return out
}

// sortRanges sorts ranges by start, then by end, longest first.
func sortRanges(rs []byteRange) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && (rs[j].start < rs[j-1].start || rs[j].start == rs[j-1].start && rs[j].end > rs[j-1].end); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// blank returns a copy of source with every byte of the ranges except LF
// replaced by a space.
func blank(source []byte, ranges []byteRange) []byte {
	out := bytes.Clone(source)
	for _, r := range ranges {
		for i := r.start; i < r.end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return out
}

// probeSource places one attribute on the field of a struct item, where the
// grammar reads outer attributes.
func probeSource(attr []byte) []byte {
	return append(append([]byte("struct P {\n"), attr...), " f: u8,\n}\n"...)
}

// probeAccepts reports whether the probe of attr parsed as exactly that
// attribute on the field.
func probeAccepts(r parse.Result, attr []byte) bool {
	if r.Err != nil || r.SyntaxError || r.Root == nil || len(r.Root.Children) != 1 {
		return false
	}
	item := r.Root.Children[0]
	fields := item.Fields["body"]
	if item.Kind != "struct_item" || fields == nil || len(fields.Children) != 2 {
		return false
	}
	a := fields.Children[0]
	return a.Kind == "attribute_item" && a.Text == string(attr) && fields.Children[1].Kind == "field_declaration"
}

// inFieldPosition reports whether the replaced range r of the blanked text
// lies, in the clean tree root, directly inside a struct pattern, after its
// `{` or a `,` and before a field (only white space, which includes other
// replaced attributes, in between). rustc 1.97 rejects an attribute before
// the `..` rest pattern, so that position does not count.
func inFieldPosition(root *parse.Node, blanked []byte, r byteRange) bool {
	n := root
	for {
		var inner *parse.Node
		for _, c := range n.Children {
			if int(c.Start) <= r.start && r.end <= int(c.End) {
				inner = c
				break
			}
		}
		if inner == nil {
			break
		}
		n = inner
	}
	if n.Kind != "struct_pattern" {
		return false
	}
	var prev, next *parse.Node
	for _, c := range n.Children {
		if int(c.End) <= r.start {
			prev = c
		}
		if next == nil && int(c.Start) >= r.end {
			next = c
		}
	}
	if prev == nil || next == nil || next.Kind != "field_pattern" {
		return false
	}
	if len(bytes.TrimSpace(blanked[r.end:next.Start])) != 0 {
		return false
	}
	between := string(bytes.TrimSpace(blanked[prev.End:r.start]))
	return between == "{" || between == ","
}

// restoreText gives every node of a tree parsed from a blanked copy the text
// of the original source, which has the same byte offsets.
func restoreText(n *parse.Node, source []byte) {
	if int(n.End) <= len(source) && n.Start <= n.End {
		n.Text = string(source[n.Start:n.End])
	}
	for _, c := range n.Children {
		restoreText(c, source)
	}
}

// Recover applies row RS-F4 to the candidates, parsing through run (the
// indexer's bounded pipeline). Each attribute is first parsed alone (a
// probe); the attributes that pass are replaced and the file is parsed
// again. When that tree is clean but some replaced attribute is not in a
// field position (an attribute of a statement or item near the error), the
// file is parsed once more with only the others replaced. accept is called,
// inside the consumer and before the tree is released, for every file the
// checks pass, with a tree whose text is the original source; a file they
// do not pass is not reported and stays with row RS-F2. Errors are the
// pipeline's (a cancelled context).
func Recover(ctx context.Context, files []*Recovery, run func(context.Context, []parse.Job, func(int, parse.Result)) error, accept func(i int, root *parse.Node)) error {
	type probe struct{ file, candidate int }
	var probes []probe
	var jobs []parse.Job
	for i, f := range files {
		for j, c := range f.candidates {
			probes = append(probes, probe{i, j})
			jobs = append(jobs, parse.Job{Language: "rust", Source: probeSource(f.source[c.start:c.end])})
		}
	}
	valid := make([][]bool, len(files))
	for i, f := range files {
		valid[i] = make([]bool, len(f.candidates))
	}
	if len(jobs) == 0 {
		return nil
	}
	if err := run(ctx, jobs, func(k int, r parse.Result) {
		p := probes[k]
		c := files[p.file].candidates[p.candidate]
		valid[p.file][p.candidate] = probeAccepts(r, files[p.file].source[c.start:c.end])
	}); err != nil {
		return err
	}
	ranges := make([][]byteRange, len(files))
	for i, f := range files {
		for j, c := range f.candidates {
			if valid[i][j] {
				ranges[i] = append(ranges[i], c)
			}
		}
	}
	// Up to two rounds: all probed attributes, then the field-position subset.
	for round := 0; round < 2; round++ {
		var order []int
		jobs = jobs[:0]
		for i, f := range files {
			if len(ranges[i]) > 0 {
				order = append(order, i)
				jobs = append(jobs, parse.Job{Language: "rust", Source: blank(f.source, ranges[i])})
			}
		}
		if len(jobs) == 0 {
			return nil
		}
		blanked := make([][]byte, len(jobs))
		for k := range jobs {
			blanked[k] = jobs[k].Source
		}
		next := make([][]byteRange, len(files))
		if err := run(ctx, jobs, func(k int, r parse.Result) {
			i := order[k]
			if r.Err != nil || r.SyntaxError || r.Root == nil {
				return
			}
			var keep []byteRange
			for _, c := range ranges[i] {
				if inFieldPosition(r.Root, blanked[k], c) {
					keep = append(keep, c)
				}
			}
			if len(keep) == len(ranges[i]) {
				structureTag("RS-F4")
				restoreText(r.Root, files[i].source)
				accept(i, r.Root)
				return
			}
			if round == 0 {
				next[i] = keep
			}
		}); err != nil {
			return err
		}
		ranges = next
	}
	return nil
}
