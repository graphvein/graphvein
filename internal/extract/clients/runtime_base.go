package clients

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// The call side of the unknown-prefix contract (task T033, rows
// UP-39 and UP-40 of docs/contract/claims/unknown-prefix-paths.md, copied
// in docs/extract/clients.md). A frontend often builds every request URL
// through one helper that prepends a base read from runtime configuration,
// as dozzle's
//
//	export const withBase = (path: string) => `${config.base}${path}`;
//
// The bounded evaluator cannot read config.base, so such a call used to be
// dynamic. When the helper is proven to prepend exactly one base value to
// its argument (a closed list of shapes, row UP-39), the call is read as an
// unresolved base followed by the argument's path, like a call through an
// environment base; the client matcher's unknown-prefix tier may then link
// it (row UP-15). Every other helper is evaluated as before.

// runtimeBaseKind marks the leading URL piece that stands for the base a
// proven helper prepends. It only arises from evaluation; template reads it
// as an unresolved base whose expression is the base's spelling.
const runtimeBaseKind = "runtime_base"

// runtimeBase applies rows UP-39 and UP-40 to one call of helper h. c is
// the helper's own module (its binding is resolved there), caller the call
// site, moduleLevel whether the helper is bound at its module's top level.
// It returns the call's values and true when UP-39 reads the call; with
// false the caller evaluates the helper as before, so no existing reading
// changes.
func (ev *evaluation) runtimeBase(caller, c evaluationContext, e expr, h *facts.URLHelper, moduleLevel bool) (valueSet, bool) {
	base, parameter, ok := runtimeBaseShape(h)
	if !ok {
		// Not a two-piece concatenation ending in the parameter: the
		// ordinary helper evaluation.
		upCallTag("D")
		return nil, false
	}
	if !moduleLevel || len(e.Args) != 1 || e.Args[0].Kind == "spread_element" {
		// A helper inside a function (its base may be a closure value) or a
		// call that does not pass exactly one argument.
		upCallTag("UP-40")
		return nil, false
	}
	// The checks below evaluate; a refusal restores the evaluation's
	// state so the ordinary evaluation runs exactly as before.
	restore := ev.snapshot()
	helper := evaluationContext{file: c.file, scope: h.Scope, env: &environment{values: map[string]localValue{}}}
	if !ev.runtimeBaseValue(helper, base, parameter) {
		restore()
		upCallTag("UP-40")
		return nil, false
	}
	values := ev.expression(helper, base)
	if len(values) != 1 || values[0].kind != "dynamic" || ev.exhausted {
		// The evaluator reads the base (a known value or several
		// alternatives), or ran out of steps: the ordinary helper
		// evaluation keeps its reading.
		restore()
		upCallTag("UP-40")
		return nil, false
	}
	args := ev.expression(caller, e.Args[0])
	for _, v := range args {
		if v.kind != "string" || !leadingPath(v.pieces) || ev.exhausted {
			// An argument the evaluator does not read as a string starting
			// with a literal "/": the call stays as the evaluator reads it.
			restore()
			upCallTag("UP-40")
			return nil, false
		}
	}
	upCallTag("UP-39")
	ev.evaluated = true
	ev.helpers[h.Identity] = true
	ev.addEvidence(c.file, &h.Body[0].Span)
	// Cached helper bodies keep no source text: the spelling is rebuilt.
	leading := piece{expression: expr{Kind: runtimeBaseKind, Text: spelling(base)}}
	out := make(valueSet, 0, len(args))
	for _, v := range args {
		out = append(out, urlValue{kind: "string", pieces: append([]piece{leading}, v.pieces...), conditional: v.conditional})
	}
	return out, true
}

// snapshot records the evaluation's counters, flags and collected helpers
// and evidence; the returned function puts them back.
func (ev *evaluation) snapshot() func() {
	steps, aliases := ev.steps, ev.urlAliases
	evaluated, constant, exhausted := ev.evaluated, ev.constant, ev.exhausted
	helpers := make(map[string]bool, len(ev.helpers))
	for k, v := range ev.helpers {
		helpers[k] = v
	}
	evidence := make(map[string]contract.Evidence, len(ev.evidence))
	for k, v := range ev.evidence {
		evidence[k] = v
	}
	return func() {
		ev.steps, ev.urlAliases = steps, aliases
		ev.evaluated, ev.constant, ev.exhausted = evaluated, constant, exhausted
		ev.helpers, ev.evidence = helpers, evidence
	}
}

// runtimeBaseShape reports whether h is `(p) => X + p`, `(p) => `${X}${p}“,
// or a function whose body is exactly `return` of one of them: one plain
// parameter without a default, the parameter used once as the last piece,
// nothing else in the body. It returns X and the parameter's name.
func runtimeBaseShape(h *facts.URLHelper) (expr, string, bool) {
	if len(h.Parameters) != 1 || h.Parameters[0].Name == "" || h.Parameters[0].Default.Kind != "" || len(h.Body) != 1 || h.Body[0].Kind != "return_statement" {
		return expr{}, "", false
	}
	name := h.Parameters[0].Name
	e := h.Body[0].Expression
	switch {
	case e.Kind == "template_string" && !e.Invalid && len(e.Parts) == 2:
	case e.Kind == "binary_expression" && e.Operator == "+" && len(e.Parts) == 2:
	default:
		return expr{}, "", false
	}
	last := e.Parts[1]
	if last.Kind != "identifier" || last.Name != name {
		return expr{}, "", false
	}
	return e.Parts[0], name, true
}

// runtimeBaseValue reports whether base is one of UP-39's base forms in the
// helper's module: a member read `O.k` of a module-level const, or a
// module-level const itself, declared once in the helper's own module (not
// imported), never written to, and whose value the evaluator does not read.
// A parameter, a let or var, a call, an imported value or a value written
// elsewhere is refused (row UP-40).
func (ev *evaluation) runtimeBaseValue(helper evaluationContext, base expr, parameter string) bool {
	target := base
	if base.Kind == "member_expression" {
		if base.Object == nil || base.Object.Kind != "identifier" || base.Name == "" {
			return false
		}
		target = *base.Object
	}
	if target.Kind != "identifier" || target.Name == parameter {
		return false
	}
	b, at, ok, _ := ev.binding(helper, target.Name)
	if !ok || at != 0 || b.Module != "" {
		return false
	}
	v := b.Evaluation
	if v == nil || !v.Constant || !v.Single || v.Mutated || v.Helper != nil {
		return false
	}
	// The const itself must be a value the evaluator cannot read: a known
	// object without the property would be undefined at run time, not a
	// configured base.
	values := ev.expression(evaluationContext{file: helper.file, env: &environment{values: map[string]localValue{}}}, target)
	return len(values) == 1 && values[0].kind == "dynamic"
}

// leadingPath reports whether pieces start, after empty literal text, with
// literal text beginning with "/" (a path below the base).
func leadingPath(pieces []piece) bool {
	for _, p := range pieces {
		if !p.literal {
			return false
		}
		if p.text != "" {
			return strings.HasPrefix(p.text, "/")
		}
	}
	return false
}

var upCallTagHook func(row string)

// SetUnknownPrefixCallTagHook installs (or, with nil, removes) the hook
// every branch of the call-side rows reports its row to (rows UP-39, UP-40
// and D, UnknownPrefixCallRows). Tests only; linking is single-threaded.
func SetUnknownPrefixCallTagHook(hook func(row string)) {
	upCallTagHook = hook
}

func upCallTag(row string) {
	if upCallTagHook != nil {
		upCallTagHook(row)
	}
}
