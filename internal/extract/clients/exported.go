package clients

import (
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// The helpers in this file expose existing resolver, binding and evaluation
// code to other link-time consumers (Tauri invoke linking, ADR 0038) without
// changing their rules: a site resolves and evaluates exactly as a client call
// would.

// Value is one evaluated alternative of an expression. Known is false for a
// dynamic or partly unknown value; Conditional marks an alternative chosen by
// a runtime condition (a ternary branch, for example).
type Value struct {
	Kind, Text  string
	Known       bool
	Conditional bool
}

// Evaluate runs the bounded URL evaluator (ADR 0020) on expression at the
// site's URLScope and returns every alternative it keeps, in evaluator order.
func Evaluate(files map[string]facts.Facts, modules *typescript.ModuleResolver, file string, scope int, expression facts.RouteExpr) []Value {
	r := &resolver{files: files, modules: modules, memo: map[string]memoValue{}}
	var out []Value
	for _, v := range newEvaluation(r).expression(evaluationContext{file: file, scope: scope}, expression) {
		out = append(out, Value{Kind: v.kind, Text: v.text, Known: v.known(), Conditional: v.conditional})
	}
	return out
}

// Binding returns the nearest lexical binding of name visible from a site's
// Scope, the scope index that declares it, and whether one exists. It is the
// lookup client recognition uses for callees and wrapper parameters.
func Binding(files map[string]facts.Facts, file string, scope int, name string) (facts.RouteBinding, int, bool) {
	r := &resolver{files: files}
	return r.binding(file, scope, name)
}

// Compact removes whitespace and comments outside literals, as client_call
// URL expressions do, so identities built from argument text stay stable
// across formatting changes.
func Compact(s string) string { return compact(s) }
