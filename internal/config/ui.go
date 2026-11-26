package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Label styles of the web UI (T025b): how it words ids, kinds and
// confidence numbers. A browser's own choice wins over the repository's.
const (
	// LabelsPlain shows plain words, with ids and numbers in tooltips.
	LabelsPlain = "plain"
	// LabelsTechnical shows rule ids, tiers with confidence numbers and raw
	// kinds as the primary text.
	LabelsTechnical = "technical"
)

// Depth defaults of the web UI (T042). The values equal
// changes.DefaultDepth, traverse.ChainDefaultDepth and traverse.MaxDepth;
// config does not import those packages, and a test in internal/server
// asserts they agree.
const (
	// MinUIDepth is the smallest depth a walk may take.
	MinUIDepth = 1
	// MaxUIDepth is the deepest walk change impact and the chain allow.
	MaxUIDepth = 10
	// DefaultImpactDepth is the Impact tab's built-in depth.
	DefaultImpactDepth = 3
	// DefaultChainDepth is the architecture chain's built-in depth.
	DefaultChainDepth = 6
)

// UI holds the web UI defaults a repository can set. A zero value means
// graphvein.yaml sets none, so the UI's built-in default applies (plain
// labels, impact depth 3, chain depth 6). A browser's own choice wins over
// every one of them.
type UI struct {
	Labels      string
	ImpactDepth int
	ChainDepth  int
}

// UIKey is one key of the ui section. UIKeys lists every key, the decoder
// reads graphvein.yaml only through it, and the Settings page can list the
// keys from it, so a new key is one row here and nowhere else.
type UIKey struct {
	// Name is the key under ui.
	Name string
	// Words are the values a word key accepts; nil for a number key.
	Words []string
	// Min and Max bound a number key (both inclusive).
	Min, Max int
	// Default is the built-in value as written in graphvein.yaml, which the
	// UI applies when neither the browser nor the repository sets one.
	Default string
	// Doc says in one line what the key changes.
	Doc string

	set func(u *UI, word string, n int)
}

// UIKeys are the keys of the ui section, in the documented order
// (docs/config.md "UI").
var UIKeys = []UIKey{
	{
		Name: "labels", Words: []string{LabelsPlain, LabelsTechnical}, Default: LabelsPlain,
		Doc: "how the UI words ids, kinds and confidence numbers",
		set: func(u *UI, word string, _ int) { u.Labels = word },
	},
	{
		Name: "impact_depth", Min: MinUIDepth, Max: MaxUIDepth, Default: strconv.Itoa(DefaultImpactDepth),
		Doc: "the change impact depth the Impact tab starts with",
		set: func(u *UI, _ string, n int) { u.ImpactDepth = n },
	},
	{
		Name: "chain_depth", Min: MinUIDepth, Max: MaxUIDepth, Default: strconv.Itoa(DefaultChainDepth),
		Doc: "the architecture chain depth the evidence chain starts with",
		set: func(u *UI, _ string, n int) { u.ChainDepth = n },
	},
}

// uiKey finds a row of UIKeys by name.
func uiKey(name string) (UIKey, bool) {
	for _, k := range UIKeys {
		if k.Name == name {
			return k, true
		}
	}
	return UIKey{}, false
}

// decode reads one value of this key into u, or says what the key accepts.
func (k UIKey) decode(u *UI, v *yaml.Node) error {
	if k.Words != nil {
		for _, w := range k.Words {
			if v.Kind == yaml.ScalarNode && v.Tag == "!!str" && v.Value == w {
				k.set(u, w, 0)
				return nil
			}
		}
		return fmt.Errorf("line %d: ui.%s must be %s", v.Line, k.Name, strings.Join(k.Words, " or "))
	}
	var n int
	if v.Kind != yaml.ScalarNode || v.Tag != "!!int" || v.Decode(&n) != nil || n < k.Min || n > k.Max {
		return fmt.Errorf("line %d: ui.%s must be an integer from %d to %d", v.Line, k.Name, k.Min, k.Max)
	}
	k.set(u, "", n)
	return nil
}

// decodeUI accepts the keys of UIKeys, for example
// `ui: {labels: technical, impact_depth: 4, chain_depth: 8}`. Unknown keys
// are an error, like every other section, so a typo is never silently
// ignored.
func decodeUI(n *yaml.Node) (UI, error) {
	var u UI
	if n.Kind != yaml.MappingNode {
		return u, fmt.Errorf("ui must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return u, fmt.Errorf("line %d: ui keys must be strings", k.Line)
		}
		if seen[k.Value] {
			return u, fmt.Errorf("line %d: duplicate ui key %s", k.Line, k.Value)
		}
		seen[k.Value] = true
		key, ok := uiKey(k.Value)
		if !ok {
			return u, fmt.Errorf("line %d: unknown ui key %s", k.Line, k.Value)
		}
		if err := key.decode(&u, v); err != nil {
			return u, err
		}
	}
	return u, nil
}
