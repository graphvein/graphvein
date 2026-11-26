package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// ManifestMember is a member as `graphvein project init` and `project add`
// write it: a repository ID and a slash-separated path relative to the
// manifest's directory.
type ManifestMember struct {
	Repository string
	Path       string
}

// manifestHeader opens every manifest init writes, so a reader of the
// committed file knows where the format is described and where local
// paths go.
const manifestHeader = "Graphvein project manifest (docs/cli/project.md). Commit this file;\n" +
	"machine-specific member paths go in " + ProjectLocalFile + " (not committed)."

// NewProjectManifest returns the text of a new manifest. The caller
// validates it with ParseProject before writing, so the writer never needs
// its own rules.
func NewProjectManifest(name string, members []ManifestMember) ([]byte, error) {
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, m := range members {
		list.Content = append(list.Content, memberNode(m))
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: manifestHeader}
	root.Content = append(root.Content,
		str("version"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"},
		str("name"), str(name),
		str("members"), list)
	return encodeManifest(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
}

// AddProjectMember returns data with m appended to the members list. The
// rest of the document, comments included, is kept; yaml.v3 re-indents it
// with two spaces.
func AddProjectMember(data []byte, m ManifestMember) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing the manifest: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the manifest is not a mapping")
	}
	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value != "members" {
			continue
		}
		list := root.Content[i+1]
		if list.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("line %d: members must be a list", list.Line)
		}
		list.Content = append(list.Content, memberNode(m))
		return encodeManifest(&doc)
	}
	return nil, fmt.Errorf("the manifest has no members list")
}

func memberNode(m ManifestMember) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		str("repository"), str(m.Repository),
		str("path"), str(m.Path),
	}}
}

// str is a string scalar; the encoder quotes it when the plain form would
// read back as another type (for example "1" or "true").
func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func encodeManifest(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	e := yaml.NewEncoder(&buf)
	e.SetIndent(2)
	if err := e.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding the manifest: %w", err)
	}
	if err := e.Close(); err != nil {
		return nil, fmt.Errorf("encoding the manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// AddProjectConsume returns data with c appended to the consumes list of
// member (or of its scope named scope, when scope is set), creating the
// list when absent. Like AddProjectMember it keeps the rest of the
// document, comments included. It is the writer behind `graphvein project
// suggest --accept` (T022 B6) and the UI's confirmation of a suggestion
// (B10); the caller validates the result with ParseProject before writing,
// so a duplicate provider is refused there, not here.
func AddProjectConsume(data []byte, member, scope string, c Consume) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		owner, err := consumesOwner(root, member, scope)
		if err != nil {
			return err
		}
		list := mappingValue(owner, "consumes")
		if list == nil {
			list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			owner.Content = append(owner.Content, str("consumes"), list)
		}
		if list.Kind != yaml.SequenceNode {
			return fmt.Errorf("line %d: consumes must be a list", list.Line)
		}
		list.Content = append(list.Content, consumeNode(c))
		return nil
	})
}

// editManifest parses data, lets edit change the root mapping in place and
// encodes the result. Comments stay attached to the nodes they belong to,
// so an edit keeps every comment of the parts it does not remove.
func editManifest(data []byte, edit func(root *yaml.Node) error) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing the manifest: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the manifest is not a mapping")
	}
	if err := edit(doc.Content[0]); err != nil {
		return nil, err
	}
	return encodeManifest(&doc)
}

// findMember returns the mapping of member in the manifest's members list.
func findMember(root *yaml.Node, member string) (*yaml.Node, error) {
	members := mappingValue(root, "members")
	if members == nil || members.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("the manifest has no members list")
	}
	target := findByKey(members, "repository", member)
	if target == nil {
		return nil, fmt.Errorf("the manifest has no member %q", member)
	}
	return target, nil
}

// consumesOwner returns the mapping that holds the consumes list of member,
// or of its scope named scope when scope is set.
func consumesOwner(root *yaml.Node, member, scope string) (*yaml.Node, error) {
	target, err := findMember(root, member)
	if err != nil || scope == "" {
		return target, err
	}
	scopes := mappingValue(target, "scopes")
	if scopes == nil || scopes.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("member %q declares no scopes", member)
	}
	if target = findByKey(scopes, "name", scope); target == nil {
		return nil, fmt.Errorf("member %q declares no scope %q", member, scope)
	}
	return target, nil
}

// ConsumeTarget names one consumes entry of a list: the manifest allows one
// entry per (repository, scope, default_scope), so the triple is the
// entry's key.
type ConsumeTarget struct {
	Repository   string
	Scope        string
	DefaultScope bool
}

func (t ConsumeTarget) String() string {
	return consumeName(Consume{Repository: t.Repository, Scope: t.Scope, DefaultScope: t.DefaultScope})
}

// targetOf reads the key of a consumes entry node: a plain scalar names a
// whole repository.
func targetOf(item *yaml.Node) ConsumeTarget {
	if item.Kind == yaml.ScalarNode {
		return ConsumeTarget{Repository: item.Value}
	}
	var t ConsumeTarget
	if v := mappingValue(item, "repository"); v != nil {
		t.Repository = v.Value
	}
	if v := mappingValue(item, "scope"); v != nil {
		t.Scope = v.Value
	}
	if v := mappingValue(item, "default_scope"); v != nil && v.Value == "true" {
		t.DefaultScope = true
	}
	return t
}

// findConsume returns the owner mapping, its consumes list and the index
// of the entry named by target.
func findConsume(root *yaml.Node, member, scope string, target ConsumeTarget) (*yaml.Node, *yaml.Node, int, error) {
	owner, err := consumesOwner(root, member, scope)
	if err != nil {
		return nil, nil, 0, err
	}
	list := mappingValue(owner, "consumes")
	if list != nil && list.Kind == yaml.SequenceNode {
		for i, item := range list.Content {
			if targetOf(item) == target {
				return owner, list, i, nil
			}
		}
	}
	return nil, nil, 0, fmt.Errorf("%s has no consumes entry for %s", ownerName(member, scope), target)
}

func ownerName(member, scope string) string {
	if scope == "" {
		return "member " + member
	}
	return "scope " + member + "/" + scope
}

// RemoveProjectConsume returns data without the consumes entry target of
// member (or of its scope). An emptied list is removed with its key: the
// manifest refuses an empty consumes list, and no list means every member
// is a candidate again.
func RemoveProjectConsume(data []byte, member, scope string, target ConsumeTarget) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		owner, list, i, err := findConsume(root, member, scope, target)
		if err != nil {
			return err
		}
		list.Content = append(list.Content[:i], list.Content[i+1:]...)
		if len(list.Content) == 0 {
			removeKey(owner, "consumes")
		}
		return nil
	})
}

// SetProjectConsumeSelect replaces the select of the consumes entry target
// in place (an empty sel removes it, so the entry covers every call
// without a literal origin). A plain scalar entry becomes a mapping.
func SetProjectConsumeSelect(data []byte, member, scope string, target ConsumeTarget, sel Selector) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		_, list, i, err := findConsume(root, member, scope, target)
		if err != nil {
			return err
		}
		item := asMapping(list, i)
		removeKey(item, "select")
		if !sel.Empty() {
			item.Content = append(item.Content, str("select"), selectorNode(sel))
		}
		return nil
	})
}

// SetProjectConsumeDefaultScope sets or clears default_scope on member's
// (or its scope's) consumes entry for repository: on, the entry for the
// whole repository then names only its default scope; off, the reverse.
func SetProjectConsumeDefaultScope(data []byte, member, scope, repository string, on bool) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		_, list, i, err := findConsume(root, member, scope, ConsumeTarget{Repository: repository, DefaultScope: !on})
		if err != nil {
			return err
		}
		item := asMapping(list, i)
		if !on {
			removeKey(item, "default_scope")
			return nil
		}
		// default_scope goes right after repository, as consumeNode writes it.
		at := 2
		item.Content = append(item.Content[:at], append([]*yaml.Node{str("default_scope"), {Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}}, item.Content[at:]...)...)
		return nil
	})
}

// AddProjectScope appends the link scope s (name and member-relative
// path; its consumes are not written) to member's scopes list, creating
// the list when absent.
func AddProjectScope(data []byte, member string, s Scope) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		target, err := findMember(root, member)
		if err != nil {
			return err
		}
		list := mappingValue(target, "scopes")
		if list == nil {
			list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			target.Content = append(target.Content, str("scopes"), list)
		}
		if list.Kind != yaml.SequenceNode {
			return fmt.Errorf("line %d: scopes must be a list", list.Line)
		}
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			str("name"), str(s.Name), str("path"), str(s.Path)}})
		return nil
	})
}

// RemoveProjectScope removes member's link scope name with its consumes
// list. An entry elsewhere that names the scope makes the result invalid,
// which the caller's ParseProject reports.
func RemoveProjectScope(data []byte, member, name string) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		target, err := findMember(root, member)
		if err != nil {
			return err
		}
		list := mappingValue(target, "scopes")
		if list != nil && list.Kind == yaml.SequenceNode {
			for i, item := range list.Content {
				if v := mappingValue(item, "name"); v != nil && v.Value == name {
					list.Content = append(list.Content[:i], list.Content[i+1:]...)
					if len(list.Content) == 0 {
						removeKey(target, "scopes")
					}
					return nil
				}
			}
		}
		return fmt.Errorf("member %q declares no scope %q", member, name)
	})
}

// RenameProjectRepository renames member from to to: its repository value
// and every consumes entry, of any member or scope, that names it. The
// member's own graphvein.yaml still wins (ADR 0075 identity rule 4): the
// caller's ParseProject refuses a name that differs from it.
func RenameProjectRepository(data []byte, from, to string) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		target, err := findMember(root, from)
		if err != nil {
			return err
		}
		mappingValue(target, "repository").Value = to
		members := mappingValue(root, "members")
		rename := func(owner *yaml.Node) {
			list := mappingValue(owner, "consumes")
			if list == nil || list.Kind != yaml.SequenceNode {
				return
			}
			for _, item := range list.Content {
				switch {
				case item.Kind == yaml.ScalarNode && item.Value == from:
					item.Value = to
				case item.Kind == yaml.MappingNode:
					if v := mappingValue(item, "repository"); v != nil && v.Value == from {
						v.Value = to
					}
				}
			}
		}
		for _, m := range members.Content {
			rename(m)
			if scopes := mappingValue(m, "scopes"); scopes != nil && scopes.Kind == yaml.SequenceNode {
				for _, s := range scopes.Content {
					rename(s)
				}
			}
		}
		return nil
	})
}

// asMapping returns list's entry i as a mapping, turning a plain scalar
// entry (a repository name) into {repository: name} with its comments.
func asMapping(list *yaml.Node, i int) *yaml.Node {
	item := list.Content[i]
	if item.Kind == yaml.MappingNode {
		return item
	}
	// The line comment belongs on the value: the encoder writes a mapping's
	// own line comment after its last line.
	value := str(item.Value)
	value.LineComment = item.LineComment
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: item.Style & yaml.FlowStyle,
		HeadComment: item.HeadComment, FootComment: item.FootComment,
		Content: []*yaml.Node{str("repository"), value}}
	list.Content[i] = m
	return m
}

// removeKey deletes key and its value from mapping m.
func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

// consumeNode writes a consumes entry as a mapping; selector lists use the
// flow style of the documented example (`bases: [VITE_API]`).
func consumeNode(c Consume) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("repository"), str(c.Repository)}}
	if c.Scope != "" {
		n.Content = append(n.Content, str("scope"), str(c.Scope))
	}
	if c.DefaultScope {
		n.Content = append(n.Content, str("default_scope"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	if c.Select.Empty() {
		return n
	}
	n.Content = append(n.Content, str("select"), selectorNode(c.Select))
	return n
}

// selectorNode writes a select mapping with flow-style lists.
func selectorNode(s Selector) *yaml.Node {
	sel := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, l := range []struct {
		key    string
		values []string
	}{{"bases", s.Bases}, {"origins", s.Origins}, {"path_prefixes", s.PathPrefixes}} {
		if len(l.values) == 0 {
			continue
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, v := range l.values {
			seq.Content = append(seq.Content, str(v))
		}
		sel.Content = append(sel.Content, str(l.key), seq)
	}
	return sel
}

// mappingValue returns the value of key in mapping m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// findByKey returns the first mapping in list whose key has the string
// value want.
func findByKey(list *yaml.Node, key, want string) *yaml.Node {
	for _, item := range list.Content {
		if v := mappingValue(item, key); v != nil && v.Kind == yaml.ScalarNode && v.Value == want {
			return item
		}
	}
	return nil
}

// ScopePathValid reports whether s is a valid link scope path: a clean,
// relative, slash-separated folder inside the member. The server checks a
// scope folder from a UI edit with it before it looks at the filesystem.
func ScopePathValid(s string) bool { return scopePathValid(s) }

// WriteFileAtomic replaces path with data through a temporary file in the
// same directory, so a crash never leaves a half-written manifest. The CLI
// (project init, add, suggest --accept) and the server's manifest edit
// both write through it.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".graphvein-project-*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, 0o644)); err != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
