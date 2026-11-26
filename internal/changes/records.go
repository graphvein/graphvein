package changes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// Sides of a record, as indexes into [2] arrays.
const (
	base = 0
	head = 1
)

// fileRec, nodeRec and edgeRec are one changed key of a change set with
// both decoded payloads (nil = absent on that side). bad is set when a
// present payload does not decode: such a record takes row D.
type fileRec struct {
	rc   store.RecordChange
	side [2]*contract.File
	bad  bool
}

type nodeRec struct {
	rc   store.RecordChange
	side [2]*contract.Node
	bad  bool
}

type edgeRec struct {
	rc   store.RecordChange
	side [2]*contract.Edge
	bad  bool
}

// decode reads a stored payload. Numbers stay json.Number so integers and
// their canonical JSON survive a round trip unchanged.
func decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding payload: %w", err)
	}
	return nil
}

func payloads(rc store.RecordChange) [2]json.RawMessage {
	return [2]json.RawMessage{rc.Before, rc.After}
}

func decodeFile(rc store.RecordChange) *fileRec {
	r := &fileRec{rc: rc}
	for i, raw := range payloads(rc) {
		if raw == nil {
			continue
		}
		var f contract.File
		if err := decode(raw, &f); err != nil || f.Path != rc.Key {
			r.bad = true
			continue
		}
		r.side[i] = &f
	}
	return r
}

func decodeNode(rc store.RecordChange) *nodeRec {
	r := &nodeRec{rc: rc}
	for i, raw := range payloads(rc) {
		if raw == nil {
			continue
		}
		var n contract.Node
		if err := decode(raw, &n); err != nil || n.Identity != rc.Key {
			r.bad = true
			continue
		}
		r.side[i] = &n
	}
	return r
}

func decodeEdge(rc store.RecordChange) *edgeRec {
	r := &edgeRec{rc: rc}
	for i, raw := range payloads(rc) {
		if raw == nil {
			continue
		}
		var e contract.Edge
		if err := decode(raw, &e); err != nil || store.EdgeKey(e.Kind, e.Source, e.Target) != rc.Key {
			r.bad = true
			continue
		}
		r.side[i] = &e
	}
	return r
}

// any returns the payload of whichever side is present, head first.
func (r *nodeRec) any() *contract.Node {
	if r.side[head] != nil {
		return r.side[head]
	}
	return r.side[base]
}

func (r *edgeRec) any() *contract.Edge {
	if r.side[head] != nil {
		return r.side[head]
	}
	return r.side[base]
}

// canonical is the JSON text of a decoded value, used for equality of
// field values. Maps encode with sorted keys.
func canonical(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(data)
}

// Projections (claim contract, Terms). Spans and evidence spans are
// positions: two payloads equal under projection but not raw are a move.
// A file record is projected to path, language, precision and content
// hash: its size and git blob id change with checkout line-ending
// conversion while the LF-normalised content hash does not (T019a2
// decision), so a difference in those alone is never a change.

func projectFile(f *contract.File) string {
	if f == nil {
		return ""
	}
	return canonical([]string{f.Path, f.Language, f.Precision, f.ContentHash})
}

func projectNode(n *contract.Node) string {
	if n == nil {
		return ""
	}
	c := *n
	c.Span = nil
	if shape, ok := c.Attributes["shape"]; ok {
		// A route's shape (T029) records source lines in its provenance
		// and call chains; like the span, they move with the code, so a
		// route that only moved is still a move.
		attrs := make(map[string]any, len(c.Attributes))
		for k, v := range c.Attributes {
			attrs[k] = v
		}
		attrs["shape"] = withoutLines(shape)
		c.Attributes = attrs
	}
	return canonical(c)
}

// withoutLines copies a decoded JSON value without its "line" keys.
func withoutLines(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if k != "line" {
				out[k] = withoutLines(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = withoutLines(item)
		}
		return out
	}
	return v
}

func projectEdge(e *contract.Edge) string {
	if e == nil {
		return ""
	}
	c := *e
	c.Evidence = make([]contract.Evidence, len(e.Evidence))
	for i, v := range e.Evidence {
		c.Evidence[i] = contract.Evidence{File: v.File}
	}
	return canonical(c)
}

// identityFile is the file part of an identity, or "" when it does not
// parse.
func identityFile(id string) string {
	_, file, _, err := contract.ParseIdentity(id)
	if err != nil {
		return ""
	}
	return file
}

// identityKind is the kind part of an identity.
func identityKind(id string) string {
	kind, _, ok := strings.Cut(id, ":")
	if !ok {
		return ""
	}
	return kind
}

// footprint is the set of files a record's state depends on (Terms): a
// node's file; an edge's source file, target file and evidence files.
// Synthetic nodes with file "." have none.
func nodeFootprint(r *nodeRec) []string {
	set := map[string]bool{}
	for _, n := range r.side {
		if n != nil {
			set[n.File] = true
		}
	}
	if len(set) == 0 {
		set[identityFile(r.rc.Key)] = true
	}
	return sortedFiles(set)
}

func edgeFootprint(r *edgeRec) []string {
	set := map[string]bool{}
	_, source, target, err := store.ParseEdgeKey(r.rc.Key)
	if err == nil {
		set[identityFile(source)] = true
		set[identityFile(target)] = true
	}
	for _, e := range r.side {
		if e == nil {
			continue
		}
		for _, v := range e.Evidence {
			set[v.File] = true
		}
	}
	return sortedFiles(set)
}

func sortedFiles(set map[string]bool) []string {
	out := []string{}
	for f := range set {
		if f != "" && f != "." {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// knowledge is what one side knows (G3): the side's gap paths and whether
// the whole side is partial.
type knowledge struct {
	gaps  []string
	whole bool
}

func newKnowledge(run store.ChangeRun) knowledge {
	gaps := append([]string(nil), run.Gaps...)
	sort.Strings(gaps)
	return knowledge{gaps: gaps, whole: run.GapsTruncated || run.GapsFileless || run.GapsUnknown}
}

// gap reports whether path is a gap or lies under a gap directory.
func (k knowledge) gap(path string) bool {
	for _, g := range k.gaps {
		if g == path || g == "." || strings.HasPrefix(path, g+"/") {
			return true
		}
	}
	return false
}

// gapUnder reports whether some gap lies at or under dir.
func (k knowledge) gapUnder(dir string) bool {
	for _, g := range k.gaps {
		if g == dir || g == "." || strings.HasPrefix(g, dir+"/") || strings.HasPrefix(dir, g+"/") {
			return true
		}
	}
	return false
}

// knows reports whether the side knows the state of a record with this
// footprint.
func (k knowledge) knows(footprint []string) bool {
	if k.whole {
		return false
	}
	for _, f := range footprint {
		if k.gap(f) {
			return false
		}
	}
	return true
}

// partialReasons names why a whole side is partial.
func partialReasons(run store.ChangeRun) []string {
	out := []string{}
	if run.GapsTruncated {
		out = append(out, "gaps_truncated")
	}
	if run.GapsFileless {
		out = append(out, "gaps_fileless")
	}
	if run.GapsUnknown {
		out = append(out, "gaps_unknown")
	}
	return out
}
