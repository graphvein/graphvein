// Package tauri links Tauri invoke calls in TypeScript to the Rust commands
// a proven app registers (ADR 0038). It reads the cached Tauri syntax (ADR
// 0037), the Rust resolver (ADR 0016), the TypeScript module resolver and the
// client evaluator (ADR 0020) at link time, so nothing it decides is cached.
//
// The rules only ever produce a proven edge or no edge: a command whose
// registry, name or host app is not proven is still emitted, with the
// unproven part named in its status or reason (VISION, principle 6).
package tauri

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Input carries the link-time inventories this linker reads. Rust and
// TypeScript are the per-language fact maps the index already builds for its
// other linkers, and Sources holds every scanned file (Cargo manifests and
// permissions files included), normalized as in the index.
type Input struct {
	Rust       map[string]facts.Facts
	Resolver   *rust.ModuleResolver
	TypeScript map[string]facts.Facts
	Modules    *typescript.ModuleResolver
	Sources    map[string][]byte
	Tests      testscope.Rules
}

// Link emits ipc_command and ipc_call nodes, their contains edges,
// exposes_command and invokes_command edges, and tauri_* diagnostics. Each
// record depends only on its own file's syntax and the current resolvers, so
// a problem in one file never stops the others.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	l := &linker{in: in, commands: map[string]command{}, lines: map[string][]int{}}
	l.collectCommands()
	l.collectRegistries()
	l.scopeApp()
	l.linkInvokes()
	sort.Slice(l.nodes, func(i, j int) bool { return l.nodes[i].Identity < l.nodes[j].Identity })
	return l.nodes, l.edges, l.diagnostics
}

type linker struct {
	in          Input
	nodes       []contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
	// commands holds K1 command functions by function identity.
	commands   map[string]command
	registries []*registry
	app        appScope
	lines      map[string][]int
	// roots collects the constructor calls of the proof in progress.
	roots []contract.Span
	// foreign holds production invoke_handler calls whose generate_handler!
	// is not Tauri's.
	foreign []appSite
}

// command is a proven command function: its invoke name (nil when unknown),
// whether it exists only in some build configurations (K3) and whether it
// exists only in test builds (m2: its own or an enclosing cfg, or a
// cfg_attr predicate, holds only under test).
type command struct {
	name        *string
	conditional bool
	test        bool
}

// diagnostic records a message naming the file, line and reason.
func (l *linker) diagnostic(file, severity, code string, line int, message string) {
	l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: fmt.Sprintf("%s:%d: %s", file, line, message)})
}

func (l *linker) test(file string) bool {
	return testscope.Classify(file, l.in.Tests) == testscope.Test
}

// position converts a byte offset in a scanned file to a contract position.
func (l *linker) position(file string, offset uint32) (int, int, bool) {
	starts, ok := l.lines[file]
	if !ok {
		source, found := l.in.Sources[file]
		if !found {
			return 0, 0, false
		}
		starts = []int{0}
		for i, b := range source {
			if b == '\n' {
				starts = append(starts, i+1)
			}
		}
		l.lines[file] = starts
	}
	if int(offset) > len(l.in.Sources[file]) {
		return 0, 0, false
	}
	line := sort.Search(len(starts), func(i int) bool { return starts[i] > int(offset) }) - 1
	return line + 1, int(offset) - starts[line], true
}

// offset converts a contract position (1-based line, byte column) back to a
// byte offset in a scanned file.
func (l *linker) offset(file string, line, column int) (int, bool) {
	if _, _, ok := l.position(file, 0); !ok {
		return 0, false
	}
	starts := l.lines[file]
	if line < 1 || line > len(starts) {
		return 0, false
	}
	at := starts[line-1] + column
	if at > len(l.in.Sources[file]) {
		return 0, false
	}
	return at, true
}

// contains reports whether span holds the byte offset (start inclusive, end
// exclusive).
func (l *linker) contains(file string, span contract.Span, offset uint32) bool {
	line, column, ok := l.position(file, offset)
	if !ok {
		return false
	}
	after := line > span.StartLine || line == span.StartLine && column >= span.StartColumn
	before := line < span.EndLine || line == span.EndLine && column < span.EndColumn
	return after && before
}

// hasControl reports whether s holds a U+0000-U+001F character; such a name
// is treated as unknown so attribute values never carry control characters.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 {
			return true
		}
	}
	return false
}

// nullable returns a JSON string or null, so unknown and absent stay distinct.
func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
