package routeshape

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ToAttribute validates s and returns the value to store under
// AttributeKey on the route node: a plain JSON value (maps, slices,
// strings, numbers), the same form the store hands back, so an in-process
// snapshot and a stored one compare equal. A shape that breaks a model
// invariant is refused: the extractor records a diagnostic and stores no
// shape (every section then reads not_read).
func ToAttribute(s *Shape) (map[string]any, error) {
	c := *s
	c.normalize()
	if err := Error(Validate(&c)); err != nil {
		return nil, fmt.Errorf("invalid route shape: %w", err)
	}
	data, err := json.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("encoding route shape: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decoding route shape: %w", err)
	}
	return out, nil
}

// FromAttribute reads a stored shape attribute. It refuses an unknown
// field, another model version and any invariant violation (rows SM-1 and
// D): a reader shows such a route as not read, never as a partial guess.
func FromAttribute(value any) (*Shape, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding route shape attribute: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var s Shape
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("decoding route shape attribute: %w", err)
	}
	if s.Version != ModelVersion {
		return nil, fmt.Errorf("route shape version %d is not %d", s.Version, ModelVersion)
	}
	s.normalize()
	if err := Error(Validate(&s)); err != nil {
		return nil, fmt.Errorf("invalid route shape: %w", err)
	}
	return &s, nil
}

// Summary is the compact form of a shape for route lists: each section's
// state and item count, and how many chains hit the depth cap.
type Summary struct {
	Sections map[string]SectionSummary `json:"sections"`
	DepthCap int                       `json:"depth_cap"`
	// DepthLimit is the limit the shape was followed with.
	DepthLimit int `json:"depth_limit"`
}

// SectionSummary is one section's state and item count.
type SectionSummary struct {
	State State `json:"state"`
	Items int   `json:"items"`
}

// Summarize returns the compact form of s.
func Summarize(s *Shape) Summary {
	counts := []int{len(s.Path.Items), len(s.Query.Items), len(s.Header.Items), len(s.Cookie.Items), len(s.Body.Items), len(s.Responses.Items), len(s.Security.Items)}
	out := Summary{Sections: map[string]SectionSummary{}, DepthLimit: s.Follow.DepthLimit}
	for i, sec := range s.sections() {
		out.Sections[SectionNames[i]] = SectionSummary{State: sec.State, Items: counts[i]}
	}
	for _, c := range s.Follow.Chains {
		if c.Stop == StopDepthCap {
			out.DepthCap++
		}
	}
	return out
}
