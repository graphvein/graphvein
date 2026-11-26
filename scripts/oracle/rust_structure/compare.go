//go:build ignore

// Command compare checks what Graphvein emits for Rust (declarations, spans,
// content hashes, contains edges, `mod` and `use` import edges) against two
// oracles that do not share Graphvein's code or assumptions:
//
//   - declarations: the `rust-structure-oracle` binary in this directory
//     (syn over the raw bytes, identity rules of docs/lang/identity.md);
//   - crate roots: `cargo metadata --no-deps --offline` (cargo's own target
//     discovery; it executes nothing and reads no network);
//   - module tree and `use` resolution: rustc's own resolver, printed with
//     `RUSTC_BOOTSTRAP=1 rustc -Zunpretty=hir-tree` for every cargo target
//     (all features, cfg(test)), with every dependency replaced by an empty
//     stub crate. rustc parses, loads `mod` files, expands built-in and
//     `macro_rules!` macros and resolves names; no build script, proc macro
//     or corpus code runs (stubs are empty rlibs, nothing is linked).
//
// Paths through a stubbed workspace crate (or a local re-export of a stubbed
// crate) are finished by walking the module and `use` tables rustc printed for
// that crate; every such leaf is counted (`via_table`).
//
// This program starts no process (the network boundary test forbids os/exec
// outside its allow-list): run.sh runs cargo, rustc and the declaration
// oracle between the phases:
//
//	compare -phase files   -snapshot S -root R -work W > W/files.txt
//	rust-structure-oracle R < W/files.txt > W/decls.jsonl
//	cargo metadata ... (one per manifest) > W/meta/<n>.json
//	compare -phase plan    -snapshot S -root R -work W   (writes W/plan.sh)
//	RUSTC=... bash W/plan.sh
//	compare -phase compare -snapshot S -root R -work W -corpus N -out O
package main

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	root     = flag.String("root", "", "checkout")
	snapshot = flag.String("snapshot", "", "Graphvein NDJSON snapshot")
	corpus   = flag.String("corpus", "", "corpus name")
	phase    = flag.String("phase", "compare", "files (print the Rust inventory), plan (write plan.sh and runs.json), compare")
	work     = flag.String("work", "", "scratch directory")
	outPath  = flag.String("out", "", "JSONL output")
	rootsIn  = flag.String("roots", "", "Graphvein's crate roots (JSON written by TestRustStructureOracle)")
)

// ---------- output ----------

type record map[string]any

var records []record
var counts = map[string]int{}

func emit(class, file, detail string, extra record) {
	counts[class]++
	r := record{"kind": "disagreement", "corpus": *corpus, "class": class, "file": file, "detail": detail}
	for k, v := range extra {
		r[k] = v
	}
	records = append(records, r)
}

func count(k string, n int) { counts[k] += n }

// ---------- raw files ----------

var raw = map[string][]byte{}

func readRaw(p string) []byte {
	if b, ok := raw[p]; ok {
		return b
	}
	b, _ := os.ReadFile(filepath.Join(*root, p))
	raw[p] = b
	return b
}

// lineStarts by LF only, as rustc and the Rust lexer count lines.
func lineStart(b []byte, line int) int {
	if line <= 1 {
		return 0
	}
	n := 1
	for i, c := range b {
		if c == '\n' {
			n++
			if n == line {
				return i + 1
			}
		}
	}
	return len(b)
}

// offset of (line, byte column) in raw bytes.
func byteOffset(p string, line, col int) int { return lineStart(readRaw(p), line) + col }

// offset of rustc's (line, 1-based char column).
func charOffset(p string, line, col int) int {
	b := readRaw(p)
	i := lineStart(b, line)
	for c := 1; c < col && i < len(b); c++ {
		_, n := utf8.DecodeRune(b[i:])
		i += n
	}
	return i
}

func hash(b []byte) string {
	s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s)))
}

// ---------- snapshot ----------

type span struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

type evidence struct {
	File string `json:"file"`
	Span span   `json:"span"`
}

type snapRec struct {
	Type          string     `json:"type"`
	Path          string     `json:"path"`
	Language      string     `json:"language"`
	Identity      string     `json:"identity"`
	Kind          string     `json:"kind"`
	Name          string     `json:"name"`
	QualifiedName string     `json:"qualified_name"`
	File          string     `json:"file"`
	Span          *span      `json:"span"`
	ContentHash   string     `json:"content_hash"`
	Source        string     `json:"source"`
	Target        string     `json:"target"`
	Evidence      []evidence `json:"evidence"`
	Code          string     `json:"code"`
	Message       string     `json:"message"`
}

type gvNode struct {
	snapRec
	start, end int
	owner      string
}

var (
	rustFiles []string
	gvNodes   = map[string][]*gvNode{} // file -> nodes
	gvImports = map[string]map[int]map[string]bool{}
	gvEvSpan  = map[string]map[int]span{}
	gvDiags   []snapRec
)

func fileOf(identity string) string {
	i := strings.Index(identity, ":")
	j := strings.LastIndex(identity, "#")
	if i < 0 || j < 0 {
		return ""
	}
	s, _ := url.PathUnescape(identity[i+1 : j])
	return s
}

func targetName(identity string) string {
	if strings.HasPrefix(identity, "external:") {
		j := strings.LastIndex(identity, "#")
		s, _ := url.PathUnescape(identity[j+1:])
		return "external:" + strings.TrimPrefix(s, "cargo:")
	}
	return "file:" + fileOf(identity)
}

func loadSnapshot() {
	f, err := os.Open(*snapshot)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	byID := map[string]*gvNode{}
	var contains []snapRec
	for sc.Scan() {
		var r snapRec
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			panic(err)
		}
		switch r.Type {
		case "file":
			if r.Language == "rust" {
				rustFiles = append(rustFiles, r.Path)
			}
		case "node":
			if r.Kind == "file" || r.Kind == "external" || !strings.HasSuffix(r.File, ".rs") || r.Span == nil {
				continue
			}
			if !strings.HasPrefix(r.Identity, "function:") && !strings.HasPrefix(r.Identity, "method:") && !strings.HasPrefix(r.Identity, "struct:") &&
				!strings.HasPrefix(r.Identity, "enum:") && !strings.HasPrefix(r.Identity, "union:") && !strings.HasPrefix(r.Identity, "trait:") &&
				!strings.HasPrefix(r.Identity, "type_alias:") && !strings.HasPrefix(r.Identity, "constant:") && !strings.HasPrefix(r.Identity, "variable:") &&
				!strings.HasPrefix(r.Identity, "macro:") {
				continue
			}
			n := &gvNode{snapRec: r}
			gvNodes[r.File] = append(gvNodes[r.File], n)
			byID[r.Identity] = n
		case "edge":
			switch r.Kind {
			case "contains":
				contains = append(contains, r)
			case "imports":
				from := fileOf(r.Source)
				if !strings.HasSuffix(from, ".rs") {
					continue
				}
				for _, ev := range r.Evidence {
					if gvImports[ev.File] == nil {
						gvImports[ev.File] = map[int]map[string]bool{}
						gvEvSpan[ev.File] = map[int]span{}
					}
					off := byteOffset(ev.File, ev.Span.StartLine, ev.Span.StartColumn)
					if gvImports[ev.File][off] == nil {
						gvImports[ev.File][off] = map[string]bool{}
					}
					gvImports[ev.File][off][targetName(r.Target)] = true
					gvEvSpan[ev.File][off] = ev.Span
				}
			}
		case "diagnostic":
			if strings.HasSuffix(r.File, ".rs") || strings.HasSuffix(r.File, "Cargo.toml") {
				gvDiags = append(gvDiags, r)
			}
		}
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
	for _, e := range contains {
		if n := byID[e.Target]; n != nil {
			if n.owner != "" {
				n.owner += "|"
			}
			n.owner += e.Source
		}
	}
	for file, ns := range gvNodes {
		for _, n := range ns {
			n.start = byteOffset(file, n.Span.StartLine, n.Span.StartColumn)
			n.end = byteOffset(file, n.Span.EndLine, n.Span.EndColumn)
		}
	}
	sort.Strings(rustFiles)
}

// ---------- declaration oracle ----------

type site struct {
	Pos   string `json:"pos"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Q     string `json:"q"`
	Owner string `json:"owner"`
	// Parent is the index of the containing site (a trait for its methods,
	// the enclosing function for an item in a function body), -1 for none.
	Parent int    `json:"parent"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Span   [4]int `json:"span"`
	Cfg    bool   `json:"cfg"`
}

type declFile struct {
	File   string         `json:"file"`
	OK     bool           `json:"ok"`
	Error  string         `json:"error"`
	Flags  []string       `json:"flags"`
	Sites  []site         `json:"sites"`
	Census map[string]int `json:"census"`
}

var decls = map[string]*declFile{}

// readDecls reads the declaration oracle's output (run.sh feeds it the
// file list printed by -phase files).
func readDecls() {
	out, err := os.ReadFile(filepath.Join(*work, "decls.jsonl"))
	if err != nil {
		panic(fmt.Errorf("decl oracle output: %w", err))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var d declFile
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			panic(err)
		}
		decls[d.File] = &d
	}
}

// modelled reports whether a site's position is claimed: module items (rows
// RS-D1..RS-D8, the first numbering pass) and, since step T020-S4X, impl
// associated consts and types (RS-D9) and items in function bodies (RS-D10),
// the second pass.
func modelled(pos string) bool {
	return firstPass(pos) || secondPass(pos)
}

func firstPass(pos string) bool {
	switch pos {
	case "item", "impl_fn", "trait_fn", "foreign_fn", "foreign_static":
		return true
	}
	return false
}

func secondPass(pos string) bool { return pos == "impl_assoc" || pos == "fn_item" }

func compareDecls() {
	census := map[string]int{}
	for _, file := range rustFiles {
		d := decls[file]
		gv := gvNodes[file]
		if d == nil || !d.OK {
			count("files_rejected_by_syn", 1)
			if len(gv) == 0 {
				continue
			}
			emit("D-REJECTED", file, fmt.Sprintf("syn rejects the file (%s); Graphvein emits %d declarations", d.Error, len(gv)), record{"graphvein_nodes": len(gv)})
			continue
		}
		count("files_parsed", 1)
		for k, v := range d.Census {
			census[k] += v
		}
		for _, f := range d.Flags {
			count("file_flag_"+f, 1)
		}
		lone := false
		for _, f := range d.Flags {
			lone = lone || f == "lone_cr"
		}
		// Expected identities: modelled sites with @n suffixes over the
		// modelled sites of the file, in source order: module items first,
		// then impl associated items and items in function bodies.
		seen := map[string]int{}
		bySpan := map[[2]int]*site{}
		expectQ := map[[2]int]string{}
		for i := range d.Sites {
			s := &d.Sites[i]
			bySpan[[2]int{s.Start, s.End}] = s
		}
		for _, pass := range []func(string) bool{firstPass, secondPass} {
			for i := range d.Sites {
				s := &d.Sites[i]
				if !pass(s.Pos) {
					continue
				}
				seen[s.Q]++
				q := s.Q
				if seen[s.Q] > 1 {
					q += "@" + strconv.Itoa(seen[s.Q])
				}
				expectQ[[2]int{s.Start, s.End}] = q
				count("oracle_decl_"+s.Kind, 1)
				count("oracle_decl_pos_"+s.Pos, 1)
			}
		}
		matched := map[[2]int]bool{}
		fileID := "file:" + file + "#" + file
		for _, n := range gv {
			count("graphvein_decl_"+n.Kind, 1)
			key := [2]int{n.start, n.end}
			s := bySpan[key]
			if lone {
				emit("D-LONECR", file, fmt.Sprintf("%s %s in a file with a lone CR (Graphvein reads CR as a line end, Rust does not)", n.Kind, n.QualifiedName), nil)
				continue
			}
			if s == nil {
				emit("D-EXTRA", file, fmt.Sprintf("%s %s at %d:%d has no item at that span", n.Kind, n.QualifiedName, n.Span.StartLine, n.Span.StartColumn), nil)
				continue
			}
			matched[key] = true
			if !modelled(s.Pos) {
				cls := map[string]string{"local": "D-LOCAL", "impl_assoc_unnamed": "D-ASSOC"}[s.Pos]
				if cls == "" {
					cls = "D-OTHERPOS"
				}
				emit(cls, file, fmt.Sprintf("%s %s (%s %s, %s)", n.Kind, n.QualifiedName, s.Pos, s.Q, s.Owner), nil)
				continue
			}
			wantQ := expectQ[key]
			wantKind := s.Kind
			problems := []string{}
			if n.Kind != wantKind {
				problems = append(problems, fmt.Sprintf("kind %s, oracle %s", n.Kind, wantKind))
			}
			if n.Name != s.Name {
				problems = append(problems, fmt.Sprintf("name %s, oracle %s", n.Name, s.Name))
			}
			if n.QualifiedName != wantQ {
				problems = append(problems, fmt.Sprintf("qualified name %s, oracle %s", n.QualifiedName, wantQ))
			}
			wantID := wantKind + ":" + file + "#" + wantQ
			if got := decodeID(n.Identity); n.Kind == wantKind && n.QualifiedName == wantQ && got != wantID {
				problems = append(problems, fmt.Sprintf("identity %s, oracle %s", got, wantID))
			}
			if h := hash(readRaw(file)[s.Start:s.End]); h != n.ContentHash {
				problems = append(problems, "content hash")
			}
			wantOwner := fileID
			if s.Parent >= 0 {
				// The containing site's own expected identity (its @n
				// included): a trait, or the enclosing function.
				ps := d.Sites[s.Parent]
				wantOwner = ps.Kind + ":" + file + "#" + expectQ[[2]int{ps.Start, ps.End}]
			}
			if decodeID(n.owner) != wantOwner {
				problems = append(problems, fmt.Sprintf("contained by %s, oracle %s", n.owner, wantOwner))
			}
			if len(problems) == 0 {
				count("decl_agree", 1)
				count("decl_agree_"+n.Kind, 1)
				count("decl_agree_pos_"+s.Pos, 1)
				continue
			}
			cls := "D-MISMATCH"
			if s.Pos == "foreign_fn" && n.Kind == "method" {
				cls = "D-FOREIGN"
			} else if n.Kind == wantKind && n.QualifiedName != wantQ && strings.Split(n.QualifiedName, "@")[0] == strings.Split(wantQ, "@")[0] {
				cls = "D-SUFFIX"
			}
			emit(cls, file, fmt.Sprintf("%s %s: %s", n.Kind, n.QualifiedName, strings.Join(problems, "; ")), nil)
		}
		if lone {
			continue
		}
		for _, s := range d.Sites {
			if !modelled(s.Pos) || matched[[2]int{s.Start, s.End}] {
				continue
			}
			emit("D-MISSING", file, fmt.Sprintf("%s %s (%s) at %d:%d not emitted", s.Kind, expectQ[[2]int{s.Start, s.End}], s.Pos, s.Span[0], s.Span[1]), nil)
		}
	}
	keys := []string{}
	for k := range census {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		records = append(records, record{"kind": "census", "corpus": *corpus, "form": k, "count": census[k]})
		counts["census_"+k] = census[k]
	}
}

// decodeID unescapes an identity's path and qualified name (several owners
// are joined with "|").
func decodeID(id string) string {
	var parts []string
	for _, one := range strings.Split(id, "|") {
		kind, rest, _ := strings.Cut(one, ":")
		p, q, _ := strings.Cut(rest, "#")
		p, _ = url.PathUnescape(p)
		q, _ = url.PathUnescape(q)
		parts = append(parts, kind+":"+p+"#"+q)
	}
	return strings.Join(parts, "|")
}

// ---------- cargo metadata ----------

type cargoTarget struct {
	Kind       []string `json:"kind"`
	CrateTypes []string `json:"crate_types"`
	Name       string   `json:"name"`
	SrcPath    string   `json:"src_path"`
	Edition    string   `json:"edition"`
}

type cargoDep struct {
	Name   string  `json:"name"`
	Rename *string `json:"rename"`
	Kind   *string `json:"kind"`
	Path   *string `json:"path"`
}

type cargoPackage struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	ManifestPath string              `json:"manifest_path"`
	Edition      string              `json:"edition"`
	Targets      []cargoTarget       `json:"targets"`
	Dependencies []cargoDep          `json:"dependencies"`
	Features     map[string][]string `json:"features"`
}

var packages = map[string]*cargoPackage{} // by manifest path (relative)

// metadataFailed holds the manifests cargo metadata refused.
var metadataFailed = map[string]bool{}

func rel(p string) string {
	r, err := filepath.Rel(*root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

// loadMetadata reads the `cargo metadata --no-deps --offline` output run.sh
// wrote for every manifest: meta/<n>.path holds the manifest, meta/<n>.json
// the output, meta/<n>.err cargo's error.
func loadMetadata() {
	dir := filepath.Join(*work, "meta")
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".path") {
			names = append(names, strings.TrimSuffix(e.Name(), ".path"))
		}
	}
	sort.Strings(names)
	for _, n := range names {
		mb, _ := os.ReadFile(filepath.Join(dir, n+".path"))
		m := strings.TrimSpace(string(mb))
		b, err := os.ReadFile(filepath.Join(dir, n+".json"))
		if err != nil || len(b) == 0 {
			msg, _ := os.ReadFile(filepath.Join(dir, n+".err"))
			first := strings.TrimSpace(string(msg))
			if i := strings.Index(first, "\n"); i > 0 {
				first = first[:i]
			}
			emit("M-METADATA", m, "cargo metadata failed: "+first, nil)
			metadataFailed[m] = true
			continue
		}
		var meta struct {
			Packages []*cargoPackage `json:"packages"`
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			panic(err)
		}
		for _, p := range meta.Packages {
			r := rel(p.ManifestPath)
			if strings.HasPrefix(r, "..") || packages[r] != nil {
				continue
			}
			packages[r] = p
		}
	}
}

// ---------- rustc hir-tree ----------

type rspan struct {
	File       string
	SL, SC     int
	EL, EC     int
	Ctx        int
	start, end int
}

var spanRe = regexp.MustCompile(`^(.*):(\d+):(\d+): (\d+):(\d+) \(#(\d+)\),?$`)

func parseSpan(s string) *rspan {
	m := spanRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil
	}
	a := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	r := &rspan{File: filepath.ToSlash(filepath.Clean(m[1])), SL: a(2), SC: a(3), EL: a(4), EC: a(5), Ctx: a(6)}
	r.start = charOffset(r.File, r.SL, r.SC)
	r.end = charOffset(r.File, r.EL, r.EC)
	return r
}

type res struct {
	Kind  string // Def kind, or "" when not a Def
	Crate int
	Path  string // def path without the crate prefix
	CName string // crate name
}

var defRe = regexp.MustCompile(`DefId\((\d+):(\d+) ~ ([^\[]+)\[[0-9a-f]+\]([^)]*)\)`)

func parseRes(text string) *res {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "Def(") {
		return nil
	}
	kind := strings.TrimSpace(strings.TrimPrefix(t, "Def("))
	if i := strings.IndexAny(kind, ",( \n"); i >= 0 {
		kind = kind[:i]
	}
	m := defRe.FindStringSubmatch(t)
	if m == nil {
		return nil
	}
	c, _ := strconv.Atoi(m[1])
	return &res{Kind: kind, Crate: c, CName: m[3], Path: strings.TrimPrefix(m[4], "::")}
}

type useLeaf struct {
	defPath string
	span    *rspan // item span (leaf tree)
	path    *rspan
	kind    string // Single, Glob, ListStem
	binding string
	ns      []*res // resolved namespaces
	segs    []string
	segRes  []*res
}

type crateRun struct {
	name    string
	target  string
	root    string
	mods    map[string]string // def path -> module file
	modItem map[string]*rspan // def path -> item span of an outlined mod decl
	items   map[string]*rspan // def path -> item span
	kinds   map[string]string
	uses    []*useLeaf
	externs map[string]string // extern name -> workspace crate key (lib target src path)
	// packageOf maps the extern name of a dependency to its package name
	// (`-` as `_`): an external crate is named by its package.
	packageOf map[string]string
	ok        bool
	errLines  int
	config    string
}

func indent(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

var ownerRe = regexp.MustCompile(`^DefId\((\d+):(\d+) ~ [^\[]+\[[0-9a-f]+\](.*)\) => OwnerNodes \{$`)

func parseHirTree(path string, cr *crateRun) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// plan.sh compresses the HIR (hundreds of megabytes per target on the
	// larger corpora); an empty file is a run that printed nothing.
	var in io.Reader = f
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		z, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		defer z.Close()
		in = z
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	var owner string
	var lines []string
	flush := func() {
		if owner == "" && len(lines) == 0 {
			return
		}
		parseOwner(owner, lines, cr)
		lines = lines[:0]
	}
	for sc.Scan() {
		l := sc.Text()
		if m := ownerRe.FindStringSubmatch(l); m != nil {
			flush()
			owner = strings.TrimPrefix(m[3], "::")
			continue
		}
		lines = append(lines, l)
	}
	flush()
	return sc.Err()
}

func parseOwner(owner string, lines []string, cr *crateRun) {
	// Crate root module.
	for i, l := range lines {
		if strings.HasPrefix(l, "        node: Crate(") {
			for _, k := range lines[i:] {
				if strings.Contains(k, "inner_span:") {
					if s := parseSpan(strings.SplitN(k, "inner_span:", 2)[1]); s != nil {
						cr.mods[""] = s.File
					}
					return
				}
			}
		}
	}
	// Item at indent 12, fields at 16.
	itemStart := -1
	for i, l := range lines {
		if l == "            Item {" {
			itemStart = i
			break
		}
	}
	if itemStart < 0 {
		return
	}
	var kind string
	var kindLines []string
	var itemSpan *rspan
	for i := itemStart + 1; i < len(lines); i++ {
		l := lines[i]
		if indent(l) == 16 && strings.HasPrefix(strings.TrimSpace(l), "kind: ") {
			kind = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "kind: "))
			kind = strings.TrimRight(kind, "({ ,")
			for j := i + 1; j < len(lines) && indent(lines[j]) > 16; j++ {
				kindLines = append(kindLines, lines[j])
			}
		}
		if indent(l) == 16 && strings.HasPrefix(strings.TrimSpace(l), "span: ") {
			itemSpan = parseSpan(strings.TrimPrefix(strings.TrimSpace(l), "span: "))
		}
		if indent(l) <= 12 && strings.TrimSpace(l) != "" {
			break
		}
	}
	cr.kinds[owner] = kind
	if itemSpan != nil {
		cr.items[owner] = itemSpan
	}
	switch kind {
	case "Mod":
		for _, k := range kindLines {
			if strings.Contains(k, "inner_span:") {
				if s := parseSpan(strings.SplitN(k, "inner_span:", 2)[1]); s != nil {
					cr.mods[owner] = s.File
					if itemSpan != nil && itemSpan.Ctx == 0 && (s.File != itemSpan.File || s.start < itemSpan.start || s.start > itemSpan.end) {
						cr.modItem[owner] = itemSpan
					}
				}
				break
			}
		}
	case "Use":
		u := &useLeaf{defPath: owner, span: itemSpan}
		// Path span (indent 24), PerNS (indent 28 entries), segments (32),
		// use kind (20).
		var ns string
		var nsText strings.Builder
		flushNS := func() {
			if ns != "" {
				if r := parseRes(strings.TrimPrefix(strings.TrimSpace(nsText.String()), "Some(")); r != nil {
					u.ns = append(u.ns, r)
				}
			}
			ns = ""
			nsText.Reset()
		}
		inSegs := false
		var segText strings.Builder
		inSegRes := false
		for i := 0; i < len(kindLines); i++ {
			l := kindLines[i]
			t := strings.TrimSpace(l)
			in := indent(l)
			switch {
			case in == 24 && strings.HasPrefix(t, "span: ") && u.path == nil:
				u.path = parseSpan(strings.TrimPrefix(t, "span: "))
			case in == 28 && (strings.HasPrefix(t, "value_ns:") || strings.HasPrefix(t, "type_ns:") || strings.HasPrefix(t, "macro_ns:")):
				flushNS()
				ns = t[:strings.Index(t, ":")]
				nsText.WriteString(strings.TrimSpace(t[strings.Index(t, ":")+1:]))
			case in > 28 && ns != "":
				nsText.WriteString(" " + t)
			case in == 24 && t == "},":
				flushNS()
			case in == 24 && t == "segments: [":
				flushNS()
				inSegs = true
			case inSegs && in == 32 && strings.HasPrefix(t, "ident: "):
				id := strings.TrimSuffix(strings.TrimPrefix(t, "ident: "), ",")
				if k := strings.LastIndex(id, "#"); k >= 0 {
					id = id[:k]
				}
				u.segs = append(u.segs, id)
				u.segRes = append(u.segRes, nil)
			case inSegs && in == 32 && strings.HasPrefix(t, "res: "):
				inSegRes = true
				segText.Reset()
				segText.WriteString(strings.TrimPrefix(t, "res: "))
			case inSegs && inSegRes && in > 32:
				segText.WriteString(" " + t)
			case inSegs && inSegRes && in == 32:
				u.segRes[len(u.segRes)-1] = parseRes(segText.String())
				inSegRes = false
			case in == 20 && (strings.HasPrefix(t, "Single(") || t == "Glob," || t == "ListStem,"):
				inSegs = false
				u.kind = strings.TrimRight(t, "(,")
				if u.kind == "Single" && i+1 < len(kindLines) {
					b := strings.TrimSuffix(strings.TrimSpace(kindLines[i+1]), ",")
					if k := strings.LastIndex(b, "#"); k >= 0 {
						b = b[:k]
					}
					u.binding = b
				}
			}
		}
		cr.uses = append(cr.uses, u)
	}
}

// ---------- running rustc (planned here, run by run.sh) ----------

// runSpec is one rustc invocation; -phase plan writes them to runs.json and
// the commands to plan.sh, -phase compare reads hir/<Index>.txt and .err.
type runSpec struct {
	Index   int               `json:"index"`
	Name    string            `json:"name"`
	Target  string            `json:"target"`
	Root    string            `json:"root"`
	Externs map[string]string `json:"externs"`
	Args    []string          `json:"args"`
	// Packages maps each dependency extern name to its package name.
	Packages map[string]string `json:"packages"`
	// Config names the cfg configuration (configs).
	Config string `json:"config"`
}

func crateIdent(s string) string { return strings.ReplaceAll(s, "-", "_") }

var runs []*crateRun

func libTarget(p *cargoPackage) *cargoTarget {
	for i := range p.Targets {
		for _, k := range p.Targets[i].Kind {
			if k == "lib" || k == "rlib" || k == "proc-macro" || k == "dylib" || k == "cdylib" || k == "staticlib" {
				return &p.Targets[i]
			}
		}
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// oracleConfig is one cfg configuration of the rustc runs: the target
// (empty: the host, x86_64-unknown-linux-gnu), cfg(test) on or off, and
// every feature of the package or none (no default features).
type oracleConfig struct {
	Name     string
	Target   string
	Test     bool
	Features bool
}

// configs is the fixed configuration set (ADR 0071, "cfg coverage"): target
// OS linux, windows and macos (x86_64 each; the windows and macos standard
// libraries are the rust-std components of the pinned toolchain), cfg(test)
// on and off, all features and no features.
var configs = func() []oracleConfig {
	var out []oracleConfig
	for _, t := range []struct{ name, triple string }{{"linux", ""}, {"windows", "x86_64-pc-windows-msvc"}, {"macos", "x86_64-apple-darwin"}} {
		for _, test := range []bool{true, false} {
			for _, all := range []bool{true, false} {
				name := t.name
				if test {
					name += "+test"
				} else {
					name += "-test"
				}
				if all {
					name += "+features"
				} else {
					name += "-features"
				}
				out = append(out, oracleConfig{Name: name, Target: t.triple, Test: test, Features: all})
			}
		}
	}
	return out
}()

// plan writes plan.sh (empty stub crates for every dependency name and
// target, then one rustc -Zunpretty=hir-tree per cargo target and
// configuration, run in the checkout so spans are relative to it) and
// runs.json.
func plan() {
	var manifests []string
	for m := range packages {
		manifests = append(manifests, m)
	}
	sort.Strings(manifests)
	byPath := map[string]*cargoPackage{}
	for _, m := range manifests {
		byPath[filepath.Dir(packages[m].ManifestPath)] = packages[m]
	}
	var sh strings.Builder
	sh.WriteString("#!/usr/bin/env bash\n# Generated by compare.go -phase plan. RUSTC must be set.\nset -u\n")
	sh.WriteString("mkdir -p " + shellQuote(filepath.Join(*work, "hir")) + "\n")
	// rustc writes an internal compiler error report into its working
	// directory, which for the hir-tree runs is the checkout: send every
	// report to the scratch directory instead, one directory per run.
	sh.WriteString("export RUSTC_ICE=" + shellQuote(filepath.Join(*work, "ice", "stubs")) + "\nmkdir -p \"$RUSTC_ICE\"\n")
	stubs := map[string]bool{}
	stubDir := func(target string) string {
		dir := filepath.Join(*work, "stubs", "host")
		if target != "" {
			dir = filepath.Join(*work, "stubs", target)
		}
		if !stubs[target+"/"] {
			stubs[target+"/"] = true
			sh.WriteString("mkdir -p " + shellQuote(dir) + " && printf '#![no_std]\\n' > " + shellQuote(filepath.Join(dir, "empty.rs")) + "\n")
		}
		return dir
	}
	stub := func(name, target string) string {
		dir := stubDir(target)
		out := filepath.Join(dir, "lib"+name+".rmeta")
		if !stubs[target+"/"+name] {
			stubs[target+"/"+name] = true
			flags := ""
			if target != "" {
				flags = " --target " + shellQuote(target)
			}
			sh.WriteString(`"$RUSTC" --edition 2021` + flags + ` --crate-type rlib --crate-name ` + shellQuote(name) + " --emit=metadata -o " + shellQuote(out) + " " + shellQuote(filepath.Join(dir, "empty.rs")) + "\n")
		}
		return out
	}
	var specs []runSpec
	for _, cfg := range configs {
		for _, m := range manifests {
			p := packages[m]
			lib := libTarget(p)
			var features []string
			for f := range p.Features {
				features = append(features, f)
			}
			sort.Strings(features)
			for ti := range p.Targets {
				t := &p.Targets[ti]
				kind := t.Kind[0]
				ctype := "bin"
				if t == lib {
					ctype = "lib"
					if kind == "proc-macro" {
						ctype = "proc-macro"
					}
				}
				spec := runSpec{Index: len(specs), Name: crateIdent(t.Name), Target: kind + ":" + t.Name, Root: rel(t.SrcPath), Externs: map[string]string{}, Packages: map[string]string{}, Config: cfg.Name}
				args := []string{"--edition", t.Edition, "--crate-type", ctype, "--crate-name", spec.Name, "-Zunpretty=hir-tree", "--cap-lints", "allow"}
				if cfg.Test {
					args = append(args, "--test")
				}
				if cfg.Target != "" {
					args = append(args, "--target", cfg.Target)
				}
				args = append(args, "-L", stubDir(cfg.Target))
				if cfg.Features {
					for _, f := range features {
						args = append(args, "--cfg", fmt.Sprintf("feature=%q", f))
					}
				}
				externs := map[string]bool{}
				addExtern := func(name, workspaceRoot string) {
					if externs[name] {
						return
					}
					externs[name] = true
					args = append(args, "--extern", name+"="+stub(name, cfg.Target))
					if workspaceRoot != "" {
						spec.Externs[name] = workspaceRoot
					}
				}
				if t != lib && lib != nil && kind != "custom-build" {
					addExtern(crateIdent(lib.Name), rel(lib.SrcPath))
				}
				for _, d := range p.Dependencies {
					dk := ""
					if d.Kind != nil {
						dk = *d.Kind
					}
					if (dk == "build") != (kind == "custom-build") && !(dk == "dev" && kind != "custom-build") {
						continue
					}
					name := crateIdent(d.Name)
					wsRoot := ""
					if d.Path != nil {
						if dp := byPath[*d.Path]; dp != nil {
							if l := libTarget(dp); l != nil {
								name = crateIdent(l.Name)
								wsRoot = rel(l.SrcPath)
							}
						}
					}
					if d.Rename != nil {
						name = crateIdent(*d.Rename)
					}
					if !externs[name] {
						spec.Packages[name] = crateIdent(d.Name)
					}
					addExtern(name, wsRoot)
				}
				args = append(args, spec.Root)
				spec.Args = args
				specs = append(specs, spec)
			}
		}
	}
	for _, spec := range specs {
		var q []string
		for _, a := range spec.Args {
			q = append(q, shellQuote(a))
		}
		hir := filepath.Join(*work, "hir", strconv.Itoa(spec.Index))
		ice := filepath.Join(*work, "ice", strconv.Itoa(spec.Index))
		sh.WriteString("mkdir -p " + shellQuote(ice) + "\n")
		sh.WriteString("(cd " + shellQuote(*root) + ` && RUSTC_ICE=` + shellQuote(ice) + ` RUSTC_BOOTSTRAP=1 "$RUSTC" ` + strings.Join(q, " ") + " 2> " + shellQuote(hir+".err") + " | gzip -1 > " + shellQuote(hir+".txt.gz") + ") || true\n")
	}
	if err := os.WriteFile(filepath.Join(*work, "plan.sh"), []byte(sh.String()), 0o644); err != nil {
		panic(err)
	}
	b, _ := json.MarshalIndent(specs, "", " ")
	if err := os.WriteFile(filepath.Join(*work, "runs.json"), b, 0o644); err != nil {
		panic(err)
	}
}

// loadRuns parses every target's hir-tree output.
func loadRuns() {
	b, err := os.ReadFile(filepath.Join(*work, "runs.json"))
	if err != nil {
		panic(err)
	}
	var specs []runSpec
	if err := json.Unmarshal(b, &specs); err != nil {
		panic(err)
	}
	for _, spec := range specs {
		cr := &crateRun{config: spec.Config, name: spec.Name, target: spec.Target, root: spec.Root, mods: map[string]string{}, modItem: map[string]*rspan{}, items: map[string]*rspan{}, kinds: map[string]string{}, externs: spec.Externs, packageOf: spec.Packages}
		if cr.externs == nil {
			cr.externs = map[string]string{}
		}
		hir := filepath.Join(*work, "hir", strconv.Itoa(spec.Index))
		stderrB, _ := os.ReadFile(hir + ".err")
		stderr := string(stderrB)
		cr.errLines = strings.Count(stderr, "\nerror")
		if strings.HasPrefix(stderr, "error") {
			cr.errLines++
		}
		if err := parseHirTree(hir+".txt.gz", cr); err != nil {
			panic(err)
		}
		cr.ok = cr.mods[""] != ""
		if !cr.ok {
			first := stderr
			if len(first) > 300 {
				first = first[:300]
			}
			emit("M-RUSTC-FAILED", cr.root, "rustc printed no HIR for "+cr.target+" ("+cr.config+"): "+first, nil)
		}
		if ices, _ := os.ReadDir(filepath.Join(*work, "ice", strconv.Itoa(spec.Index))); len(ices) > 0 {
			emit("M-RUSTC-ICE", cr.root, "rustc crashed (internal compiler error) for "+cr.target+" ("+cr.config+"): "+filepath.Join(*work, "ice", strconv.Itoa(spec.Index), ices[0].Name()), nil)
		}
		count("rustc_targets", 1)
		count("rustc_error_messages", cr.errLines)
		runs = append(runs, cr)
	}
}

// ---------- resolution ----------

func lookupRun(rootFile, config string) *crateRun {
	for _, r := range runs {
		kind := strings.SplitN(r.target, ":", 2)[0]
		if r.root == rootFile && r.ok && r.config == config && (kind == "lib" || kind == "rlib" || kind == "proc-macro" || kind == "dylib" || kind == "cdylib" || kind == "staticlib") {
			return r
		}
	}
	return nil
}

// localFile maps a local def path to the file a Graphvein edge should name.
func localFile(cr *crateRun, r *res) string {
	if r.Kind == "Mod" {
		return cr.mods[r.Path]
	}
	p := r.Path
	for {
		if s := cr.items[p]; s != nil && cr.kinds[p] != "" {
			if s.Ctx == 0 {
				return s.File
			}
			break
		}
		i := strings.LastIndex(p, "::")
		if i < 0 {
			p = ""
			break
		}
		p = p[:i]
	}
	// Macro-generated or not an owner: the enclosing module's file.
	p = r.Path
	for {
		if f, ok := cr.mods[p]; ok {
			return f
		}
		i := strings.LastIndex(p, "::")
		if i < 0 {
			return cr.mods[""]
		}
		p = p[:i]
	}
}

func std(name string) string {
	if name == "core" || name == "alloc" {
		return "std"
	}
	return name
}

// externalName names the external crate behind an extern name of a run:
// its package (`ser = { package = "serde" }` is serde).
func externalName(cr *crateRun, name string) string {
	if p := cr.packageOf[name]; p != "" {
		return "external:" + std(p)
	}
	return "external:" + std(name)
}

type resolveCtx struct {
	depth   int
	table   bool
	visited map[string]bool
}

// place is where a path leads: a module of a crate run (isMod), or a
// target ("file:<path>" / "external:<crate>").
type place struct {
	run   *crateRun
	mod   string
	isMod bool
	t     string
}

// resolveLeaf returns the targets ("file:<path>" / "external:<crate>") of a use leaf.
func resolveLeaf(cr *crateRun, u *useLeaf, ctx *resolveCtx) []string {
	var out []string
	for _, p := range leafPlaces(cr, u, ctx) {
		if p.t != "" && p.t != "file:" {
			out = append(out, p.t)
		}
	}
	return dedupe(out)
}

func modPlace(w *crateRun, mod string) place {
	return place{run: w, mod: mod, isMod: true, t: "file:" + w.mods[mod]}
}

// leafPlaces follows rustc's resolution of a use leaf; where rustc stopped at
// a stub crate (or a local re-export of one), it continues in the tables.
func leafPlaces(cr *crateRun, u *useLeaf, ctx *resolveCtx) []place {
	key := cr.root + "#" + u.defPath
	if ctx.visited[key] || ctx.depth > 24 {
		return nil
	}
	ctx.visited[key] = true
	defer delete(ctx.visited, key)
	var out []place
	for _, r := range u.ns {
		switch {
		case r.Crate == 0 && r.Kind == "Mod":
			out = append(out, modPlace(cr, r.Path))
		case r.Crate == 0:
			if f := localFile(cr, r); f != "" {
				out = append(out, place{t: "file:" + f})
			}
		case r.Path == "" && cr.externs[r.CName] != "":
			if w := lookupRun(cr.externs[r.CName], cr.config); w != nil {
				out = append(out, modPlace(w, ""))
			} else {
				out = append(out, place{t: "file:" + cr.externs[r.CName]})
			}
		default:
			first := ""
			if len(u.segRes) > 0 && u.segRes[0] != nil && u.segRes[0].Crate != 0 && u.segRes[0].Path == "" {
				first = u.segRes[0].CName
			}
			if first == "" {
				first = r.CName
			}
			out = append(out, place{t: externalName(cr, first)})
		}
	}
	if len(out) > 0 {
		return out
	}
	deep := -1
	for i, r := range u.segRes {
		if r != nil {
			deep = i
		}
	}
	if deep < 0 {
		// rustc resolved no segment (an import through another import that
		// failed at a stub crate, such as `use header::X` after `use
		// reqwest::header`): finish from the module's use table.
		if len(u.segs) == 0 || u.kind == "Glob" {
			return nil
		}
		mod := parentOf(u.defPath)
		if _, ok := cr.mods[mod]; !ok {
			return nil
		}
		ctx.table = true
		return walk(cr, mod, u.segs, false, ctx)
	}
	r := u.segRes[deep]
	rest := u.segs[deep+1:]
	glob := u.kind == "Glob"
	switch {
	case r.Crate != 0 && r.Path == "" && cr.externs[r.CName] != "":
		if w := lookupRun(cr.externs[r.CName], cr.config); w != nil {
			ctx.table = true
			return walk(w, "", rest, glob, ctx)
		}
	case r.Crate != 0:
		return []place{{t: externalName(cr, r.CName)}}
	case r.Kind == "Mod":
		ctx.table = true
		return walk(cr, r.Path, rest, glob, ctx)
	}
	return nil
}

func parentOf(defPath string) string {
	if i := strings.LastIndex(defPath, "::"); i >= 0 {
		return defPath[:i]
	}
	return ""
}

// walk resolves the remaining segments from module mod of run w using the
// item and use tables rustc printed for that crate (glob: the path names the
// module of a glob import).
func walk(w *crateRun, mod string, rest []string, glob bool, ctx *resolveCtx) []place {
	if len(rest) == 0 {
		if _, ok := w.mods[mod]; ok {
			return []place{modPlace(w, mod)}
		}
		return nil
	}
	wkey := "walk:" + w.root + "#" + mod + "#" + strings.Join(rest, "::")
	if ctx.depth > 24 || ctx.visited[wkey] {
		return nil
	}
	ctx.visited[wkey] = true
	defer delete(ctx.visited, wkey)
	ctx.depth++
	defer func() { ctx.depth-- }()
	name := rest[0]
	if name == "self" {
		return walk(w, mod, rest[1:], glob, ctx)
	}
	if name == "super" {
		return walk(w, parentOf(mod), rest[1:], glob, ctx)
	}
	if name == "crate" {
		return walk(w, "", rest[1:], glob, ctx)
	}
	child := name
	if mod != "" {
		child = mod + "::" + name
	}
	if _, ok := w.mods[child]; ok {
		return walk(w, child, rest[1:], glob, ctx)
	}
	if len(rest) == 1 && !glob {
		if s := w.items[child]; s != nil && w.kinds[child] != "Use" && w.kinds[child] != "" {
			return []place{{t: "file:" + localFile(w, &res{Kind: w.kinds[child], Path: child})}}
		}
	}
	var out []place
	follow := func(ps []place, rest []string) {
		for _, p := range ps {
			switch {
			case len(rest) == 0:
				out = append(out, p)
			case p.isMod:
				out = append(out, walk(p.run, p.mod, rest, glob, ctx)...)
			case strings.HasPrefix(p.t, "external:"):
				out = append(out, p)
			}
		}
	}
	for _, u := range w.uses {
		if parentOf(u.defPath) == mod && u.kind == "Single" && u.binding == name {
			follow(leafPlaces(w, u, ctx), rest[1:])
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, u := range w.uses {
		if parentOf(u.defPath) != mod || u.kind != "Glob" {
			continue
		}
		for _, p := range leafPlaces(w, u, ctx) {
			if p.isMod && !(p.run == w && p.mod == mod) {
				out = append(out, walk(p.run, p.mod, rest, glob, ctx)...)
			}
		}
	}
	return out
}

func dedupe(in []string) []string {
	m := map[string]bool{}
	var out []string
	for _, s := range in {
		if !m[s] {
			m[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- import comparison ----------

type declKey struct {
	file string
	off  int
}

func compareImports() {
	// Syntax sites from the declaration oracle: use and mod declarations.
	useDecl := map[declKey]site{}
	modDecl := map[declKey]site{}
	for f, d := range decls {
		if !d.OK {
			continue
		}
		for _, s := range d.Sites {
			switch s.Pos {
			case "use_decl", "use_decl_local":
				useDecl[declKey{f, s.Start}] = s
			case "mod_decl", "mod_decl_local":
				modDecl[declKey{f, s.Start}] = s
			}
		}
	}
	// rustc results, unioned over every target that reached the declaration.
	oracle := map[declKey]map[string]bool{}
	reachedIn := map[declKey]map[string]bool{}
	reach := func(k declKey, config string) {
		if reachedIn[k] == nil {
			reachedIn[k] = map[string]bool{}
		}
		reachedIn[k][config] = true
	}
	resolvedBy := map[declKey]string{}
	unresolved := map[declKey]int{}
	reached := map[string]bool{}
	for _, cr := range runs {
		if !cr.ok {
			continue
		}
		for _, f := range cr.mods {
			reached[f] = true
		}
		for p, s := range cr.modItem {
			k := declKey{s.File, s.start}
			if oracle[k] == nil {
				oracle[k] = map[string]bool{}
			}
			oracle[k]["file:"+cr.mods[p]] = true
			resolvedBy[k] = "rustc_mod"
			reach(k, cr.config)
		}
		// Declaration span of each leaf: the largest use span containing it.
		var all []*rspan
		for _, u := range cr.uses {
			if u.span != nil && u.span.Ctx == 0 {
				all = append(all, u.span)
			}
		}
		for _, u := range cr.uses {
			if u.kind == "ListStem" || u.span == nil || u.span.Ctx != 0 {
				continue
			}
			decl := u.span
			for _, s := range all {
				if s.File == decl.File && s.start <= u.span.start && s.end >= u.span.end && s.end-s.start > decl.end-decl.start {
					decl = s
				}
			}
			k := declKey{decl.File, decl.start}
			if oracle[k] == nil {
				oracle[k] = map[string]bool{}
			}
			reach(k, cr.config)
			ctx := &resolveCtx{visited: map[string]bool{}}
			targets := resolveLeaf(cr, u, ctx)
			if len(targets) == 0 {
				unresolved[k]++
			}
			for _, t := range targets {
				oracle[k][t] = true
			}
			if ctx.table {
				resolvedBy[k] = "via_table"
			} else if resolvedBy[k] == "" {
				resolvedBy[k] = "rustc_res"
			}
		}
	}
	for _, f := range rustFiles {
		if !reached[f] {
			count("files_in_no_rustc_crate", 1)
			records = append(records, record{"kind": "unreached_file", "corpus": *corpus, "file": f})
		}
	}
	// Compare per declaration.
	keys := map[declKey]bool{}
	for f, m := range gvImports {
		for off := range m {
			keys[declKey{f, off}] = true
		}
	}
	for k := range oracle {
		keys[k] = true
	}
	var ordered []declKey
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].file != ordered[j].file {
			return ordered[i].file < ordered[j].file
		}
		return ordered[i].off < ordered[j].off
	})
	for _, k := range ordered {
		gv := gvImports[k.file][k.off]
		or, seen := oracle[k]
		form := "other"
		if _, ok := useDecl[k]; ok {
			form = "use"
		} else if _, ok := modDecl[k]; ok {
			form = "mod"
		}
		if strings.HasSuffix(useDecl[k].Pos, "_local") || strings.HasSuffix(modDecl[k].Pos, "_local") {
			form += "_in_block"
		}
		count("decl_"+form, 1)
		line := 0
		if s, ok := gvEvSpan[k.file][k.off]; ok {
			line = s.StartLine
		} else {
			b := readRaw(k.file)
			line = strings.Count(string(b[:min(k.off, len(b))]), "\n") + 1
		}
		where := fmt.Sprintf("%s:%d", k.file, line)
		if !seen {
			if len(gv) > 0 {
				reason := "cfg-inactive or unreached"
				if !reached[k.file] {
					reason = "file in no rustc crate"
				}
				count("graphvein_edges_not_compared", len(gv))
				records = append(records, record{"kind": "not_compared", "corpus": *corpus, "file": k.file, "line": line, "form": form, "reason": reason, "graphvein": keysOf(gv)})
			}
			continue
		}
		extra := []string{}
		for t := range gv {
			if !or[t] {
				extra = append(extra, t)
			}
		}
		missing := []string{}
		for t := range or {
			if !gv[t] {
				missing = append(missing, t)
			}
		}
		sort.Strings(extra)
		sort.Strings(missing)
		count("compared_decl_"+form, 1)
		count("graphvein_edges_compared", len(gv))
		if len(gv) > 0 {
			for c := range reachedIn[k] {
				count("graphvein_evidence_reached_in_"+c, 1)
			}
			if len(reachedIn[k]) == 1 {
				for c := range reachedIn[k] {
					count("graphvein_evidence_reached_only_in_"+c, 1)
				}
			}
		}
		count("graphvein_edges_agree", len(gv)-len(extra))
		if len(extra) == 0 && len(missing) == 0 {
			continue
		}
		cls := ""
		switch {
		case len(extra) > 0 && len(or) == 0:
			cls = "I-UNPROVEN" // Graphvein edge, rustc resolves nothing (stub or error)
		case len(extra) > 0:
			cls = "I-WRONG"
		default:
			cls = "I-MISSING"
		}
		count(cls+"_edges", max(len(extra), len(missing)))
		emit(cls, k.file, fmt.Sprintf("%s %s: graphvein %v, oracle %v (%s, unresolved leaves %d)", form, where, keysOf(gv), keysOf(or), resolvedBy[k], unresolved[k]), record{"line": line, "form": form, "graphvein": keysOf(gv), "oracle": keysOf(or), "extra": extra, "missing": missing, "by": resolvedBy[k]})
	}
}

func keysOf(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	flag.Parse()
	loadSnapshot()
	switch *phase {
	case "files":
		for _, f := range rustFiles {
			fmt.Println(f)
		}
		return
	case "plan":
		loadMetadata()
		plan()
		return
	case "compare":
	default:
		panic("unknown -phase " + *phase)
	}
	readDecls()
	compareDecls()
	loadMetadata()
	compareRoots()
	loadRuns()
	compareImports()
	keys := []string{}
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	summary := record{"kind": "summary", "corpus": *corpus}
	for _, k := range keys {
		summary[k] = counts[k]
		fmt.Printf("%-48s %d\n", k, counts[k])
	}
	f, err := os.Create(*outPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.Encode(summary)
	for _, r := range records {
		enc.Encode(r)
	}
}

// ---------- crate roots ----------

// gvRoot is one crate root Graphvein reads (row RS-M1).
type gvRoot struct {
	Manifest, File, Kind string
}

// targetKind normalises a cargo target kind to Graphvein's root kinds.
func targetKind(t cargoTarget) string {
	switch k := t.Kind[0]; k {
	case "lib", "rlib", "proc-macro", "dylib", "cdylib", "staticlib":
		return "lib"
	default:
		return k
	}
}

// compareRoots compares Graphvein's crate roots with the targets cargo
// metadata reports for the same manifest: a Graphvein root that is no
// target of its package (file and kind) is R-EXTRA; a target Graphvein does
// not read as a root is R-MISSING (an omission). Manifests cargo refused are
// not compared.
func compareRoots() {
	if *rootsIn == "" {
		return
	}
	b, err := os.ReadFile(*rootsIn)
	if err != nil {
		panic(err)
	}
	var roots []gvRoot
	if err := json.Unmarshal(b, &roots); err != nil {
		panic(err)
	}
	gv := map[string]bool{}
	byManifest := map[string]bool{}
	for _, r := range roots {
		gv[r.Manifest+"\x00"+r.File+"\x00"+r.Kind] = true
		byManifest[r.Manifest] = true
		if metadataFailed[r.Manifest] {
			count("roots_not_compared", 1)
			records = append(records, record{"kind": "not_compared", "corpus": *corpus, "file": r.File, "form": "root", "reason": "cargo metadata failed for " + r.Manifest})
			continue
		}
		p := packages[r.Manifest]
		found := false
		if p != nil {
			for _, t := range p.Targets {
				found = found || rel(t.SrcPath) == r.File && targetKind(t) == r.Kind
			}
		}
		count("roots_compared", 1)
		if !found {
			emit("R-EXTRA", r.File, fmt.Sprintf("%s root %s of %s is no cargo target of that package", r.Kind, r.File, r.Manifest), record{"manifest": r.Manifest})
			continue
		}
		count("roots_agree", 1)
	}
	var manifests []string
	for m := range packages {
		manifests = append(manifests, m)
	}
	sort.Strings(manifests)
	for _, m := range manifests {
		for _, t := range packages[m].Targets {
			f := rel(t.SrcPath)
			if gv[m+"\x00"+f+"\x00"+targetKind(t)] {
				continue
			}
			count("R-MISSING_roots", 1)
			emit("R-MISSING", f, fmt.Sprintf("cargo target %s:%s (%s) is no Graphvein root", targetKind(t), t.Name, m), record{"manifest": m})
		}
	}
}
