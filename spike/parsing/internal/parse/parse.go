// Package parse adapts two tree-sitter backends for the throwaway T001 spike.
package parse

import "fmt"

// Entry keeps the spike output small enough to check by hand.
type Entry struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Node exposes only the tree operations needed by both spike implementations.
type Node interface {
	Kind() string
	Text([]byte) string
	Line() int
	Field(string) Node
	Children() []Node
	HasError() bool
	IsMissing() bool
	AllChildren() []Node
}

// Tree allows callers to release native or WASM resources after traversal.
type Tree interface {
	Root() Node
	Close()
}

// Parser separates parsing from traversal so measurements time parsing alone.
type Parser interface {
	Parse([]byte) (Tree, error)
	Close()
}

// New selects an explicitly requested backend; it never silently falls back.
func New(backend, language string) (Parser, error) {
	switch backend {
	case "wasm":
		return newWASM(language)
	case "native":
		return newNative(language)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

// Extract walks syntax nodes, including arrow-function variable declarators.
// This intentionally emits syntax-level names, not Graphvein facts.
func Extract(tree Tree, source []byte) ([]Entry, error) {
	root := tree.Root()
	if root.HasError() {
		return nil, fmt.Errorf("syntax tree contains errors")
	}
	entries := []Entry{}
	var walk func(Node)
	walk = func(n Node) {
		kind := ""
		var target Node
		switch n.Kind() {
		case "function_declaration", "function_item", "function_definition", "method_declaration":
			kind, target = "function", n.Field("name")
		case "class_declaration", "class_definition":
			kind, target = "class", n.Field("name")
		case "interface_declaration":
			kind, target = "interface", n.Field("name")
		case "type_spec":
			kind, target = "type", n.Field("name")
		case "method_definition":
			kind, target = "method", n.Field("name")
		case "decorator":
			kind, target = "decorator", n
		case "pair":
			key := n.Field("key")
			if key != nil && (key.Text(source) == "selector" || key.Text(source) == "templateUrl" || key.Text(source) == "template") {
				kind, target = "metadata", n
			}
		case "struct_item":
			kind, target = "struct", n.Field("name")
		case "function_signature": // Dart signatures are separate from bodies.
			kind, target = "function", directChild(n, "identifier")
		case "import_statement":
			kind, target = "import", n.Field("source")
		case "use_declaration":
			kind, target = "use", n.Field("argument")
		case "variable_declarator":
			value := n.Field("value")
			if value != nil && value.Kind() == "arrow_function" {
				kind, target = "function", n.Field("name")
			}
		}
		if root.Kind() == "document" {
			switch n.Kind() {
			case "start_tag", "self_closing_tag":
				kind, target = "element", directChild(n, "tag_name")
			case "structural_directive", "property_binding", "event_binding", "two_way_binding", "interpolation", "pipe_call":
				kind, target = n.Kind(), n
			case "control_keyword":
				kind, target = "control", n
			}
		}
		if target != nil {
			entries = append(entries, Entry{kind, target.Text(source), target.Line()})
		}
		for _, child := range n.Children() {
			walk(child)
		}
	}
	walk(root)
	return entries, nil
}

func directChild(n Node, kind string) Node {
	for _, child := range n.Children() {
		if child.Kind() == kind {
			return child
		}
	}
	return nil
}

// ErrorCount counts actual ERROR and inserted missing nodes, including unnamed
// punctuation. HasError on ancestors must not inflate the benchmark count.
func ErrorCount(tree Tree) int {
	count := 0
	stack := []Node{tree.Root()}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind() == "ERROR" || n.IsMissing() {
			count++
		}
		if n.HasError() {
			stack = append(stack, n.AllChildren()...)
		}
	}
	return count
}
