package config

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// SettingChangeWords says in words what each operation does to the
// graphvein.yaml text data (which loads; nil: no file), for the edit's
// preview: a scalar with its previous value, a list or mapping item by
// item (`adds`, `removes`, `changes`) as `key: value` items, never as
// encoded JSON. One line per operation, in order.
func SettingChangeWords(data []byte, ops []SettingOp) ([]string, error) {
	values, _, err := SettingValues(data)
	if err != nil {
		return nil, err
	}
	cur := map[string]SettingValue{}
	for _, v := range values {
		cur[v.Key.Key] = v
	}
	var out []string
	for _, op := range ops {
		k, ok := SettingKeyByName(op.Key)
		if !ok {
			return nil, fmt.Errorf("%s is not a key the Settings form edits", op.Key)
		}
		was := cur[k.Key]
		if op.Remove {
			if k.Default != nil {
				out = append(out, fmt.Sprintf("removes %s (the built-in %s applies)", k.Key, scalarWord(k.Default)))
			} else {
				out = append(out, fmt.Sprintf("removes %s (nothing is set)", k.Key))
			}
			cur[k.Key] = SettingValue{Key: k, Value: k.Default, Origin: OriginBuiltin}
			continue
		}
		out = append(out, valueChangeWords(k, was, op.Value))
		cur[k.Key] = SettingValue{Key: k, Value: op.Value, Origin: OriginFile}
	}
	return out, nil
}

// valueChangeWords is one set operation in words.
func valueChangeWords(k SettingKey, was SettingValue, value any) string {
	set := was.Origin == OriginFile
	switch v := value.(type) {
	case []string:
		var old []string
		if set {
			old, _ = was.Value.([]string)
		}
		return itemWords(k.Key, set, listDiff(old, v))
	case []MapEntry:
		var old []MapEntry
		if set {
			old, _ = was.Value.([]MapEntry)
		}
		return itemWords(k.Key, set, mapDiff(old, v))
	}
	switch {
	case set && reflect.DeepEqual(was.Value, value):
		return fmt.Sprintf("sets %s to %s (unchanged)", k.Key, scalarWord(value))
	case set:
		return fmt.Sprintf("sets %s to %s (was %s)", k.Key, scalarWord(value), scalarWord(was.Value))
	case was.Value != nil:
		return fmt.Sprintf("sets %s to %s (the built-in value was %s)", k.Key, scalarWord(value), scalarWord(was.Value))
	}
	return fmt.Sprintf("sets %s to %s", k.Key, scalarWord(value))
}

// itemWords joins a list or mapping change: "sets K: adds `a`; removes
// `b`", or "sets K: reorders its items" when only the order changes.
func itemWords(key string, set bool, parts []string) string {
	if !set {
		key += " (not set before)"
	}
	if len(parts) == 0 {
		return "sets " + key + ": no item changes"
	}
	return "sets " + key + ": " + strings.Join(parts, "; ")
}

// listDiff words the items of a list that are added and removed (each
// value counted as often as it occurs), or a reordering.
func listDiff(old, new []string) []string {
	left := map[string]int{}
	for _, s := range old {
		left[s]++
	}
	var added, removed []string
	for _, s := range new {
		if left[s] > 0 {
			left[s]--
			continue
		}
		added = append(added, "`"+s+"`")
	}
	for _, s := range old {
		if left[s] > 0 {
			left[s]--
			removed = append(removed, "`"+s+"`")
		}
	}
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "adds "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removes "+strings.Join(removed, ", "))
	}
	if len(parts) == 0 && !reflect.DeepEqual(old, new) && len(old) > 0 {
		parts = append(parts, "reorders its items")
	}
	return parts
}

// mapDiff words the `key: value` entries of a mapping that are added,
// changed (with the previous value) and removed, or a reordering.
func mapDiff(old, new []MapEntry) []string {
	before := map[string]string{}
	for _, e := range old {
		before[e.Key] = e.Value
	}
	after := map[string]bool{}
	var added, changed, removed []string
	for _, e := range new {
		after[e.Key] = true
		prev, ok := before[e.Key]
		switch {
		case !ok:
			added = append(added, entryWord(e.Key, e.Value))
		case prev != e.Value:
			changed = append(changed, entryWord(e.Key, e.Value)+" (was `"+prev+"`)")
		}
	}
	for _, e := range old {
		if !after[e.Key] {
			removed = append(removed, entryWord(e.Key, e.Value))
		}
	}
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "adds "+strings.Join(added, ", "))
	}
	if len(changed) > 0 {
		parts = append(parts, "changes "+strings.Join(changed, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removes "+strings.Join(removed, ", "))
	}
	if len(parts) == 0 && !reflect.DeepEqual(old, new) && len(old) > 0 {
		parts = append(parts, "reorders its entries")
	}
	return parts
}

func entryWord(key, value string) string { return "`" + key + ": " + value + "`" }

// scalarWord is a scalar value as the file would show it.
func scalarWord(v any) string {
	if s, ok := v.(string); ok {
		return "`" + s + "`"
	}
	return fmt.Sprint(v)
}

// ReencodeWords says what a re-encode of graphvein.yaml (the fallback of
// EditSettings) changes beyond the edit and the indentation, comparing
// the file on disk (before) with the result (after): a byte order mark
// it drops, CRLF line endings it turns into LF, and every anchor
// (`&name`) of before the result no longer has. One line each, empty
// when none applies.
func ReencodeWords(before, after []byte) []string {
	var out []string
	if bytes.HasPrefix(before, []byte(utf8BOM)) && !bytes.HasPrefix(after, []byte(utf8BOM)) {
		out = append(out, "drops the byte order mark at the start of "+File)
	}
	if bytes.Contains(before, []byte("\r\n")) && !bytes.Contains(after, []byte("\r\n")) {
		out = append(out, "changes the line endings of "+File+" from CRLF to LF")
	}
	kept := map[string]int{}
	for _, a := range anchors(after) {
		kept[a.name]++
	}
	for _, a := range anchors(before) {
		if kept[a.name] > 0 {
			kept[a.name]--
			continue
		}
		out = append(out, fmt.Sprintf("drops the anchor &%s (line %d)", a.name, a.line))
	}
	return out
}

// anchor is an anchor's name and the line of the node it names.
type anchor struct {
	name string
	line int
}

// anchors lists the anchors of every node of data's documents. A text
// that does not parse has none to compare.
func anchors(data []byte) []anchor {
	var out []anchor
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Anchor != "" {
			out = append(out, anchor{name: n.Anchor, line: n.Line})
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		if d.Decode(&doc) != nil {
			return out
		}
		walk(&doc)
	}
}
