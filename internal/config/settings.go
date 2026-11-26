package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Kinds of a Settings key: how its value is typed in JSON and in YAML.
const (
	// SettingEnum is a string from Choices.
	SettingEnum = "enum"
	// SettingInteger is a whole number from Min to Max.
	SettingInteger = "integer"
	// SettingBoolean is true or false.
	SettingBoolean = "boolean"
	// SettingList is a list of strings (patterns).
	SettingList = "list"
	// SettingMap is an ordered list of {key, value} string pairs (a
	// directory or file mapped to a base path).
	SettingMap = "map"
)

// When a change of a Settings key takes effect (ADR 0088). The server
// reads every at-once key on each request; next-index-run keys are read by
// the index run, which re-links over the fact cache.
const (
	AppliesAtOnce       = "at_once"
	AppliesNextIndexRun = "next_index_run"
)

// SettingKey is one row of the Settings form: a graphvein.yaml key the
// web UI shows and edits with a typed control. The table is the single
// place that decides which keys the form edits; every other key of the
// file is kept as it is and listed as outside the form. Adding a key (for
// example ui.impact_depth) is one row here plus its claim row.
type SettingKey struct {
	// Row is the claim row of docs/contract/claims/settings.md.
	Row string
	// Key is the dotted path: a section and a key inside it.
	Key  string
	Kind string
	// Choices are the values an enum accepts, in display order.
	Choices []string
	// Min and Max bound an integer.
	Min, Max int
	// Default is the built-in value when the file does not set the key;
	// nil when there is none (an absent list or map applies nothing).
	Default any
	// Applies says when a written change takes effect.
	Applies string
	// Shorthand is true when the section itself may be a scalar standing
	// for this key (`ide: cursor` is `ide: {editor: cursor}`).
	Shorthand bool
	// LockedBy names a key of the same section whose presence makes this
	// key not editable in the form (it would not decide the result).
	LockedBy string
}

// section and leaf split Key.
func (k SettingKey) section() string { s, _, _ := strings.Cut(k.Key, "."); return s }
func (k SettingKey) leaf() string    { _, l, _ := strings.Cut(k.Key, "."); return l }

// settingKeys is the form's key table in display order (rows ST-1..ST-11,
// then the T042 depths ST-31 and ST-32).
var settingKeys = []SettingKey{
	{Row: "ST-1", Key: "ui.labels", Kind: SettingEnum, Choices: []string{LabelsPlain, LabelsTechnical}, Default: LabelsPlain, Applies: AppliesAtOnce},
	{Row: "ST-2", Key: "ide.editor", Kind: SettingEnum, Choices: PresetNames(), Default: DefaultEditor, Applies: AppliesAtOnce, Shorthand: true, LockedBy: "link_template"},
	{Row: "ST-3", Key: "index.shape_depth", Kind: SettingInteger, Min: MinShapeDepth, Max: MaxShapeDepth, Default: DefaultShapeDepth, Applies: AppliesNextIndexRun},
	{Row: "ST-4", Key: "clients.base_paths", Kind: SettingMap, Applies: AppliesNextIndexRun},
	{Row: "ST-5", Key: "clients.link_generated_output", Kind: SettingBoolean, Default: false, Applies: AppliesNextIndexRun},
	{Row: "ST-6", Key: "tests.include", Kind: SettingList, Applies: AppliesNextIndexRun},
	{Row: "ST-7", Key: "tests.exclude", Kind: SettingList, Applies: AppliesNextIndexRun},
	{Row: "ST-8", Key: "openapi.include", Kind: SettingList, Applies: AppliesNextIndexRun},
	{Row: "ST-9", Key: "openapi.exclude", Kind: SettingList, Applies: AppliesNextIndexRun},
	{Row: "ST-10", Key: "openapi.base_paths", Kind: SettingMap, Applies: AppliesNextIndexRun},
	{Row: "ST-11", Key: "architecture.hide", Kind: SettingList, Applies: AppliesAtOnce},
	// The walk depths (T042): the depth the Impact tab and the evidence
	// chain start with; this browser's choice wins in the UI (ADR 0078).
	{Row: "ST-31", Key: "ui.impact_depth", Kind: SettingInteger, Min: MinUIDepth, Max: MaxUIDepth, Default: DefaultImpactDepth, Applies: AppliesAtOnce},
	{Row: "ST-32", Key: "ui.chain_depth", Kind: SettingInteger, Min: MinUIDepth, Max: MaxUIDepth, Default: DefaultChainDepth, Applies: AppliesAtOnce},
}

// SettingKeys returns the form's key table in display order.
func SettingKeys() []SettingKey { return append([]SettingKey(nil), settingKeys...) }

// SettingKeyByName returns the table row of key.
func SettingKeyByName(key string) (SettingKey, bool) {
	for _, k := range settingKeys {
		if k.Key == key {
			return k, true
		}
	}
	return SettingKey{}, false
}

// MapEntry is one pair of a SettingMap value, in file order.
type MapEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Origins of a Settings value.
const (
	// OriginFile: graphvein.yaml sets the key (Line is its line).
	OriginFile = "file"
	// OriginBuiltin: the file does not set it; the built-in default applies.
	OriginBuiltin = "builtin"
	// OriginNotSet: the file does not set it and there is no default.
	OriginNotSet = "not_set"
)

// SettingValue is a key's effective value as graphvein.yaml gives it.
type SettingValue struct {
	Key SettingKey
	// Value is a string, int, bool, []string or []MapEntry; nil when
	// Origin is OriginNotSet.
	Value  any
	Origin string
	// Line is the key's line in the file (OriginFile only).
	Line int
	// Locked is the LockedBy key when the file sets it, so the form
	// cannot edit this key; empty otherwise. LockedLine is that key's
	// line, so the UI can say which line decides instead.
	Locked     string
	LockedLine int
}

// OtherKey is a key the file sets that the form does not show: a scalar
// root key, or a key inside a mapping section. Typed edits never change it.
type OtherKey struct {
	Key  string
	Line int
}

// SettingValues reads the form's keys from graphvein.yaml text (nil or
// empty: no file). The caller has validated data with Parse, so every
// value has the type its decoder accepts. Other lists the keys of the
// file outside the form, in file order.
func SettingValues(data []byte) (values []SettingValue, other []OtherKey, err error) {
	root, err := settingsRoot(data)
	if err != nil {
		return nil, nil, err
	}
	for _, k := range settingKeys {
		v := SettingValue{Key: k, Value: k.Default, Origin: OriginBuiltin}
		if k.Default == nil {
			v.Origin = OriginNotSet
		}
		key, node := lookupSetting(root, k)
		if node != nil {
			v.Origin, v.Line, v.Value = OriginFile, key.Line, nodeValue(k, node)
		}
		if _, sec := mappingEntry(root, k.section()); k.LockedBy != "" && sec != nil {
			if lk, _ := mappingEntry(sec, k.LockedBy); lk != nil {
				v.Locked, v.LockedLine = k.section()+"."+k.LockedBy, lk.Line
			}
		}
		values = append(values, v)
	}
	if root == nil {
		return values, nil, nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if value.Kind != yaml.MappingNode {
			// A shorthand scalar (`ide: cursor`) is the form's key.
			if _, ok := shorthandFor(key.Value); !ok || value.Kind != yaml.ScalarNode {
				other = append(other, OtherKey{Key: key.Value, Line: key.Line})
			}
			continue
		}
		for j := 0; j+1 < len(value.Content); j += 2 {
			name := key.Value + "." + value.Content[j].Value
			if _, ok := SettingKeyByName(name); !ok {
				other = append(other, OtherKey{Key: name, Line: value.Content[j].Line})
			}
		}
	}
	return values, other, nil
}

// shorthandFor returns the row whose section may be written as a scalar.
func shorthandFor(section string) (SettingKey, bool) {
	for _, k := range settingKeys {
		if k.Shorthand && k.section() == section {
			return k, true
		}
	}
	return SettingKey{}, false
}

// settingsRoot is the root mapping of data; nil for no file.
func settingsRoot(data []byte) (*yaml.Node, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", File, err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is not a mapping", File)
	}
	return doc.Content[0], nil
}

// lookupSetting returns the key node and value node of k in root; the
// section's own key and scalar for a shorthand. Both nil when absent.
func lookupSetting(root *yaml.Node, k SettingKey) (*yaml.Node, *yaml.Node) {
	if root == nil {
		return nil, nil
	}
	skey, sec := mappingEntry(root, k.section())
	if sec == nil {
		return nil, nil
	}
	if k.Shorthand && sec.Kind == yaml.ScalarNode {
		return skey, sec
	}
	if sec.Kind != yaml.MappingNode {
		return nil, nil
	}
	return mappingEntry(sec, k.leaf())
}

// mappingEntry returns the key and value nodes of key in mapping m.
func mappingEntry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// nodeValue converts a validated value node to the key's Go value. Every
// scalar goes through yaml.v3's Decode, as the strict decoder reads it
// (`shape_depth: 0x7` is 7, `010` is 8, `1_0` is 10), never through a
// second conversion of the raw text: a value shown or compared here is
// the value an index run uses.
func nodeValue(k SettingKey, n *yaml.Node) any {
	scalar := func(n *yaml.Node) string {
		var s string
		_ = n.Decode(&s)
		return s
	}
	switch k.Kind {
	case SettingInteger:
		var i int
		_ = n.Decode(&i)
		return i
	case SettingBoolean:
		var b bool
		_ = n.Decode(&b)
		return b
	case SettingList:
		out := []string{}
		for _, item := range n.Content {
			out = append(out, scalar(item))
		}
		return out
	case SettingMap:
		out := []MapEntry{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, MapEntry{Key: scalar(n.Content[i]), Value: scalar(n.Content[i+1])})
		}
		return out
	}
	return scalar(n)
}

// settingNode builds the YAML node of a typed value; flow copies the
// flow style of the value it replaces.
func settingNode(k SettingKey, value any, flow bool) (*yaml.Node, error) {
	var style yaml.Style
	if flow {
		style = yaml.FlowStyle
	}
	switch k.Kind {
	case SettingEnum:
		if s, ok := value.(string); ok {
			return str(s), nil
		}
	case SettingInteger:
		if i, ok := value.(int); ok {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(i)}, nil
		}
	case SettingBoolean:
		if b, ok := value.(bool); ok {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}, nil
		}
	case SettingList:
		if items, ok := value.([]string); ok {
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: style}
			for _, s := range items {
				n.Content = append(n.Content, str(s))
			}
			return n, nil
		}
	case SettingMap:
		if pairs, ok := value.([]MapEntry); ok {
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: style}
			for _, p := range pairs {
				n.Content = append(n.Content, str(p.Key), str(p.Value))
			}
			return n, nil
		}
	}
	return nil, fmt.Errorf("%s takes a %s value, not %T", k.Key, k.Kind, value)
}

// ChangedSettings compares two texts of graphvein.yaml (nil: no file):
// the form keys whose effective value differs, in table order, and
// whether anything outside the form differs (keys outside the table,
// their values or order). Comments, styles and layout are not changes.
// Both texts must be YAML mappings; the caller has validated them.
func ChangedSettings(old, new []byte) (keys []SettingKey, otherChanged bool, err error) {
	a, _, err := SettingValues(old)
	if err != nil {
		return nil, false, err
	}
	b, _, err := SettingValues(new)
	if err != nil {
		return nil, false, err
	}
	for i := range a {
		if !reflect.DeepEqual(a[i].Value, b[i].Value) {
			keys = append(keys, a[i].Key)
		}
	}
	ra, err := withoutSettings(old)
	if err != nil {
		return nil, false, err
	}
	rb, err := withoutSettings(new)
	if err != nil {
		return nil, false, err
	}
	return keys, !nodeEqual(ra, rb), nil
}

// withoutSettings is the root mapping of data with every form key (and a
// section it empties) removed; `version: 1` for no file.
func withoutSettings(data []byte) (*yaml.Node, error) {
	if len(data) == 0 {
		data = []byte("version: 1\n")
	}
	root, err := settingsRoot(data)
	if err != nil {
		return nil, err
	}
	for _, k := range settingKeys {
		if _, v := lookupSetting(root, k); v != nil {
			// The key is set, so removing it cannot fail on a validated file;
			// a locked key is removed directly.
			if err := applySetting(root, SettingOp{Key: k.Key, Remove: true}); err != nil {
				_, sec := mappingEntry(root, k.section())
				removeKey(sec, k.leaf())
			}
		}
	}
	return root, nil
}
