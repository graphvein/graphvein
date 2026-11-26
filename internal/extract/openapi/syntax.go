// Package openapi imports declared API operations from scanner-retained sources.
// It never reads files or fetches references, and retains no cross-pass cache.
package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"go.yaml.in/yaml/v3"
)

type value struct {
	kind   byte // m mapping, s sequence, v scalar
	scalar any
	// scalarText preserves numeric spelling for version checks without making
	// numbers act like strings in other OpenAPI fields.
	scalarText string
	entries    []entry
	items      []*value
	// An aliased mapping supplies values but no lexical keys at its use site.
	// Propagate the occurrence region through descendants instead of reusing
	// the anchor's source positions.
	region *sourceRegion
}
type sourceRegion struct{ start, end int }
type entry struct {
	key        string
	value      *value
	start, end int
	merge      bool
}

func (v *value) fields() []entry { return fields(v, map[*value]bool{}) }
func fields(v *value, seen map[*value]bool) []entry {
	if v == nil || v.kind != 'm' || seen[v] {
		return nil
	}
	seen[v] = true
	defer delete(seen, v)
	var out []entry
	own := map[string]bool{}
	for _, e := range v.entries {
		if !e.merge {
			out = append(out, e)
			own[e.key] = true
		}
	}
	for _, merge := range v.entries {
		if !merge.merge {
			continue
		}
		vs := []*value{merge.value}
		if merge.value.kind == 's' {
			vs = merge.value.items
		}
		for _, source := range vs {
			for _, e := range fields(source, seen) {
				if !own[e.key] {
					e.start, e.end = merge.start, merge.end
					out = append(out, e)
					own[e.key] = true
				}
			}
		}
	}
	if v.region != nil {
		for i := range out {
			out[i].start, out[i].end = v.region.start, v.region.end
			child := *out[i].value
			child.region = v.region
			out[i].value = &child
		}
	}
	return out
}
func (v *value) get(key string) *value { e, _ := v.field(key); return e.value }
func (v *value) field(key string) (entry, bool) {
	for _, e := range v.fields() {
		if e.key == key {
			return e, true
		}
	}
	return entry{}, false
}
func (v *value) text() string {
	if v == nil {
		return ""
	}
	s, _ := v.scalar.(string)
	return s
}
func (v *value) versionText() string {
	if v == nil || v.kind != 'v' {
		return ""
	}
	return v.scalarText
}
func (v *value) nullable() any {
	if v != nil {
		if s, ok := v.scalar.(string); ok {
			return s
		}
	}
	return nil
}
func (v *value) boolean() bool {
	if v == nil {
		return false
	}
	b, _ := v.scalar.(bool)
	return b
}
func (v *value) list() []*value {
	if v == nil {
		return nil
	}
	return v.items
}

func parseSource(source []byte, isJSON bool) (*value, error) {
	if isJSON {
		d := json.NewDecoder(bytes.NewReader(source))
		d.UseNumber()
		v, err := jsonValue(d, source)
		if err != nil {
			return nil, err
		}
		if _, err := d.Token(); err != io.EOF {
			if err != nil {
				return nil, fmt.Errorf("after JSON document: %w", err)
			}
			return nil, fmt.Errorf("found additional JSON content; expected one document")
		}
		return v, nil
	}
	d := yaml.NewDecoder(bytes.NewReader(source))
	var doc yaml.Node
	if err := d.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("missing YAML document root")
		}
		return nil, err
	}
	count := 1
	for {
		var extra yaml.Node
		if err := d.Decode(&extra); err != nil {
			if err != io.EOF {
				return nil, fmt.Errorf("after YAML document %d: %w", count, err)
			}
			break
		}
		count++
	}
	if count != 1 {
		return nil, fmt.Errorf("found %d YAML documents; expected one", count)
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("expected a document root")
	}
	return yamlValue(doc.Content[0], len(source), source, map[*yaml.Node]*value{})
}

func jsonValue(d *json.Decoder, source []byte) (*value, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	v := &value{kind: 'v', scalar: t}
	switch scalar := t.(type) {
	case string:
		v.scalarText = scalar
	case json.Number:
		v.scalarText = string(scalar)
	}
	if t == json.Delim('{') {
		v.kind = 'm'
		seen := map[string]bool{}
		for d.More() {
			start := int(d.InputOffset())
			for start < len(source) && (strings.ContainsRune(" \t\n\r,", rune(source[start]))) {
				start++
			}
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			s, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if seen[s] {
				return nil, fmt.Errorf("duplicate key %q at byte %d", s, start)
			}
			seen[s] = true
			child, err := jsonValue(d, source)
			if err != nil {
				return nil, err
			}
			v.entries = append(v.entries, entry{key: s, value: child, start: start, end: int(d.InputOffset())})
		}
		_, err = d.Token()
	} else if t == json.Delim('[') {
		v.kind = 's'
		for d.More() {
			child, e := jsonValue(d, source)
			if e != nil {
				return nil, e
			}
			v.items = append(v.items, child)
		}
		_, err = d.Token()
	}
	return v, err
}

func yamlOffset(n *yaml.Node, source []byte) int {
	line, offset := 1, 0
	for offset < len(source) && line < n.Line {
		if source[offset] == '\n' {
			line++
		}
		offset++
	}
	for col := 1; offset < len(source) && col < n.Column; col++ {
		_, size := utf8.DecodeRune(source[offset:])
		offset += size
	}
	return offset
}
func yamlEnd(source []byte, boundary int, flow bool) int {
	for boundary > 0 && strings.ContainsRune(" \t\r\n", rune(source[boundary-1])) {
		boundary--
	}
	if flow && boundary > 0 && source[boundary-1] == ',' {
		boundary--
		for boundary > 0 && strings.ContainsRune(" \t\r\n", rune(source[boundary-1])) {
			boundary--
		}
	}
	return boundary
}
func yamlValue(n *yaml.Node, boundary int, source []byte, memo map[*yaml.Node]*value) (*value, error) {
	if v := memo[n]; v != nil {
		return v, nil
	}
	if n.Kind == yaml.AliasNode {
		target, err := yamlValue(n.Alias, boundary, source, memo)
		if err != nil {
			return nil, err
		}
		alias := *target
		// A comma after an alias can only delimit its enclosing flow mapping;
		// it is never part of the alias token itself.
		alias.region = &sourceRegion{yamlOffset(n, source), yamlEnd(source, boundary, true)}
		return &alias, nil
	}
	v := &value{kind: 'v'}
	memo[n] = v
	switch n.Kind {
	case yaml.MappingNode:
		v.kind = 'm'
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k, child := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: complex mapping key", k.Line)
			}
			if seen[k.Value] {
				return nil, fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			seen[k.Value] = true
			end := boundary
			if i+2 < len(n.Content) {
				end = yamlOffset(n.Content[i+2], source)
			}
			cv, err := yamlValue(child, end, source, memo)
			if err != nil {
				return nil, err
			}
			v.entries = append(v.entries, entry{key: k.Value, value: cv, start: yamlOffset(k, source), end: yamlEnd(source, end, n.Style&yaml.FlowStyle != 0), merge: k.Tag == "!!merge"})
		}
	case yaml.SequenceNode:
		v.kind = 's'
		for i, child := range n.Content {
			end := boundary
			if i+1 < len(n.Content) {
				end = yamlOffset(n.Content[i+1], source)
			}
			cv, err := yamlValue(child, end, source, memo)
			if err != nil {
				return nil, err
			}
			v.items = append(v.items, cv)
		}
	case yaml.ScalarNode:
		if n.Tag != "!!null" {
			v.scalarText = n.Value
		}
		if n.Tag == "!!str" {
			v.scalar = n.Value
		} else if n.Tag == "!!bool" {
			var b bool
			if err := n.Decode(&b); err != nil {
				return nil, err
			}
			v.scalar = b
		}
	}
	return v, nil
}

func span(source []byte, start, end int) contract.Span {
	position := func(offset int) (int, int) {
		before := source[:offset]
		line := bytes.Count(before, []byte{'\n'}) + 1
		return line, offset - bytes.LastIndexByte(before, '\n') - 1
	}
	sl, sc := position(start)
	el, ec := position(end)
	return contract.Span{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
}
