// Package packages states confirmed package manifests as level-2 `package`
// nodes (T026, ADR 0079, rows CM-M1..CM-M5, CM-W1 and CM-U1 of the
// components claim contract in docs/contract/claims/architecture-graph.md).
//
// The indexer only states what a manifest says: its ecosystem, its name,
// its workspace members, binary markers and dependency names. The server
// derives components, kinds and visibility from these nodes and the other
// stored facts (ADR 0079 option 1A), so a configuration change never needs
// a re-index. A manifest the reader refuses is a diagnostic
// (`component_manifest_unread`), never a guess: the server shows its
// directory as an unread component.
package packages

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Row is one row of the indexer half of the components claim contract.
// The server holds the other rows (CM-A1, CM-R1, CK, CV, CD).
type Row struct {
	ID string
	// Form names the manifest form the row reads, as the task file names it.
	Form string
}

// Contract lists the indexer rows in task-file order. Every form the code
// reads maps to exactly one row; every form no row covers is CM-U1 (the
// single unknown branch: the reader refuses, the directory is unread).
var Contract = []Row{
	{"CM-M1", "package.json"},
	{"CM-M2", "Cargo.toml"},
	{"CM-M3", "pyproject.toml"},
	{"CM-M4", "go.mod"},
	{"CM-M5", "setup.py"},
	{"CM-W1", "workspace root"},
	{"CM-U1", "unread"},
}

// UnreadCode is the diagnostic code of row CM-U1. The server reads it to
// show the manifest's directory as an unread component of unknown kind.
const UnreadCode = "component_manifest_unread"

// claimTag is a test-only hook: every branch reports its row ("<row>" or
// "<row>/<form>").
var claimTag func(row string)

func tag(row string) {
	if claimTag != nil {
		claimTag(row)
	}
}

// Input is what the pass reads: every retained source by path and the
// cached Python and Go facts (setup.py calls, Go package clauses). It
// parses nothing itself beyond manifest text.
type Input struct {
	Sources map[string][]byte
	Python  map[string]typescript.Facts
	Go      map[string]typescript.Facts
}

// manifest is one read manifest before it becomes a node.
type manifest struct {
	file      string
	ecosystem string
	name      string // empty when the manifest names none (CM-M5 only)
	attrs     map[string]any
}

// Link reads every scanned manifest and returns one `package` node per
// confirmed manifest and one diagnostic per refused one. A failure in one
// manifest never affects another.
func Link(in Input) ([]contract.Node, []contract.Diagnostic) {
	files := make([]string, 0, len(in.Sources))
	for p := range in.Sources {
		files = append(files, p)
	}
	sort.Strings(files)
	var found []manifest
	var diags []contract.Diagnostic
	unread := func(file, reason string) {
		tag("CM-U1")
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: UnreadCode,
			Message: fmt.Sprintf("%s: %s; its directory is an unread component of unknown kind (row CM-U1)", file, reason)})
	}
	// enclosing holds every directory with a CM-M1..M4 package or a CM-U1
	// manifest of those names: CM-M5 reads setup.py/setup.cfg only outside
	// them.
	enclosing := map[string]bool{}
	var legacy []string
	for _, file := range files {
		source := string(in.Sources[file])
		var m *manifest
		var reason string
		switch path.Base(file) {
		case "package.json":
			m, reason = readPackageJSON(file, source)
		case "Cargo.toml":
			m, reason = readCargo(file, source)
		case "pyproject.toml":
			m, reason = readPyproject(file, source)
		case "go.mod":
			m, reason = readGoMod(file, source, in.Go, files)
		case "setup.py", "setup.cfg":
			legacy = append(legacy, file)
			continue
		default:
			continue
		}
		switch {
		case reason != "":
			unread(file, reason)
			enclosing[path.Dir(file)] = true
		case m != nil:
			found = append(found, *m)
			enclosing[path.Dir(file)] = true
		}
	}
	for _, file := range legacy {
		dir := path.Dir(file)
		if encloses(enclosing, dir) {
			tag("CM-M5/enclosed")
			continue
		}
		var m *manifest
		var reason string
		if path.Base(file) == "setup.py" {
			m, reason = readSetupPy(file, string(in.Sources[file]), in.Python[file])
		} else {
			m = readSetupCfg(file, string(in.Sources[file]))
		}
		switch {
		case reason != "":
			unread(file, reason)
		case m != nil:
			found = append(found, *m)
		}
	}
	nodes := make([]contract.Node, 0, len(found))
	for _, m := range found {
		n, err := node(m, in.Sources[m.file])
		if err != nil {
			unread(m.file, err.Error())
			continue
		}
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	sort.SliceStable(diags, func(i, j int) bool { return diags[i].File < diags[j].File })
	return nodes, diags
}

// encloses reports whether dir or one of its ancestors is in set.
func encloses(set map[string]bool, dir string) bool {
	for {
		if set[dir] {
			return true
		}
		if dir == "." {
			return false
		}
		dir = path.Dir(dir)
	}
}

// node builds the contract record: identity package:<manifest>#<name or
// directory>, level 2 (area), spanning the whole manifest.
func node(m manifest, source []byte) (contract.Node, error) {
	dir := path.Dir(m.file)
	qualified := m.name
	display := m.name
	if qualified == "" {
		qualified = dir
		display = path.Base(dir)
		if dir == "." {
			display = "."
		}
	}
	identity, err := contract.Identity("package", m.file, qualified)
	if err != nil {
		return contract.Node{}, fmt.Errorf("package name %q cannot form an identity", qualified)
	}
	attrs := map[string]any{"ecosystem": m.ecosystem, "directory": dir}
	if m.name != "" {
		attrs["name"] = m.name
	} else {
		attrs["name"] = nil
	}
	for k, v := range m.attrs {
		attrs[k] = v
	}
	lines := strings.Split(string(source), "\n")
	span := contract.Span{StartLine: 1, EndLine: len(lines), EndColumn: len(lines[len(lines)-1])}
	return contract.Node{Type: "node", Identity: identity, Kind: "package", Level: 2, Name: display, QualifiedName: qualified,
		File: m.file, Span: &span, ContentHash: contract.ContentHash(source), Attributes: attrs}, nil
}

// sortedSet returns the distinct strings of list in byte order, as []any so
// the attribute is plain JSON.
func sortedSet(list []string) []any {
	seen := map[string]bool{}
	var keep []string
	for _, s := range list {
		if s != "" && !seen[s] {
			seen[s] = true
			keep = append(keep, s)
		}
	}
	sort.Strings(keep)
	out := make([]any, 0, len(keep))
	for _, s := range keep {
		out = append(out, s)
	}
	return out
}

// asWritten converts a string list to []any keeping order and duplicates.
func asWritten(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}
