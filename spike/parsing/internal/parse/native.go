//go:build native && cgo

package parse

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type nativeParser struct{ parser *sitter.Parser }
type nativeTree struct{ tree *sitter.Tree }
type nativeNode struct{ node *sitter.Node }

func newNative(language string) (Parser, error) {
	var grammar *sitter.Language
	switch language {
	case "tsx":
		grammar = sitter.NewLanguage(ts.LanguageTSX())
	case "rust":
		grammar = sitter.NewLanguage(rust.Language())
	default:
		return nil, fmt.Errorf("unsupported language %q", language)
	}
	p := sitter.NewParser()
	if err := p.SetLanguage(grammar); err != nil {
		p.Close()
		return nil, fmt.Errorf("loading %s native parser: %w", language, err)
	}
	return &nativeParser{p}, nil
}
func (p *nativeParser) Parse(source []byte) (Tree, error) {
	t := p.parser.Parse(source, nil)
	if t == nil {
		return nil, fmt.Errorf("native parser returned no tree")
	}
	return &nativeTree{t}, nil
}
func (p *nativeParser) Close()                 { p.parser.Close() }
func (t *nativeTree) Root() Node               { return nativeNode{t.tree.RootNode()} }
func (t *nativeTree) Close()                   { t.tree.Close() }
func (n nativeNode) Kind() string              { return n.node.Kind() }
func (n nativeNode) Text(source []byte) string { return n.node.Utf8Text(source) }
func (n nativeNode) Line() int                 { return int(n.node.StartPosition().Row) + 1 }
func (n nativeNode) HasError() bool            { return n.node.HasError() }
func (n nativeNode) IsMissing() bool           { return n.node.IsMissing() }
func (n nativeNode) AllChildren() []Node {
	children := make([]Node, n.node.ChildCount())
	for i := range children {
		children[i] = nativeNode{n.node.Child(uint(i))}
	}
	return children
}
func (n nativeNode) Field(name string) Node {
	child := n.node.ChildByFieldName(name)
	if child == nil {
		return nil
	}
	return nativeNode{child}
}
func (n nativeNode) Children() []Node {
	children := make([]Node, n.node.NamedChildCount())
	for i := range children {
		children[i] = nativeNode{n.node.NamedChild(uint(i))}
	}
	return children
}
