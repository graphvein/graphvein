package clients

import "github.com/graphvein/graphvein/internal/lang/typescript"

// LinkPrecisionRows are the rows of the link precision table
// (docs/extract/clients.md, "Link precision rules") in table order; LF-U
// is the single default branch and comes last. Every branch that decides
// one of these rows reports it through linkTag (or, for the cached
// TypeScript syntax of LF-3, the typescript package's pattern shadow
// hook), so TestLinkPrecisionRules can check that the code, the table and
// the fixture stay 1:1.
var LinkPrecisionRows = []string{"LF-1", "LF-2", "LF-3", "LF-U"}

var linkTagHook func(row string)

// SetLinkPrecisionTagHook installs (or, with nil, removes) the hook every
// link precision branch reports its row to, the cached-syntax branch of
// LF-3 included. Tests only; linking is single-threaded.
func SetLinkPrecisionTagHook(hook func(row string)) {
	linkTagHook = hook
	typescript.SetPatternShadowTagHook(hook)
}

func linkTag(row string) {
	if linkTagHook != nil {
		linkTagHook(row)
	}
}
