package rust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds the Rust structural claim contract
// (docs/lang/rust-structure.md, ADR 0071): the row table, the default-branch
// forms and the whole-file refusals. TestRustStructureContract keeps
// structureRows, the document's tables and the fixture
// testdata/fixtures/rust-structure 1:1; TestRustStructureOracle compares the
// claimed facts with syn, cargo metadata and rustc's own resolver.

// structureRow is one row of the Rust structural claim contract.
type structureRow struct {
	// ID is the row id in docs/lang/rust-structure.md.
	ID string
	// Claim says in a few words what the row claims ("nothing" for a
	// refusal row).
	Claim string
}

// structureRows is the Rust side of the contract tables, in document order.
// "D" is the single default branch: every form no row reads.
var structureRows = []structureRow{
	{"RS-M1", "crate roots (cargo targets) and package facts of a read Cargo.toml; nothing from an unread one"},
	{"RS-M2", "imports edge to the module file of an outlined mod item"},
	{"RS-M3", "imports edge to the #[path] file of an outlined mod item"},
	{"RS-M4", "no edge; the inline module names its items and directory"},
	{"RS-M5", "nothing: a mod item inside a block goes to D"},
	{"RS-F1", "file record and file node"},
	{"RS-F2", "nothing beyond the file: a file with a syntax error goes to D"},
	{"RS-F3", "nothing beyond the file: a file with a lone CR goes to D"},
	{"RS-D1", "function node and contains edge"},
	{"RS-D2", "struct, enum, union, trait or type_alias node and contains edge"},
	{"RS-D3", "macro node and contains edge"},
	{"RS-D4", "constant or variable node with a visibility modifier"},
	{"RS-D5", "method node Type.name or Type.name@Trait"},
	{"RS-D6", "method node Trait.name contained by the trait"},
	{"RS-D7", "function node for an extern block fn"},
	{"RS-D8", "variable node for a pub extern block static"},
	{"RS-D9", "constant or type_alias node Type.NAME or Type.NAME@Trait for an impl associated const or type"},
	{"RS-D10", "node named and contained under the enclosing function for an item in a function body"},
	{"RS-D11", "nothing: macro invocations are not expanded"},
	{"RS-D12", "nothing: items in blocks outside function bodies go to D"},
	{"RS-I1", "imports edge to the file of a same-crate declaration"},
	{"RS-I2", "imports edge into another scanned crate through a declared path dependency"},
	{"RS-I3", "imports edge to external:.#cargo:<package> for a sysroot crate or declared dependency"},
	{"RS-I4", "nothing: leaves of, or resolved through imports of, an edition 2015 or unknown-edition crate go to D"},
	{"RS-I5", "no edge; unresolved_import warning"},
	{"D", "nothing; in-scope forms counted in rust_structure_unread"},
}

// Default-branch forms, the keys of facts.RustFacts.Unread and of the
// rust_structure_unread diagnostic.
const (
	unreadSyntaxError     = "syntax_error"
	unreadLoneCR          = "lone_cr"
	unreadLocalItem       = "local_item"
	unreadBlockModule     = "block_module"
	unreadImplAssoc       = "impl_assoc_item"
	unreadEdition2015     = "edition_2015_path"
	unreadEditionUnknown  = "edition_unknown_path"
	unreadExternalGlob    = "external_glob_name"
	unreadUndeclaredCrate = "undeclared_crate"
	unreadUndecidedCrate  = "undecided_crate"
	unreadExternAlias     = "extern_crate_alias"
	unreadMacroName       = "macro_name"
	unreadUnrootedCrate   = "unrooted_crate_path"
	unreadCfgModule       = "cfg_module_alternatives"
)

// unreadForms lists every default-branch form, for the contract test.
var unreadForms = []string{unreadSyntaxError, unreadLoneCR, unreadLocalItem, unreadBlockModule, unreadImplAssoc, unreadEdition2015, unreadEditionUnknown, unreadExternalGlob, unreadUndeclaredCrate, unreadUndecidedCrate, unreadExternAlias, unreadMacroName, unreadUnrootedCrate, unreadCfgModule}

// structureTagHook is a test-only hook: every branch of the contract reports
// its row id. It is nil in production.
var structureTagHook func(row string)

func structureTag(row string) {
	if structureTagHook != nil {
		structureTagHook(row)
	}
}

// Kinds of the sites structureSite reports.
const (
	siteDeclaration = "declaration" // an item that claims no node
	siteMod         = "mod"         // a mod item that claims no edge
	siteUse         = "use"         // a use leaf that claims no edge
	siteClaimedUse  = "claimed_use" // a use leaf that claims an edge
	siteFile        = "file"        // a file that claims nothing beyond its file node
)

// structureSiteHook is a test-only hook: every branch that claims nothing
// for a site (the default branch D and the refusals of a row) reports the
// site, and every claimed use leaf its declaration, so that
// TestRustStructureContract can check that the site emits nothing. It is
// nil in production.
var structureSiteHook func(kind, file string, span contract.Span)

// structureSite reports a site: its row (if any) through structureTag, and
// the site through structureSiteHook.
func structureSite(row, kind, file string, span contract.Span) {
	if row != "" {
		structureTag(row)
	}
	if structureSiteHook != nil {
		structureSiteHook(kind, file, span)
	}
}

// Refuse applies row RS-F2 (tree-sitter reports a syntax error in the
// file) or RS-F3 (loneCR: the raw bytes hold a CR not followed by LF, which
// Rust reads as white space, so a // comment runs on past it, while
// Graphvein's extractors read the scanner's text, where it is a line end).
// Rather than model either, the file claims nothing beyond its file node:
// every declaration, mod item and use declaration goes to the default branch,
// counted (lone_cr wins over syntax_error), and every other payload of the
// file (calls, Tauri, routes, queries) is dropped with them, since it rests
// on the same tree.
func Refuse(f facts.Facts, loneCR bool) facts.Facts {
	form := unreadSyntaxError
	if loneCR {
		structureTag("RS-F3")
		form = unreadLoneCR
	} else {
		structureTag("RS-F2")
	}
	n := len(f.Nodes) + len(f.Imports)
	for _, m := range f.Modules {
		if !m.Inline {
			n++
		}
	}
	if f.Rust != nil {
		for _, c := range f.Rust.Unread {
			n += c
		}
	}
	out := facts.Facts{Exports: map[string]string{}, Rust: &facts.RustFacts{LoneCR: loneCR, Rejected: !loneCR}}
	for _, d := range f.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, d)
	}
	if n > 0 {
		structureTag("D")
		out.Rust.Unread = map[string]int{form: n}
	}
	return out
}

// sameCrate reports whether c is one of the crates the file belongs to.
func (r *ModuleResolver) sameCrate(file string, c *rustCrate) bool {
	for _, m := range r.members[file] {
		if m == c {
			return true
		}
	}
	return false
}

// Unrooted reports whether a scanned Rust file is reached from no crate root
// of a read Cargo.toml (an orphan: no package, an unread manifest, or no
// `mod` chain from a target). Its declarations are claimed (rows RS-D*),
// but its imports are not resolved as a member of any crate (its outlined
// `mod` items resolve nothing, `crate::` is refused) and no cargo target
// compiles it, so no oracle configuration checks them: the indexer stores
// such a file best_effort (precision row PL-L4, reason structure_unrooted).
func (r *ModuleResolver) Unrooted(file string) bool {
	members := r.members[file]
	for _, c := range members {
		if !c.orphan {
			return false
		}
	}
	return len(members) > 0
}

// unreadDiagnostic returns the file's rust_structure_unread diagnostic: the
// count of in-scope forms that went to the default branch, per form.
func unreadDiagnostic(file string, unread map[string]int) []contract.Diagnostic {
	total := 0
	var forms []string
	for form, n := range unread {
		if n > 0 {
			total += n
			forms = append(forms, form)
		}
	}
	if total == 0 {
		return nil
	}
	sort.Strings(forms)
	parts := make([]string, len(forms))
	for i, form := range forms {
		parts[i] = fmt.Sprintf("%s=%d", form, unread[form])
	}
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "rust_structure_unread", Message: fmt.Sprintf("%d Rust declarations, modules or import leaves are outside the structural claim contract and claim nothing (%s)", total, strings.Join(parts, ", "))}}
}
