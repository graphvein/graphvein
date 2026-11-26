// Package shapediff compares what a linked frontend call sends with what
// the route it reaches reads (T029 W4, rows SD-1..SD-12 of
// docs/contract/claims/shape-differences.md).
//
// The comparison exists so the link details can say "sent by the
// frontend, not read by the handler" where both sides are fully read, and
// say nothing anywhere else. Two halves keep the precision rules apart:
//
//   - the extractor stores what a call sends as the client_call node
//     attribute "sends" (Sends), only for the forms whose keys are proven
//     (a literal object, or a typed generated client whose type is read);
//   - the server compares that attribute with the route's stored shape
//     (Compare) when it presents a link, only where the route's section is
//     complete and every item in it is an enforced declaration.
//
// Anything else gives no result at all, never "no differences".
package shapediff

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// AttributeKey is the client_call node attribute that holds a Sends value
// (row SD-1). A call without it sends nothing Graphvein compares.
const AttributeKey = "sends"

// Version is the version of the attribute's own format. A reader refuses
// any other version instead of guessing at its meaning.
const Version = 1

// Forms of a sent part: how its keys are known.
const (
	// FormLiteral: an object literal written at the call site, with
	// literal keys.
	FormLiteral = "literal"
	// FormTyped: the declared type of a typed generated client's
	// operation (the client is proven, its type is read from the
	// repository).
	FormTyped = "typed"
)

// JSON is the one body media type compared.
const JSON = "application/json"

// Sends is what one client call sends, as far as it is proven.
type Sends struct {
	Version int `json:"version"`
	// Body is the request body's keys; nil when not proven.
	Body *Part `json:"body,omitempty"`
	// Query is the query string's keys; nil when not proven.
	Query *Part `json:"query,omitempty"`
}

// Part is one sent part (the body or the query string).
type Part struct {
	// Rule is the row that read it (SD-2..SD-5).
	Rule string `json:"rule"`
	// Form is FormLiteral or FormTyped.
	Form string `json:"form"`
	// ContentType is the body's media type (body only).
	ContentType string `json:"content_type,omitempty"`
	// Type names the declared type of a typed part.
	Type string `json:"type,omitempty"`
	// File and Line locate the object literal or the type declaration.
	File string `json:"file"`
	Line int    `json:"line"`
	// Complete is true when every key is known: a literal object without
	// a spread, a computed key, a method or an accessor. A typed part is
	// never complete (a value of a TypeScript type may carry more keys).
	Complete bool `json:"complete"`
	// Fields are the keys sent: every key of a literal, the required
	// properties of a type.
	Fields []Field `json:"fields"`
}

// Field is one key sent.
type Field struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Nested is the object sent under the key when it is itself an object
	// literal or an object type; nil otherwise.
	Nested *Nested `json:"nested,omitempty"`
}

// Nested is an object sent under a key.
type Nested struct {
	Complete bool    `json:"complete"`
	Fields   []Field `json:"fields"`
}

// ToAttribute returns the attribute value of s.
func ToAttribute(s *Sends) (map[string]any, error) {
	c := *s
	c.Version = Version
	if err := c.validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("encoding sends: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decoding sends: %w", err)
	}
	return out, nil
}

// FromAttribute decodes and checks a stored attribute. Any unknown field,
// another version or a broken rule is refused: the call is then compared
// with nothing.
func FromAttribute(value any) (*Sends, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding sends attribute: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var s Sends
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("decoding sends attribute: %w", err)
	}
	if s.Version != Version {
		return nil, fmt.Errorf("sends attribute version %d, want %d", s.Version, Version)
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Sends) validate() error {
	if s.Body == nil && s.Query == nil {
		return fmt.Errorf("sends attribute without a part")
	}
	for name, p := range map[string]*Part{"body": s.Body, "query": s.Query} {
		if p == nil {
			continue
		}
		if p.Form != FormLiteral && p.Form != FormTyped {
			return fmt.Errorf("sends %s: unknown form %q", name, p.Form)
		}
		if p.Form == FormTyped && p.Complete {
			return fmt.Errorf("sends %s: a typed part is never complete", name)
		}
		if (name == "body") != (p.ContentType != "") {
			return fmt.Errorf("sends %s: content type %q", name, p.ContentType)
		}
		if p.File == "" || p.Line < 1 || p.Rule == "" {
			return fmt.Errorf("sends %s: missing provenance", name)
		}
		if err := validFields(p.Fields, p.Form == FormTyped, 0); err != nil {
			return fmt.Errorf("sends %s: %w", name, err)
		}
	}
	return nil
}

func validFields(fields []Field, typed bool, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("fields nested deeper than %d", maxDepth)
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if f.Name == "" || seen[f.Name] || f.File == "" || f.Line < 1 {
			return fmt.Errorf("field %q repeated or without provenance", f.Name)
		}
		seen[f.Name] = true
		if f.Nested != nil {
			if typed && f.Nested.Complete {
				return fmt.Errorf("field %q: a typed object is never complete", f.Name)
			}
			if err := validFields(f.Nested.Fields, typed, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// maxDepth bounds the nesting read and compared.
const maxDepth = 8
