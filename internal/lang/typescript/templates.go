package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/parse"
)

// CachedFacts adds Angular syntax without changing the shared language facts.
type CachedFacts struct {
	Facts
	TemplateSyntax *TemplateSyntax    `json:"template_syntax"`
	Declarations   *DeclarationSyntax `json:"declarations"`
	Injections     *InjectionSyntax   `json:"injections"`
	Router         *RouterSyntax      `json:"router"`
	// Models is the gated ORM model syntax (ADR 0059); nil, and omitted,
	// for files outside the gate.
	Models *ModelSyntax `json:"models,omitempty"`
}

// TemplateSyntax is source-only metadata, independent of Angular import proof.
type TemplateSyntax struct {
	Decorators []ClassDecorator `json:"decorators"`
	Inline     []InlineTemplate `json:"inline"`
}

// ClassDecorator retains aliases and ordered metadata for link-time evaluation.
type ClassDecorator struct {
	Class          string           `json:"class"`
	Callee         facts.RouteExpr  `json:"callee"`
	Span           contract.Span    `json:"span"`
	Arguments      int              `json:"arguments"`
	ObjectMetadata bool             `json:"object_metadata"`
	Entries        []DecoratorEntry `json:"entries"`
}

// DecoratorEntry preserves duplicates, spreads and computed keys in source order.
type DecoratorEntry struct {
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	KeySpan   *contract.Span  `json:"key_span,omitempty"`
	Value     facts.RouteExpr `json:"value"`
	ValueSpan contract.Span   `json:"value_span"`
}

// InlineTemplate retains exact content coordinates or an inadmissibility reason.
type InlineTemplate struct {
	Decorator    int             `json:"decorator"`
	Span         contract.Span   `json:"span"`
	ContentSpan  *contract.Span  `json:"content_span,omitempty"`
	ContentStart int             `json:"content_start,omitempty"`
	ContentEnd   int             `json:"content_end,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Facts        *template.Facts `json:"facts,omitempty"`
}

// Valid rejects incomplete arrays and incomplete inline results on cache replay.
func (s *TemplateSyntax) Valid() bool {
	if s == nil || s.Decorators == nil || s.Inline == nil {
		return false
	}
	for _, i := range s.Inline {
		if i.Reason == "parse_failed" || i.Reason == "extract_failed" {
			return false
		}
		if i.Reason == "" && (i.ContentSpan == nil || i.Facts == nil || !i.Facts.Valid()) {
			return false
		}
	}
	return true
}

// CaptureTemplates detaches only metadata and byte ranges, never parser roots.
func CaptureTemplates(root *parse.Node, nodes []contract.Node) TemplateSyntax {
	out := TemplateSyntax{Decorators: []ClassDecorator{}, Inline: []InlineTemplate{}}
	var visit func(*parse.Node, []*parse.Node)
	visit = func(n *parse.Node, inherited []*parse.Node) {
		if n == nil {
			return
		}
		if n.Kind == "export_statement" {
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					inherited = append(inherited, c)
				}
			}
			for _, c := range n.Children {
				if c.Kind != "decorator" {
					visit(c, inherited)
				}
			}
			return
		}
		if n.Kind == "class_declaration" || n.Kind == "abstract_class_declaration" || n.Kind == "class" {
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					inherited = append(inherited, c)
				}
			}
			identity := ""
			for _, node := range nodes {
				if node.Kind == "class" && node.Span != nil && node.Span.EndLine == int(n.EndRow)+1 && node.Span.EndColumn == int(n.EndColumn) {
					identity = node.Identity
					break
				}
			}
			for _, d := range inherited {
				var call *parse.Node
				for _, c := range d.Children {
					if c.Kind == "call_expression" {
						call = c
						break
					}
				}
				if call == nil {
					continue
				}
				callee := call.Fields["function"]
				if !decoratorCallee(callee) {
					continue
				}
				cs := urlExpr(callee)
				cs.Text = compact(callee.Text)
				s := span(callee)
				cs.Span = &s
				meta := ClassDecorator{Class: identity, Callee: cs, Span: span(d), Entries: []DecoratorEntry{}}
				var args []*parse.Node
				if a := call.Fields["arguments"]; a != nil {
					for _, c := range a.Children {
						if c.Kind != "comment" {
							args = append(args, c)
						}
					}
				}
				meta.Arguments = len(args)
				index := len(out.Decorators)
				if len(args) > 0 {
					obj := unwrapMetadata(args[0])
					if obj != nil && obj.Kind == "object" {
						meta.ObjectMetadata = true
						for _, p := range obj.Children {
							if p.Kind == "comment" {
								continue
							}
							key, value := p.Fields["key"], p.Fields["value"]
							if p.Kind == "method_definition" {
								key = p.Fields["name"]
							}
							if p.Kind == "shorthand_property_identifier" {
								key = p
								identifier := *p
								identifier.Kind = "identifier"
								value = &identifier
							}
							entry := DecoratorEntry{Kind: p.Kind}
							if key != nil {
								entry.Key = key.Text
								keySpan := span(key)
								entry.KeySpan = &keySpan
								if key.Kind == "string" {
									entry.Key, _ = jsString(key.Text)
								}
								if key.Kind == "computed_property_name" {
									entry.Kind = "computed"
								}
							}
							if value == nil {
								value = p
							}
							entry.Value = urlExpr(value)
							entry.ValueSpan = span(value)
							entry.Value.Text = value.Text
							entry.Value.Span = &entry.ValueSpan
							meta.Entries = append(meta.Entries, entry)
							if p.Kind == "spread_element" || entry.Kind == "computed" {
								out.Inline = append(out.Inline, InlineTemplate{Decorator: index, Span: span(p), Reason: "metadata_" + entry.Kind})
								continue
							}
							if entry.Key != "template" {
								continue
							}
							in := InlineTemplate{Decorator: index, Span: span(value)}
							if value.Kind != "string" && value.Kind != "template_string" {
								in.Reason = "nonliteral"
							} else if strings.Contains(value.Text, "\\") {
								in.Reason = "escape"
							} else {
								for _, c := range value.Children {
									if c.Kind == "template_substitution" {
										in.Reason = "substitution"
									}
								}
								if in.Reason == "" {
									in.ContentStart = int(value.Start) + 1
									in.ContentEnd = int(value.End) - 1
									contentSpan := span(value)
									in.ContentSpan = &contentSpan
									in.ContentSpan.StartColumn++
									in.ContentSpan.EndColumn--
								}
							}
							out.Inline = append(out.Inline, in)
						}
					}
				}
				out.Decorators = append(out.Decorators, meta)
			}
			for _, c := range n.Children {
				if c.Kind != "decorator" {
					visit(c, nil)
				}
			}
			return
		}
		for _, c := range n.Children {
			visit(c, nil)
		}
	}
	visit(root, nil)
	return out
}

func decoratorCallee(n *parse.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == "identifier" {
		return true
	}
	return n.Kind == "member_expression" && n.Fields["property"] != nil && n.Fields["property"].Kind == "property_identifier" && decoratorCallee(n.Fields["object"])
}
func unwrapMetadata(n *parse.Node) *parse.Node {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "type_assertion", "non_null_expression":
			var next *parse.Node
			for _, c := range n.Children {
				if !strings.Contains(c.Kind, "type") && c.Kind != "comment" {
					next = c
					break
				}
			}
			n = next
		default:
			return n
		}
	}
	return nil
}
