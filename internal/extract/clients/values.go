package clients

import (
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// MetadataValue exposes an existing bounded evaluation without changing its rules.
type MetadataValue struct {
	Kind, Text string
	Known      bool
}

// EvaluateMetadata shares the URL evaluator's constants, imports and budget.
func EvaluateMetadata(files map[string]facts.Facts, modules *typescript.ModuleResolver, file string, scope int, expression facts.RouteExpr) MetadataValue {
	r := &resolver{files: files, modules: modules, memo: map[string]memoValue{}}
	values := newEvaluation(r).expression(evaluationContext{file: file, scope: scope}, expression)
	if len(values) != 1 {
		return MetadataValue{}
	}
	v := values[0]
	return MetadataValue{Kind: v.kind, Text: v.text, Known: v.known()}
}

// ExclusionSource exposes the existing absent-versus-excluded diagnostic lookup.
func ExclusionSource(file string, sources map[string][]byte) string {
	return generatedIgnoreSource(file, sources)
}
