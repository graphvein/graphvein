package typescript

import (
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Config writes require stronger unconditional-execution proof than route
// registrations. Keep this annotation separate so existing route rules stay put.
func markHeyConfigConditionals(root *parse.Node, r *facts.RouteFacts) {
	hasConfig := false
	for _, call := range r.Calls {
		hasConfig = hasConfig || call.Method == "setConfig"
	}
	if !hasConfig {
		return
	}
	conditional := map[contract.Span]bool{}
	var walk func(*parse.Node, bool)
	walk = func(n *parse.Node, guarded bool) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "if_statement", "switch_statement", "ternary_expression", "for_statement", "for_in_statement", "while_statement", "do_statement", "try_statement", "catch_clause":
			guarded = true
		case "binary_expression":
			for _, token := range n.Tokens {
				if token == "&&" || token == "||" || token == "??" {
					guarded = true
				}
			}
		}
		if n.Kind == "call_expression" && guarded {
			conditional[span(n)] = true
		}
		for _, c := range n.Children {
			walk(c, guarded)
		}
	}
	walk(root, false)
	for i := range r.Calls {
		call := &r.Calls[i]
		if call.Method != "setConfig" {
			continue
		}
		call.Conditional = call.Conditional || conditional[call.Span]
	}
}
