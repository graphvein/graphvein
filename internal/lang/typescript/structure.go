package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds the TypeScript, TSX and JavaScript structural claim
// contract (docs/lang/typescript-structure.md, ADR 0070): the row table,
// the default-branch forms and the unread diagnostic.
// TestTypeScriptStructureContract keeps structureRows, the document's
// tables and the fixture testdata/fixtures/typescript-structure 1:1;
// TestTypeScriptStructureOracle compares the claimed facts with the
// TypeScript compiler (scripts/oracle/ts_structure).

// structureRow is one row of the TypeScript structural claim contract.
type structureRow struct {
	// ID is the row id in docs/lang/typescript-structure.md.
	ID string
	// Claim says in a few words what the row claims.
	Claim string
}

// structureRows is the Go side of the contract tables, in document order.
// "D" is the single default branch: every form no row reads.
var structureRows = []structureRow{
	{"TS-C1", "the governing configuration's options"},
	{"TS-C2", "the inferred-project options"},
	{"TS-C3", "workspace links for declared members"},
	{"TS-F1", "file record and file node"},
	{"TS-F2", "nothing beyond the file: declarations and imports go to D"},
	{"TS-F3", "the file node only"},
	{"TS-D1", "function node and contains edge"},
	{"TS-D2", "function name@overload(signature) and contains edge"},
	{"TS-D3", "default.function, default.class or default.arrow node"},
	{"TS-D4", "function node named by its binding"},
	{"TS-D5", "function node named by the expression's name"},
	{"TS-D6", "class node and contains edge"},
	{"TS-D7", "method node Class.name"},
	{"TS-D8", "method node of an object literal"},
	{"TS-D9", "object owner segment (naming only)"},
	{"TS-D10", "interface, type_alias or enum node"},
	{"TS-D11", "exported variable node"},
	{"TS-D12", "@2, @3 suffixes for repeated names"},
	{"TS-I1", "imports edge to the relative module's file"},
	{"TS-I2", "imports edge to the asset file"},
	{"TS-I3", "imports edge through paths or baseUrl"},
	{"TS-I4", "imports edge through a workspace link"},
	{"TS-I5", "imports edge to external:.#npm:<package>"},
	{"TS-I6", "imports edge to an unresolved web-root external"},
	{"TS-I7", "no edge; unresolved_import warning"},
	{"D", "nothing; in-scope forms counted in ts_structure_unread"},
}

// Default-branch forms, the keys of facts.TypeScriptFacts.Unread and of
// the ts_structure_unread diagnostic.
const (
	unreadSyntaxError        = "syntax_error"
	unreadMisparse           = "misparse"
	unreadBindingPattern     = "binding_pattern"
	unreadNamespaceMember    = "namespace_member"
	unreadConfig             = "config_unread"
	unreadDeclarationOrImpl  = "declaration_or_implementation"
	unreadWorkspaceEntry     = "workspace_entry_missing"
	unreadNotAPackage        = "not_a_package"
	unreadUnlinkedPackage    = "unlinked_package"
	unreadPathsTargetMissing = "paths_target_missing"
	unreadUndeclaredPackage  = "undeclared_package"
	unreadUnsupportedPackage = refuseUnsupportedPackage
	unreadImportAttributes   = "import_attributes"
	unreadRequireCall        = "require_call"
	unreadDynamicImport      = "dynamic_import"
	unreadImportEquals       = "import_equals"
)

// unreadForms lists every default-branch form (the contract test checks the
// document names each).
var unreadForms = []string{unreadSyntaxError, unreadMisparse, unreadBindingPattern, unreadNamespaceMember, unreadConfig, unreadDeclarationOrImpl, unreadWorkspaceEntry, unreadNotAPackage, unreadUnlinkedPackage, unreadPathsTargetMissing, unreadUndeclaredPackage, unreadUnsupportedPackage, unreadImportAttributes, unreadRequireCall, unreadDynamicImport, unreadImportEquals}

// structureTagHook is a test-only hook: every branch of the contract reports
// its row id. It is nil in production.
var structureTagHook func(row string)

func structureTag(row string) {
	if structureTagHook != nil {
		structureTagHook(row)
	}
}

// patternShadowHook is the test-only hook of row LF-3's cached-syntax
// branch (docs/extract/clients.md, "Link precision rules"); the clients
// package installs it with its own link-time rows
// (clients.SetLinkPrecisionTagHook). It is nil in production.
var patternShadowHook func(row string)

// SetPatternShadowTagHook installs (or, with nil, removes) the hook the
// destructuring shadow of row LF-3 reports to. Tests only.
func SetPatternShadowTagHook(hook func(row string)) { patternShadowHook = hook }

func patternShadowTag(row string) {
	if patternShadowHook != nil {
		patternShadowHook(row)
	}
}

// unread adds n occurrences of a default-branch form to the file's counts.
func unread(out *Facts, form string, n int) {
	if n <= 0 {
		return
	}
	structureTag("D")
	if out.TypeScript == nil {
		out.TypeScript = &facts.TypeScriptFacts{}
	}
	if out.TypeScript.Unread == nil {
		out.TypeScript.Unread = map[string]int{}
	}
	out.TypeScript.Unread[form] += n
}

// NotParsed applies row TS-F3 to a file the indexer does not parse (over
// the size limit, a parse timeout or failure, an extraction failure): the
// file claims its file node only, which the indexer builds for every
// scanned file.
func NotParsed() Facts {
	structureTag("TS-F3")
	return Facts{Exports: map[string]string{}, TypeScript: &facts.TypeScriptFacts{NotParsed: true}}
}

// Unparsed reports a file row TS-F3 refuses (NotParsed): it claims its
// file node only.
func Unparsed(f Facts) bool {
	return f.TypeScript != nil && f.TypeScript.NotParsed
}

// RefuseSyntaxError applies row TS-F2 to a file tree-sitter reports an
// error in (O-S3-1 (a), scoped by the S3 review decision B3): the file
// claims nothing structural beyond its file node. Every declaration and
// import site tree-sitter recovered goes to the default branch as one
// count. The extracted facts stay in the payload, marked Rejected, because
// the family extractors (models, table access, client calls, Angular,
// Tauri, routes) read such files as before under their own contracts;
// LinkWithResolver and the indexer withhold the structural records
// (declaration nodes, contains, imports and calls edges), and
// RejectedOwners re-anchors what the families attach to a withheld
// declaration.
func RefuseSyntaxError(f Facts) Facts {
	structureTag("TS-F2")
	n := len(f.Nodes) + len(f.Imports)
	seen := map[contract.Span]bool{}
	for _, c := range f.ExportClauses {
		if c.Module != "" && !seen[c.Span] {
			seen[c.Span] = true
			n++
		}
	}
	if f.TypeScript != nil {
		for _, c := range f.TypeScript.Unread {
			n += c
		}
	}
	if n == 0 {
		n = 1
	}
	// Such a file counts no other form.
	f.TypeScript = &facts.TypeScriptFacts{Rejected: true}
	unread(&f, unreadSyntaxError, n)
	return f
}

// Rejected reports a file row TS-F2 refuses: its structural records are
// withheld.
func Rejected(f Facts) bool {
	return f.TypeScript != nil && f.TypeScript.Rejected
}

// RejectedOwners maps every declaration identity of a file row TS-F2
// refuses to that file's node identity. The indexer emits none of these
// declarations; a family fact owned by one (a table access site, a client
// call, a route registration) is owned by the file instead, and an edge
// pointing at one is not emitted.
func RejectedOwners(files map[string]Facts) map[string]string {
	owners := map[string]string{}
	for file, f := range files {
		if !Rejected(f) {
			continue
		}
		fileID := id("file", file, file)
		for _, n := range f.Nodes {
			owners[n.Identity] = fileID
		}
	}
	return owners
}

// unreadDiagnostic reports a file's default-branch forms: one info
// diagnostic whose message ends with the count per form, sorted.
func unreadDiagnostic(file string, counts map[string]int) []contract.Diagnostic {
	total := 0
	forms := make([]string, 0, len(counts))
	for form, n := range counts {
		if n > 0 {
			forms = append(forms, form)
			total += n
		}
	}
	if total == 0 {
		return nil
	}
	sort.Strings(forms)
	parts := make([]string, len(forms))
	for i, form := range forms {
		parts[i] = fmt.Sprintf("%s=%d", form, counts[form])
	}
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "ts_structure_unread", Message: fmt.Sprintf("%d TypeScript or JavaScript declarations or imports are outside the structural claim contract and claim nothing (%s)", total, strings.Join(parts, ", "))}}
}
