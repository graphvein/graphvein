// Package template captures Angular syntax without adding graph facts.
package template

import "github.com/graphvein/graphvein/internal/contract"

// Facts preserves source syntax for later ownership and directive resolution.
// All arrays must be present, even for a clean, empty template.
type Facts struct {
	ErrorSpans []contract.Span `json:"error_spans"`
	Elements   []Element       `json:"elements"`
	Attributes []Attribute     `json:"attributes"`
	Blocks     []Block         `json:"blocks"`
	Pipes      []Pipe          `json:"pipes"`
}

// Location keeps recovery uncertainty attached to every captured item.
type Location struct {
	Span    contract.Span `json:"span"`
	Flagged bool          `json:"flagged,omitempty"`
}

// Element retains even dropped and non-bindable source for honest replay.
type Element struct {
	Location
	Name        string `json:"name"`
	Parent      int    `json:"parent"`
	Dropped     bool   `json:"dropped,omitempty"`
	NonBindable bool   `json:"non_bindable,omitempty"`
	Select      string `json:"select,omitempty"`
}

// Attribute keeps Angular's classification separate from grammar node names.
type Attribute struct {
	Location
	Element         int            `json:"element"`
	RawName         string         `json:"raw_name"`
	NormalizedName  string         `json:"normalized_name"`
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	Value           string         `json:"value"`
	NameSpan        contract.Span  `json:"name_span"`
	ValueSpan       *contract.Span `json:"value_span,omitempty"`
	I18n            bool           `json:"i18n,omitempty"`
	ExtraStructural bool           `json:"extra_structural,omitempty"`
	InvalidVariable bool           `json:"invalid_variable,omitempty"`
}

// Parameter preserves each block header segment without evaluating it.
type Parameter struct {
	Text    string        `json:"text"`
	Span    contract.Span `json:"span"`
	Flagged bool          `json:"flagged,omitempty"`
}

// Block retains source order and ancestry; connection/grouping is link-time work.
type Block struct {
	Location
	Name       string      `json:"name"`
	RawName    string      `json:"raw_name"`
	Parameters []Parameter `json:"parameters"`
	Parent     int         `json:"parent"`
	Element    int         `json:"element"`
}

// Pipe records a lexical pipe name; errors remain unknown through ErrorSpans.
type Pipe struct {
	Location
	Name  string `json:"name"`
	Track bool   `json:"track,omitempty"`
}

// Valid distinguishes complete payloads from old or truncated caches.
func (f Facts) Valid() bool {
	return f.ErrorSpans != nil && f.Elements != nil && f.Attributes != nil && f.Blocks != nil && f.Pipes != nil
}
