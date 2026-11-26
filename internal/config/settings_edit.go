package config

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// SettingOp is one typed edit of a form key: set Value (of the key's Go
// type: string, int, bool, []string or []MapEntry), or Remove the key so
// its built-in default applies again (or nothing, without a default).
type SettingOp struct {
	Key    string
	Remove bool
	Value  any
}

// EditSettings returns graphvein.yaml text (data; nil when the file does
// not exist) with ops applied in order, and whether the whole file had to
// be re-encoded (ADR 0088, row ST-27).
//
// Each operation is first made on the yaml.v3 node tree: that tree is the
// reference result. The text is then edited line-wise: only the lines of
// the edited entry change, the line comment of a replaced scalar is kept,
// and new lines take the file's line ending and the section's indentation.
// The spliced text is kept only when it decodes to exactly the reference
// tree (same keys in the same order, same tags and values); otherwise, or
// when the entry's lines hold a comment the splice would drop, the
// reference tree of all operations is encoded instead: comments stay
// attached to their nodes, but yaml.v3 re-indents the file with two spaces
// and drops blank lines, so reformatted is true and the caller says so.
// The caller validates the result with Parse before writing it.
func EditSettings(data []byte, ops []SettingOp) (out []byte, reformatted bool, err error) {
	cur := data
	if len(cur) == 0 {
		cur = []byte("version: 1\n")
	}
	for _, op := range ops {
		ref, err := editedTree(cur, op)
		if err != nil {
			return nil, false, err
		}
		if spliced, ok := spliceSetting(cur, op); ok && sameTree(spliced, ref) {
			cur = spliced
			continue
		}
		out, err := editConfig(data, func(root *yaml.Node) error {
			for _, op := range ops {
				if err := applySetting(root, op); err != nil {
					return err
				}
			}
			return nil
		})
		return out, true, err
	}
	return cur, false, nil
}

// editedTree is the document of text with op applied to its node tree.
func editedTree(text []byte, op SettingOp) (*yaml.Node, error) {
	root, err := settingsRoot(text)
	if err != nil {
		return nil, err
	}
	if err := applySetting(root, op); err != nil {
		return nil, err
	}
	return root, nil
}

// settingPath is where op's value lives in root: [section] for a
// shorthand scalar (or a shorthand key the file does not set yet, written
// as `ide: cursor`), else [section, key].
func settingPath(root *yaml.Node, k SettingKey) []string {
	if k.Shorthand {
		if sec := mappingValue(root, k.section()); sec == nil || sec.Kind == yaml.ScalarNode {
			return []string{k.section()}
		}
	}
	return []string{k.section(), k.leaf()}
}

// applySetting makes op on the node tree of root.
func applySetting(root *yaml.Node, op SettingOp) error {
	k, ok := SettingKeyByName(op.Key)
	if !ok {
		return fmt.Errorf("%s is not a key the Settings form edits", op.Key)
	}
	sec := mappingValue(root, k.section())
	if k.LockedBy != "" && sec != nil && hasKey(sec, k.LockedBy) {
		return fmt.Errorf("%s.%s is set, so %s does not decide the result; edit the YAML", k.section(), k.LockedBy, k.Key)
	}
	if sec != nil && sec.Kind != yaml.MappingNode && !(k.Shorthand && sec.Kind == yaml.ScalarNode) {
		return fmt.Errorf("line %d: %s must be a mapping", sec.Line, k.section())
	}
	path := settingPath(root, k)
	if op.Remove {
		if _, v := lookupSetting(root, k); v == nil {
			return fmt.Errorf("%s does not set %s", File, k.Key)
		}
		if len(path) == 1 {
			removeKey(root, path[0])
			return nil
		}
		removeKey(sec, path[1])
		if len(sec.Content) == 0 {
			removeKey(root, path[0])
		}
		return nil
	}
	_, old := lookupSetting(root, k)
	n, err := settingNode(k, op.Value, old != nil && old.Style&yaml.FlowStyle != 0)
	if err != nil {
		return err
	}
	n = mergeCollection(old, n)
	if len(path) == 1 {
		setNode(root, path[0], n)
		return nil
	}
	if sec == nil {
		sec = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, str(path[0]), sec)
	}
	setNode(sec, path[1], n)
	return nil
}

// setNode sets key in mapping m to n, appending it when absent. A
// replaced value keeps the comments yaml.v3 attached to it.
func setNode(m *yaml.Node, key string, n *yaml.Node) {
	if _, v := mappingEntry(m, key); v != nil {
		head, line, foot := v.HeadComment, v.LineComment, v.FootComment
		*v = *n
		v.HeadComment, v.LineComment, v.FootComment = head, line, foot
		return
	}
	m.Content = append(m.Content, str(key), n)
}

// textLines is a file split into lines, each with its own terminator
// (the last may have none).
type textLines struct {
	lines []string
	eol   string
	// step is the file's indentation step, used for new lines.
	step int
}

func splitText(b []byte) textLines {
	t := textLines{eol: "\n"}
	s := string(b)
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			t.lines = append(t.lines, s)
			break
		}
		t.lines = append(t.lines, s[:i+1])
		s = s[i+1:]
	}
	if len(t.lines) > 0 && strings.HasSuffix(t.lines[0], "\r\n") {
		t.eol = "\r\n"
	}
	return t
}

func (t textLines) bytes() []byte { return []byte(strings.Join(t.lines, "")) }

// trimmedEnd moves end (exclusive) back over blank and comment-only lines,
// which belong to what follows (a head comment) or to nothing.
func (t textLines) trimmedEnd(start, end int) int {
	for end > start+1 {
		s := strings.TrimSpace(t.lines[end-1])
		if s != "" && !strings.HasPrefix(s, "#") {
			break
		}
		end--
	}
	return end
}

// entryRange is the line range [start, end) of the i-th entry (key index
// i) of the block mapping m, within limit.
func (t textLines) entryRange(m *yaml.Node, i, limit int) (int, int) {
	start, end := m.Content[i].Line-1, limit
	if i+2 < len(m.Content) {
		end = m.Content[i+2].Line - 1
	}
	return start, t.trimmedEnd(start, end)
}

// hasComment reports whether a line in [start, end) may hold a comment.
func (t textLines) hasComment(start, end int) bool {
	for _, l := range t.lines[start:end] {
		if strings.Contains(l, "#") {
			return true
		}
	}
	return false
}

// replace swaps lines [start, end) for repl, giving the line before an
// insertion a terminator when the file ended without one.
func (t *textLines) replace(start, end int, repl []string) {
	if start == end && start > 0 && !strings.HasSuffix(t.lines[start-1], "\n") {
		t.lines[start-1] += t.eol
	}
	out := append([]string{}, t.lines[:start]...)
	out = append(out, repl...)
	t.lines = append(out, t.lines[end:]...)
}

// keyIndex is the content index of key in mapping m; -1 when absent.
func keyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func block(n *yaml.Node) bool {
	return n.Kind == yaml.MappingNode && n.Style&yaml.FlowStyle == 0
}

// spliceSetting edits text line-wise for op; ok is false when it cannot
// (a flow mapping, a comment in the lines it would replace, an empty
// section), so the caller re-encodes.
func spliceSetting(text []byte, op SettingOp) ([]byte, bool) {
	k, found := SettingKeyByName(op.Key)
	root, err := settingsRoot(text)
	if !found || err != nil || root == nil || !block(root) || len(root.Content) == 0 {
		return nil, false
	}
	t := splitText(text)
	t.step = indentStep(root)
	path := settingPath(root, k)
	si := keyIndex(root, path[0])
	var n *yaml.Node
	if !op.Remove {
		_, old := lookupSetting(root, k)
		if n, err = settingNode(k, op.Value, old != nil && old.Style&yaml.FlowStyle != 0); err != nil {
			return nil, false
		}
	}
	if si < 0 {
		// A new section (or shorthand) after the last root entry.
		if op.Remove {
			return nil, false
		}
		if len(path) == 2 {
			n = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str(path[1]), n}}
		}
		last := len(root.Content) - 2
		lastStart, end := t.entryRange(root, last, len(t.lines))
		lines, ok := t.entryLines(path[0], n, 0)
		if !ok {
			return nil, false
		}
		if lastStart > 0 && strings.TrimSpace(t.lines[lastStart-1]) == "" {
			// The file separates its sections with a blank line.
			lines = append([]string{t.eol}, lines...)
		}
		t.replace(end, end, lines)
		return t.bytes(), true
	}
	sStart, sEnd := t.entryRange(root, si, len(t.lines))
	parent, i, limit := root, si, len(t.lines)
	if len(path) == 2 {
		sec := root.Content[si+1]
		if !block(sec) || len(sec.Content) == 0 {
			return nil, false
		}
		li := keyIndex(sec, path[1])
		if li < 0 {
			if op.Remove {
				return nil, false
			}
			lines, ok := t.entryLines(path[1], n, sec.Content[0].Column-1)
			if !ok {
				return nil, false
			}
			t.replace(sEnd, sEnd, lines)
			return t.bytes(), true
		}
		if op.Remove && len(sec.Content) == 2 {
			// The section's only key: the section goes too, with the
			// comments inside its lines (they belong to what is removed).
			t.remove(sStart, sEnd)
			return t.bytes(), true
		}
		parent, i, limit = sec, li, sEnd
	}
	start, end := t.entryRange(parent, i, limit)
	key, value := parent.Content[i], parent.Content[i+1]
	if op.Remove {
		// Comments inside the entry's lines go with it; trailing comment
		// lines were trimmed off the range and stay.
		t.remove(start, end)
		return t.bytes(), true
	}
	if (value.Kind == yaml.SequenceNode || value.Kind == yaml.MappingNode) && value.Line > key.Line {
		// A block list or mapping: edited entry by entry.
		if lines, ok := t.spliceEntries(value, n, start+1, end); ok {
			t.replace(start+1, end, lines)
			return t.bytes(), true
		}
	}
	if line, ok := inlineValue(t.lines[start], key, value, n); ok && end == start+1 {
		t.lines[start] = line
		return t.bytes(), true
	}
	if t.hasComment(start, end) {
		return nil, false
	}
	lines, ok := t.entryLines(key.Value, n, key.Column-1)
	if !ok {
		return nil, false
	}
	t.replace(start, end, lines)
	return t.bytes(), true
}

// inlineValue replaces a value written on its key's line in place,
// keeping the key, its spacing and the line comment: a scalar, or a flow
// list or mapping replaced by one that encodes on one line.
func inlineValue(line string, key, value, n *yaml.Node) (string, bool) {
	scalar := value.Kind == yaml.ScalarNode && n.Kind == yaml.ScalarNode && value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0
	flow := value.Kind == n.Kind && value.Style&yaml.FlowStyle != 0 && n.Style&yaml.FlowStyle != 0
	if value.Line != key.Line || !scalar && !flow {
		return "", false
	}
	body := strings.TrimRight(line, "\r\n")
	eol := line[len(body):]
	at := value.Column - 1
	if at <= 0 || at > len(body) {
		return "", false
	}
	enc, ok := encodeOneLine(n)
	if !ok {
		return "", false
	}
	suffix := ""
	comment := value.LineComment
	if comment == "" {
		comment = key.LineComment
	}
	if comment != "" {
		c := strings.LastIndex(body, comment)
		if c < at {
			return "", false
		}
		for c > at && (body[c-1] == ' ' || body[c-1] == '\t') {
			c--
		}
		suffix = body[c:]
	}
	return body[:at] + enc + suffix + eol, true
}

// encodeOneLine is the YAML text of a scalar or flow node when it fits
// on one line.
func encodeOneLine(n *yaml.Node) (string, bool) {
	b, err := encodeManifest(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}})
	if err != nil {
		return "", false
	}
	s := strings.TrimSuffix(string(b), "\n")
	return s, s != "" && !strings.ContainsAny(s, "\n")
}

// entryLines encodes `key: n` indented by indent spaces with the file's
// indentation step inside, each line ending with the file's line ending.
func (t textLines) entryLines(key string, n *yaml.Node, indent int) ([]string, bool) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str(key), n}}
	var buf bytes.Buffer
	e := yaml.NewEncoder(&buf)
	e.SetIndent(t.step)
	if e.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m}}) != nil || e.Close() != nil || indent < 0 {
		return nil, false
	}
	var out []string
	pad := strings.Repeat(" ", indent)
	for _, l := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		out = append(out, pad+l+t.eol)
	}
	return out, true
}

// remove deletes the entry at lines [start, end) with the comment lines
// directly above it (its head comment, which the node tree drops with
// it) unless they open the file, and one blank line when blank lines
// would otherwise meet.
func (t *textLines) remove(start, end int) {
	head := start
	for head > 0 && strings.HasPrefix(strings.TrimSpace(t.lines[head-1]), "#") {
		head--
	}
	if head > 0 {
		// Comment lines at the very top are the file's header, kept.
		start = head
	}
	blank := func(i int) bool { return strings.TrimSpace(t.lines[i]) == "" }
	if end < len(t.lines) && blank(end) && (start == 0 || blank(start-1)) {
		end++
	}
	t.replace(start, end, nil)
}

// indentStep is the indentation of the first nested block mapping key
// relative to its section's key: the file's step (2 when there is none).
func indentStep(root *yaml.Node) int {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if v := root.Content[i+1]; block(v) && len(v.Content) > 0 {
			if step := v.Content[0].Column - root.Content[i].Column; step >= 2 && step <= 8 {
				return step
			}
		}
	}
	return 2
}

// sameTree reports whether text decodes to exactly the tree want: one
// document, the same kinds, resolved tags and values, keys in the same
// order. Styles, comments and positions may differ.
func sameTree(text []byte, want *yaml.Node) bool {
	d := yaml.NewDecoder(bytes.NewReader(text))
	var doc, extra yaml.Node
	if d.Decode(&doc) != nil || d.Decode(&extra) == nil || len(doc.Content) != 1 {
		return false
	}
	return nodeEqual(doc.Content[0], want)
}

func nodeEqual(a, b *yaml.Node) bool {
	if a.Kind != b.Kind || a.Kind == yaml.AliasNode || a.ShortTag() != b.ShortTag() || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode && a.Value != b.Value {
		return false
	}
	for i := range a.Content {
		if !nodeEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}
