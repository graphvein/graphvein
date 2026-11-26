package python

import "github.com/graphvein/graphvein/internal/lang/facts"

// Prefix returns only source-proven literal defaults; settings defaults remain
// environment-overridable and never evaluate validators or environment files.
func (r *ModuleResolver) Prefix(file string, scope int, e facts.RouteExpr) (string, string, bool) {
	if e.Kind == "literal" {
		return e.Name, "literal", true
	}
	if e.Kind == "identifier" || e.Kind == "attribute" {
		b := r.Expression(file, scope, e)
		if !b.ViaAssignment && b.File != "" && !b.External && b.Scope == 0 && b.Syntax.Count == 1 && b.Syntax.Stable && b.Syntax.Value.Kind == "literal" {
			return b.Syntax.Value.Name, "constant", true
		}
	}
	if e.Kind != "attribute" || e.Object == nil {
		return "", "", false
	}
	b := r.Expression(file, scope, *e.Object)
	if b.ViaAssignment || b.External || b.File == "" || b.Scope != 0 || !b.Syntax.Stable || b.Syntax.Count != 1 {
		return "", "", false
	}
	call := b.Syntax.Value
	if call.Kind != "call" || call.Object == nil || len(call.Args) != 0 {
		return "", "", false
	}
	for _, kw := range call.Keywords {
		if kw.Name == e.Name || kw.Name == "" {
			return "", "", false
		}
	}
	class := r.Expression(b.File, b.Scope, *call.Object)
	if class.Syntax.Value.Kind != "class_definition" {
		return "", "", false
	}
	settings := false
	for _, base := range class.Syntax.Bases {
		resolved := r.Expression(class.File, class.Scope, base)
		if resolved.External && (resolved.File == "pydantic_settings" || resolved.File == "pydantic") && resolved.Name == "BaseSettings" {
			settings = true
		}
	}
	if !settings {
		return "", "", false
	}
	p := r.files[class.File].Python
	if p == nil || class.Syntax.ClassScope >= len(p.Scopes) {
		return "", "", false
	}
	attr, ok := p.Scopes[class.Syntax.ClassScope].Bindings[e.Name]
	if !ok || !attr.Stable || attr.Count != 1 || attr.Value.Kind != "literal" {
		return "", "", false
	}
	return attr.Value.Name, "settings_default", true
}
