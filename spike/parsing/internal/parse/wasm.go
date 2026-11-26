//go:build !native

package parse

import (
	"context"
	"fmt"

	"github.com/zema1/wasitter"
	"graphvein/spike/parsing/grammars"
)

type wasmParser struct {
	parser  *wasitter.Parser
	runtime *wasitter.Runtime
}
type wasmTree struct{ tree *wasitter.Tree }
type wasmNode struct{ node wasitter.Node }

func newWASM(language string) (Parser, error) {
	data, err := grammars.Load(language)
	if err != nil {
		return nil, err
	}
	p, r, err := wasitter.NewParserFromWASM(context.Background(), data)
	if err != nil {
		return nil, fmt.Errorf("loading %s WASM parser: %w", language, err)
	}
	return &wasmParser{p, r}, nil
}
func (p *wasmParser) Parse(source []byte) (Tree, error) {
	t, err := p.parser.ParseContext(context.Background(), source, nil)
	if err != nil {
		return nil, fmt.Errorf("WASM parsing: %w", err)
	}
	return &wasmTree{t}, nil
}
func (p *wasmParser) Close()                 { _ = p.parser.Close(); _ = p.runtime.Close() }
func (p *wasmParser) ABIVersion() uint32     { return p.parser.Language().ABIVersion() }
func (t *wasmTree) Root() Node               { return wasmNode{t.tree.RootNode()} }
func (t *wasmTree) Close()                   { _ = t.tree.Close() }
func (n wasmNode) Kind() string              { return n.node.Type() }
func (n wasmNode) Text(source []byte) string { return n.node.Content(source) }
func (n wasmNode) Line() int                 { return int(n.node.StartPoint().Row) + 1 }
func (n wasmNode) HasError() bool            { return n.node.HasError() }
func (n wasmNode) IsMissing() bool           { return n.node.IsMissing() }
func (n wasmNode) AllChildren() []Node {
	children := make([]Node, n.node.ChildCount())
	for i := range children {
		children[i] = wasmNode{n.node.Child(i)}
	}
	return children
}
func (n wasmNode) Field(name string) Node {
	child := n.node.ChildByFieldName(name)
	if child.IsNull() {
		return nil
	}
	return wasmNode{child}
}
func (n wasmNode) Children() []Node {
	children := make([]Node, n.node.NamedChildCount())
	for i := range children {
		children[i] = wasmNode{n.node.NamedChild(i)}
	}
	return children
}
