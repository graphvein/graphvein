package config

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file edits a block list or block mapping of a Settings key entry
// by entry (ADR 0088, row ST-27): an entry the edit keeps keeps every one
// of its lines (its comments included), a removed entry takes its own
// lines and the comment lines directly above it, an added entry is
// written next to its neighbours, and a mapping entry whose value alone
// changes is rewritten in place with its line comment.

// collectionEntry is one entry of a block list or block mapping as text.
type collectionEntry struct {
	// id is the list item's value or the mapping entry's key.
	id string
	// key is the mapping entry's key node (nil for a list item); value is
	// the item or the entry's value.
	key, value *yaml.Node
	// pre are the lines between the previous entry and this entry's head
	// comment (blank lines, comments set apart by a blank line): they
	// belong to no entry and are always kept. head are the comment lines
	// directly above the entry, body its own lines.
	pre, head, body []string
}

// collectionEntries splits the lines [from, end) holding the entries of
// the block collection coll into entries; ok is false when an entry does
// not start its own line.
func (t textLines) collectionEntries(coll *yaml.Node, from, end int) ([]collectionEntry, bool) {
	var out []collectionEntry
	step := 1
	if coll.Kind == yaml.MappingNode {
		step = 2
	}
	starts := []int{}
	for i := 0; i < len(coll.Content); i += step {
		n := coll.Content[i]
		start := n.Line - 1
		if start < from || start >= end || (len(starts) > 0 && start <= starts[len(starts)-1]) {
			return nil, false
		}
		line := strings.TrimSpace(t.lines[start])
		if coll.Kind == yaml.SequenceNode && !strings.HasPrefix(line, "-") {
			return nil, false
		}
		starts = append(starts, start)
		e := collectionEntry{value: n, id: n.Value}
		if step == 2 {
			e.key, e.value = n, coll.Content[i+1]
			if n.Kind != yaml.ScalarNode {
				return nil, false
			}
		} else if n.Kind != yaml.ScalarNode {
			return nil, false
		}
		out = append(out, e)
	}
	prevEnd := from
	for j := range out {
		start := starts[j]
		limit := end
		if j+1 < len(starts) {
			limit = starts[j+1]
		}
		head := start
		for head > prevEnd && strings.HasPrefix(strings.TrimSpace(t.lines[head-1]), "#") {
			head--
		}
		bodyEnd := t.trimmedEnd(start, limit)
		out[j].pre = t.lines[prevEnd:head]
		out[j].head = t.lines[head:start]
		out[j].body = t.lines[start:bodyEnd]
		prevEnd = bodyEnd
	}
	if prevEnd != end {
		return nil, false
	}
	return out, true
}

// matchEntries pairs old and new entries that stay: for a mapping the
// entries with the same key, which must keep their order (ok is false
// otherwise, or for a repeated key); for a list the longest common
// subsequence of item values.
func matchEntries(mapping bool, old, new []string) (pairs [][2]int, ok bool) {
	if mapping {
		at := map[string]int{}
		for j, id := range old {
			at[id] = j
		}
		seen := map[string]bool{}
		last := -1
		for i, id := range new {
			if seen[id] {
				return nil, false
			}
			seen[id] = true
			j, found := at[id]
			if !found {
				continue
			}
			if j < last {
				return nil, false
			}
			last = j
			pairs = append(pairs, [2]int{j, i})
		}
		return pairs, true
	}
	// lcs[j][i] is the length of the longest common subsequence of
	// old[j:] and new[i:].
	lcs := make([][]int, len(old)+1)
	for j := range lcs {
		lcs[j] = make([]int, len(new)+1)
	}
	for j := len(old) - 1; j >= 0; j-- {
		for i := len(new) - 1; i >= 0; i-- {
			switch {
			case old[j] == new[i]:
				lcs[j][i] = lcs[j+1][i+1] + 1
			case lcs[j+1][i] >= lcs[j][i+1]:
				lcs[j][i] = lcs[j+1][i]
			default:
				lcs[j][i] = lcs[j][i+1]
			}
		}
	}
	for j, i := 0, 0; j < len(old) && i < len(new); {
		switch {
		case old[j] == new[i]:
			pairs = append(pairs, [2]int{j, i})
			j, i = j+1, i+1
		case lcs[j+1][i] >= lcs[j][i+1]:
			j++
		default:
			i++
		}
	}
	return pairs, true
}

// spliceEntries rewrites the entries of the block collection old, held in
// lines [from, end), to the entries of n (same kind, block style). It
// returns the replacement of those lines; ok is false when the edit
// cannot be made entry by entry (an entry not on its own lines, a
// reordered mapping, a changed entry whose lines hold a comment the
// rewrite would drop), so the caller falls back.
func (t textLines) spliceEntries(old, n *yaml.Node, from, end int) ([]string, bool) {
	mapping := old.Kind == yaml.MappingNode
	if old.Kind != n.Kind || old.Style&yaml.FlowStyle != 0 || n.Style&yaml.FlowStyle != 0 || len(n.Content) == 0 {
		return nil, false
	}
	entries, ok := t.collectionEntries(old, from, end)
	if !ok || len(entries) == 0 {
		return nil, false
	}
	type newEntry struct {
		id         string
		key, value *yaml.Node
	}
	var added []newEntry
	if mapping {
		for i := 0; i+1 < len(n.Content); i += 2 {
			added = append(added, newEntry{n.Content[i].Value, n.Content[i], n.Content[i+1]})
		}
	} else {
		for _, item := range n.Content {
			added = append(added, newEntry{id: item.Value, value: item})
		}
	}
	oldIDs, newIDs := make([]string, len(entries)), make([]string, len(added))
	for j, e := range entries {
		oldIDs[j] = e.id
	}
	for i, e := range added {
		newIDs[i] = e.id
	}
	pairs, ok := matchEntries(mapping, oldIDs, newIDs)
	if !ok {
		return nil, false
	}
	// New lines copy the first entry's indentation (and a list's dash
	// spacing).
	first := entries[0]
	lead := first.body[0][:len(first.body[0])-len(strings.TrimLeft(first.body[0], " "))]
	dash := ""
	if !mapping {
		at := first.value.Column - 1
		dash = strings.TrimRight(first.body[0], "\r\n")
		if at <= len(lead) || at > len(dash) || strings.TrimSpace(dash[:at]) != "-" {
			return nil, false
		}
		dash = dash[:at]
	}
	write := func(e newEntry) ([]string, bool) {
		if mapping {
			return t.entryLines(e.id, e.value, len(lead))
		}
		enc, ok := encodeOneLine(e.value)
		if !ok {
			return nil, false
		}
		return []string{dash + enc + t.eol}, true
	}
	var out []string
	j, i := 0, 0
	flush := func(toOld, toNew int) bool {
		for ; j < toOld; j++ {
			// A removed entry: its head comment and lines go, the lines
			// before them stay.
			out = append(out, entries[j].pre...)
		}
		for ; i < toNew; i++ {
			lines, ok := write(added[i])
			if !ok {
				return false
			}
			out = append(out, lines...)
		}
		return true
	}
	for _, p := range pairs {
		if !flush(p[0], p[1]) {
			return nil, false
		}
		e, ne := entries[j], added[i]
		out = append(out, e.pre...)
		out = append(out, e.head...)
		switch {
		case !mapping || e.value.Kind == ne.value.Kind && e.value.Kind == yaml.ScalarNode && e.value.Value == ne.value.Value:
			out = append(out, e.body...)
		default:
			line, ok := inlineValue(e.body[0], e.key, e.value, ne.value)
			switch {
			case ok && len(e.body) == 1:
				out = append(out, line)
			case t.hasCommentIn(e.body):
				return nil, false
			default:
				lines, ok := write(ne)
				if !ok {
					return nil, false
				}
				out = append(out, lines...)
			}
		}
		j, i = j+1, i+1
	}
	if !flush(len(entries), len(added)) {
		return nil, false
	}
	return out, true
}

// hasCommentIn reports whether a line of lines may hold a comment.
func (t textLines) hasCommentIn(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "#") {
			return true
		}
	}
	return false
}

// mergeCollection is n with the nodes of old reused where an entry stays
// (a list item with the same value, a mapping entry with the same key,
// its value node rewritten in place when only the value changes), so the
// comments yaml.v3 attached to them survive a re-encode of the tree.
func mergeCollection(old, n *yaml.Node) *yaml.Node {
	if old == nil || old.Kind != n.Kind {
		return n
	}
	switch n.Kind {
	case yaml.SequenceNode:
		used := make([]bool, len(old.Content))
		for i, item := range n.Content {
			for j, o := range old.Content {
				if !used[j] && o.Kind == yaml.ScalarNode && o.Value == item.Value {
					used[j], n.Content[i] = true, o
					break
				}
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := mappingEntry(old, n.Content[i].Value)
			if k == nil || v.Kind != yaml.ScalarNode {
				continue
			}
			n.Content[i] = k
			if v.Value != n.Content[i+1].Value {
				changed := *v
				changed.Value, changed.Tag = n.Content[i+1].Value, "!!str"
				if changed.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
					changed.Style = 0
				}
				v = &changed
			}
			n.Content[i+1] = v
		}
	}
	return n
}
