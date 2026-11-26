package changes

import (
	"fmt"
	"strconv"

	"github.com/graphvein/graphvein/internal/store"
)

// Violations re-checks the classification invariants of the T019 oracle
// (step 5) on a computed comparison, independently of the code paths that
// produced the items: every item names a contract row and a key of the raw
// change set; no removed record lies in a gap of the head and no added one
// in a gap of the base (G3); every lost link carries a proven reason with
// a known static, unmatched head call or a removed call (Revision 1); a
// D item has a kind no row names; move hints pair a removed and an
// added node both ways. It returns one message per violation; the goldens
// and the corpus oracle require none.
func (c *Comparison) Violations() []string {
	var out []string
	keys := map[string]bool{}
	for _, rc := range c.set.Changes {
		keys[itemID(rc.Record, rc.Key)] = true
	}
	for p := range gapSet(c.set.Base.Run.Gaps, c.set.Head.Run.Gaps) {
		keys[itemID("gap", p)] = true
	}
	for i := range c.items {
		it := &c.items[i]
		id := it.ID()
		row, ok := rowOf(it.Row)
		if !ok || row.Group == "" {
			out = append(out, fmt.Sprintf("%s: row %q is not a change row", id, it.Row))
		}
		backed := keys[id]
		for _, rel := range it.raw.Related {
			backed = backed || keys[itemID(rel.Record, rel.Key)]
		}
		if !backed {
			out = append(out, fmt.Sprintf("%s: no record of the raw change set", id))
		}
		if it.Row == "D" && namedKind(it.Record, it.Kind) && !it.undecodable {
			out = append(out, fmt.Sprintf("%s: unclassified although row kinds name %s", id, it.Kind))
		}
		if it.Row == "C16" && it.Record == store.RecordEdge && (structuralEdges[it.Kind] || tableAccessKinds[it.Kind]) {
			if _, source, target, err := store.ParseEdgeKey(it.Identity); err == nil {
				for _, end := range []string{source, target} {
					if o, ok := c.byID[itemID(store.RecordNode, end)]; ok && classifiedRows[c.items[o].Row] {
						out = append(out, fmt.Sprintf("%s: structural edge claimed beside the %s item of %s", id, c.items[o].Row, end))
					}
				}
			}
		}
		switch it.Change {
		case Removed, Added:
			// G3 is symmetric (R3-7): both sides must know the footprint.
			for side, name := range [2]string{"base", "head"} {
				if !c.know[side].knows(it.footprint) {
					out = append(out, fmt.Sprintf("%s: %s although a footprint file is a %s gap", id, it.Change, name))
				}
			}
		case LinkLost:
			out = append(out, c.linkViolations(it)...)
		}
		for _, h := range it.Hints {
			other, ok := c.byID[h.Other]
			if h.Row != "C14" || !ok {
				out = append(out, fmt.Sprintf("%s: hint to %s is not a C14 pair", id, h.Other))
				continue
			}
			o := &c.items[other]
			mutual := false
			for _, back := range o.Hints {
				mutual = mutual || back.Other == it.Identity
			}
			pair := it.Change == Removed && o.Change == Added || it.Change == Added && o.Change == Removed
			if !mutual || !pair {
				out = append(out, fmt.Sprintf("%s: hint to %s is not a mutual removed/added pair", id, h.Other))
			}
		}
	}
	return out
}

func gapSet(lists ...[]string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lists {
		for _, p := range l {
			out[p] = true
		}
	}
	return out
}

// linkViolations checks one link_lost item against C5's conditions.
func (c *Comparison) linkViolations(it *Item) []string {
	id := it.ID()
	kind, source, target, err := store.ParseEdgeKey(it.Identity)
	if err != nil || it.Reason == nil {
		return []string{fmt.Sprintf("%s: lost link without a reason", id)}
	}
	spec := routeLinkSpec
	switch kind {
	case "calls_operation":
		spec = operationLinkSpec
	case "invokes_command":
		spec = commandLinkSpec
	}
	node := func(side int, nid string) map[string]any {
		var raw any
		if i, ok := c.byID[nid]; ok {
			raw = c.items[i].raw.Before
			if side == head {
				raw = c.items[i].raw.After
			}
		}
		return rawAttributes(raw)
	}
	var out []string
	callItem, callChanged := c.byID[source]
	headCall := node(head, source)
	if callChanged && headCall == nil && (*it.Reason != ReasonCallAbsent || c.items[callItem].Change != Removed) {
		out = append(out, fmt.Sprintf("%s: lost link of a call absent on the head without a removed call", id))
	}
	if headCall != nil && headCall["status"] != "unmatched" {
		out = append(out, fmt.Sprintf("%s: lost link whose head call status is %v", id, headCall["status"]))
	}
	targetItem, targetListed := c.byID[target]
	switch *it.Reason {
	case ReasonTargetRemoved:
		if !targetListed || c.items[targetItem].Change != Removed {
			out = append(out, fmt.Sprintf("%s: target_removed without a removed target", id))
		}
	case ReasonTargetChanged:
		ok := false
		if targetListed {
			for _, f := range c.items[targetItem].Fields {
				for _, n := range spec.targetFields {
					ok = ok || f.Name == n
				}
			}
		}
		if !ok {
			out = append(out, fmt.Sprintf("%s: target_changed without a changed method or path", id))
		}
	case ReasonCallAbsent:
		if !callChanged || headCall != nil || c.items[callItem].Change != Removed {
			out = append(out, fmt.Sprintf("%s: call_absent without a removed call", id))
		}
	case ReasonCallChanged:
		if !callChanged || targetListed && c.items[targetItem].Change != BodyChanged {
			out = append(out, fmt.Sprintf("%s: call_changed without a changed call and an unchanged target", id))
		}
	default:
		out = append(out, fmt.Sprintf("%s: unknown lost-link reason %q", id, *it.Reason))
	}
	if *it.Reason != ReasonCallChanged && callChanged {
		b, h := node(base, source), node(head, source)
		for _, name := range spec.callFields {
			if name == "url_templates" {
				continue
			}
			if b != nil && h != nil && canonical(b[name]) != canonical(h[name]) {
				out = append(out, fmt.Sprintf("%s: %s although the call's %s changed", id, *it.Reason, name))
			}
		}
	}
	return out
}

// rawAttributes decodes the attributes of a raw node payload.
func rawAttributes(raw any) map[string]any {
	data, ok := raw.([]byte)
	if !ok {
		if m, isRaw := raw.(interface{ MarshalJSON() ([]byte, error) }); isRaw {
			data, _ = m.MarshalJSON()
		}
	}
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var n struct {
		Attributes map[string]any `json:"attributes"`
	}
	if decode(data, &n) != nil {
		return nil
	}
	return n.Attributes
}

// ImpactViolations checks one traversal: every step names a relation row,
// a path's provenance is fact only when every step is, none follows an
// inferred relation (I15), and it touches no
// node in a gap file (R3-10), its confidence is
// the weakest step's, and the claim wording follows the provenance.
func ImpactViolations(r *ImpactResult) []string {
	var out []string
	// R3-10: a path through a node in a gap file is inferred whatever its
	// steps' provenance.
	gaps := map[string]bool{}
	for _, n := range r.Unknowns.GapNodes {
		gaps[n] = true
	}
	for _, p := range r.Paths {
		fact, conf, stepInferred := !gaps[p.Node], 1.0, false
		for _, s := range p.Steps {
			stepInferred = stepInferred || s.Provenance != "fact"
			fact = fact && !gaps[s.Source] && !gaps[s.Target]
			if _, ok := relationOf(s.Relation); !ok || s.Relation == "ID" || s.Relation == "I11" {
				out = append(out, fmt.Sprintf("%s: step %s names relation %q", p.Node, s.Kind, s.Relation))
			}
			// Revision 4: an I8 step is a code-to-table edge and carries
			// its owner's entry; no other step does.
			if (s.Relation == "I8") != (s.Access != nil) || s.Relation == "I8" && !tableAccessKinds[s.Kind] {
				out = append(out, fmt.Sprintf("%s: step %s of relation %s with access %t", p.Node, s.Kind, s.Relation, s.Access != nil))
			}
			fact = fact && s.Provenance == "fact" && !inferredRelations[s.Relation]
			conf = min(conf, s.Confidence)
		}
		want := "inferred"
		claim := "may depend on"
		if fact {
			want, claim = "fact", "depends on"
		}
		// Revision 7: a path inferred only by a rule (I15, a
		// gap) has no confidence; any other path has the weakest step's.
		confOK := p.Confidence == nil && !fact && !stepInferred || p.Confidence != nil && *p.Confidence == conf && (fact || stepInferred)
		if p.Provenance != want || !confOK || p.Claim != claim || len(p.Steps) != p.Distance {
			out = append(out, fmt.Sprintf("%s: provenance %s confidence %s claim %q over %d steps at distance %d", p.Node, p.Provenance, confidenceText(p.Confidence), p.Claim, len(p.Steps), p.Distance))
		}
		// T022 B7: a project link step (I12, I13) shows its assumption and
		// names both repositories; a path through a stale link is stale.
		stale := false
		for _, s := range p.Steps {
			stale = stale || len(s.StaleReasons) > 0
			if projectRelation[s.Relation] != (s.Assumption != "") || projectRelation[s.Relation] && (s.SourceRepository == "" || s.TargetRepository == "") {
				out = append(out, fmt.Sprintf("%s: step %s of relation %s with assumption %q", p.Node, s.Kind, s.Relation, s.Assumption))
			}
		}
		if p.Stale != stale {
			out = append(out, fmt.Sprintf("%s: stale %t over steps stale %t", p.Node, p.Stale, stale))
		}
		if n := len(p.Steps); n > 0 && p.Steps[n-1].Relation == "I8" && p.Wording != accessWording(p.Steps[n-1]) {
			out = append(out, fmt.Sprintf("%s: I8 wording %q", p.Node, p.Wording))
		}
	}
	// Revision 4: a table's users are a lower bound, never "no users".
	for _, u := range r.TableUses {
		if u.Wording != "uses found" {
			out = append(out, fmt.Sprintf("%s: table users worded %q", u.Table, u.Wording))
		}
		for _, m := range u.MayAlsoBeUsedBy {
			if m.Provenance != "inferred" {
				out = append(out, fmt.Sprintf("%s: may also be used by %s as %s", u.Table, m.Owner, m.Provenance))
			}
		}
	}
	return out
}

// confidenceText prints a path confidence, "none" when it has none.
func confidenceText(c *float64) string {
	if c == nil {
		return "none"
	}
	return strconv.FormatFloat(*c, 'g', -1, 64)
}
