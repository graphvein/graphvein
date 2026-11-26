// Package contracttest holds the test helpers that keep a best-effort
// language's claim contract and its code one to one (T021, ADR 0067). Only
// tests import it, so go/parser stays out of the graphvein binary.
package contracttest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
)

// RowLiterals returns the row ids that the Go files at paths spell as
// string literals: "D", or the language prefix or "BE" followed by "-" and
// a row number ("CS-I2", "BE-8"). Branches name their row through such a
// literal (a trace call or a returned row), so the contract tests compare
// these with the Go table. The files are parsed, so a row id in a comment
// does not count as a branch; that a branch is taken is what the fixture
// coverage tests check through the tracer.
func RowLiterals(paths []string, prefix string) (map[string]bool, error) {
	row := regexp.MustCompile(`^(?:` + regexp.QuoteMeta(prefix) + `|BE)-[A-Z0-9]+$|^D$`)
	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(lit.Value); err == nil && row.MatchString(value) {
				found[value] = true
			}
			return true
		})
	}
	return found, nil
}
