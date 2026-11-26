package config

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// SetExplorerPathValue returns the text of graphvein.yaml (data; nil when
// the file does not exist) with explorer.path_values holding value for key:
// the entry for key is replaced, or appended when there is none, and the
// explorer section and its list are created when absent. The rest of the
// document, comments included, is kept; yaml.v3 re-indents it with two
// spaces, as the project manifest writer does. The caller validates the
// result with Parse before writing it.
//
// registration, when not empty, is written as the entry's registration
// (the route's content hash, for a key with a declaration-order ordinal);
// an empty one removes a stale one.
func SetExplorerPathValue(data []byte, key, value, registration string) ([]byte, error) {
	return editConfig(data, func(root *yaml.Node) error {
		list, err := pathValuesList(root, true)
		if err != nil {
			return err
		}
		item := findByKey(list, "key", key)
		if item == nil {
			item = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("key"), str(key)}}
			list.Content = append(list.Content, item)
		}
		setField(item, "value", value)
		if registration != "" {
			setField(item, "registration", registration)
		} else {
			removeKey(item, "registration")
		}
		return nil
	})
}

// setField sets key in mapping m to a string, appending it when absent.
func setField(m *yaml.Node, key, value string) {
	if v := mappingValue(m, key); v != nil {
		*v = *str(value)
		return
	}
	m.Content = append(m.Content, str(key), str(value))
}

// RemoveExplorerPathValue returns data without the explorer.path_values
// entry for key; an emptied list and section are removed too, so setting
// and removing a value gives the file back.
func RemoveExplorerPathValue(data []byte, key string) ([]byte, error) {
	return editConfig(data, func(root *yaml.Node) error {
		list, err := pathValuesList(root, false)
		if err != nil {
			return err
		}
		if list == nil || findByKey(list, "key", key) == nil {
			return fmt.Errorf("explorer.path_values has no entry for %s", key)
		}
		kept := list.Content[:0]
		for _, item := range list.Content {
			if v := mappingValue(item, "key"); v != nil && v.Value == key {
				continue
			}
			kept = append(kept, item)
		}
		list.Content = kept
		if len(list.Content) == 0 {
			explorer := mappingValue(root, "explorer")
			removeKey(explorer, "path_values")
			if len(explorer.Content) == 0 {
				removeKey(root, "explorer")
			}
		}
		return nil
	})
}

// pathValuesList returns the explorer.path_values sequence of root,
// creating the section and the list when create is set.
func pathValuesList(root *yaml.Node, create bool) (*yaml.Node, error) {
	explorer := mappingValue(root, "explorer")
	if explorer == nil {
		if !create {
			return nil, nil
		}
		explorer = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, str("explorer"), explorer)
	}
	if explorer.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: explorer must be a mapping", explorer.Line)
	}
	list := mappingValue(explorer, "path_values")
	if list == nil {
		if !create {
			return nil, nil
		}
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		explorer.Content = append(explorer.Content, str("path_values"), list)
	}
	if list.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: explorer.path_values must be a list", list.Line)
	}
	return list, nil
}

// editConfig parses graphvein.yaml text, lets edit change the root mapping
// and encodes the result. A missing file (nil or empty data) starts from
// `version: 1`, which keeps the repository identity a file without a
// repository key gives (the directory name).
func editConfig(data []byte, edit func(root *yaml.Node) error) ([]byte, error) {
	var doc yaml.Node
	if len(data) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			str("version"), {Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"},
		}}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	} else if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", File, err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is not a mapping", File)
	}
	if err := edit(doc.Content[0]); err != nil {
		return nil, err
	}
	return encodeManifest(&doc)
}
