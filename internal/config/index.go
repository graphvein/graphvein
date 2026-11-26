package config

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Index holds settings that change what an index run links, not what it
// parses. Changing one needs a re-index (a re-link over the fact cache),
// never a re-parse.
type Index struct {
	// ShapeDepth bounds how many proven calls the route shape extractors
	// follow from a handler (rows SM-21 and SM-23 of
	// docs/contract/claims/route-shape.md). It equals
	// routeshape.DefaultDepth unless graphvein.yaml or the --shape-depth
	// flag of graphvein index sets it; a test asserts the defaults agree.
	ShapeDepth int
}

// Shape depth bounds. A deeper chain is cut with the stop reason
// depth_cap, never dropped silently. The values equal routeshape's
// DefaultDepth, MinDepth and MaxDepthLimit (config does not import it; a
// test there asserts they agree).
const (
	DefaultShapeDepth = 6
	MinShapeDepth     = 1
	MaxShapeDepth     = 10
)

// DefaultIndex is the index section of a graphvein.yaml without one.
func DefaultIndex() Index {
	return Index{ShapeDepth: DefaultShapeDepth}
}

// decodeIndex accepts `index: {shape_depth: N}`. Unknown keys, duplicate
// keys, non-integers and values out of range are errors, like every other
// section.
func decodeIndex(n *yaml.Node) (Index, error) {
	c := DefaultIndex()
	if n.Kind != yaml.MappingNode {
		return c, fmt.Errorf("index must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return c, fmt.Errorf("line %d: index keys must be strings", k.Line)
		}
		if seen[k.Value] {
			return c, fmt.Errorf("line %d: duplicate index key %s", k.Line, k.Value)
		}
		seen[k.Value] = true
		if k.Value != "shape_depth" {
			return c, fmt.Errorf("line %d: unknown index key %s", k.Line, k.Value)
		}
		var depth int
		if v.Kind != yaml.ScalarNode || v.Tag != "!!int" || v.Decode(&depth) != nil || depth < MinShapeDepth || depth > MaxShapeDepth {
			return c, fmt.Errorf("line %d: index.shape_depth must be an integer from %d to %d", v.Line, MinShapeDepth, MaxShapeDepth)
		}
		c.ShapeDepth = depth
	}
	return c, nil
}
