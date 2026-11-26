package parse

import (
	"fmt"
	"strings"
)

// InlineAngular prototypes line mapping for literal Component templates. It
// rejects escapes and JS substitutions rather than inventing source coordinates.
// Template failures are returned independently so later components still parse.
func InlineAngular(source []byte) ([]Entry, []error) {
	entries := []Entry{}
	diagnostics := []error{}
	ts, err := New("wasm", "typescript")
	if err != nil {
		return entries, []error{err}
	}
	defer ts.Close()
	tree, err := ts.Parse(source)
	if err != nil {
		return entries, []error{err}
	}
	defer tree.Close()
	if tree.Root().HasError() {
		return entries, []error{fmt.Errorf("inline TypeScript contains syntax errors")}
	}
	angular, err := New("wasm", "angular")
	if err != nil {
		return entries, []error{err}
	}
	defer angular.Close()
	parseTemplate := func(value Node) {
		text := value.Text(source)
		if (value.Kind() != "string" && value.Kind() != "template_string") || len(text) < 2 || strings.Contains(text, "\\") || strings.Contains(text, "${") {
			diagnostics = append(diagnostics, fmt.Errorf("template at line %d: only unescaped literals are supported", value.Line()))
			return
		}
		body := []byte(text[1 : len(text)-1])
		parsed, err := angular.Parse(body)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Errorf("template at line %d: %w", value.Line(), err))
			return
		}
		defer parsed.Close()
		items, err := Extract(parsed, body)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Errorf("template at line %d: %w", value.Line(), err))
			return
		}
		for _, item := range items {
			item.Line += value.Line() - 1
			entries = append(entries, item)
		}
	}
	var walk func(Node)
	walk = func(n Node) {
		if n.Kind() == "decorator" {
			// Require the actual Component call's first object argument. Arbitrary
			// template properties and other decorators are outside this prototype.
			for _, call := range n.Children() {
				function := call.Field("function")
				if call.Kind() != "call_expression" || function == nil || function.Text(source) != "Component" {
					continue
				}
				args := call.Field("arguments")
				if args == nil || len(args.Children()) == 0 || args.Children()[0].Kind() != "object" {
					continue
				}
				for _, pair := range args.Children()[0].Children() {
					key, value := pair.Field("key"), pair.Field("value")
					if pair.Kind() == "pair" && key != nil && value != nil && strings.Trim(key.Text(source), "\"'") == "template" {
						parseTemplate(value)
					}
				}
			}
			return
		}
		for _, child := range n.Children() {
			walk(child)
		}
	}
	walk(tree.Root())
	return entries, diagnostics
}
