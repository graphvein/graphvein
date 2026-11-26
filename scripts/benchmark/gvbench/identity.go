package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// publicTree returns the Git tree id of the public snapshot of a commit:
// the commit's tree minus the paths listed in its
// scripts/public-exclude.txt, with docs/public/AGENTS.md moved to the root
// as AGENTS.md, exactly what scripts/public-snapshot.sh exports (ADR 0080).
// The private repository's commits never become public, but a public commit
// made from that snapshot has this same tree id (`git rev-parse <public
// commit>^{tree}`), so a recorded tree id names the public commit that holds
// the same code.
//
// It needs no git process: lsTree is the output of `git ls-tree -r -z
// <commit>` and exclude the commit's public-exclude.txt; the trees are
// hashed as Git does (SHA-1 object format). Submodule entries are left out,
// as `git archive` exports them as empty directories, which Git does not
// record.
func publicTree(lsTree, exclude []byte) (string, error) {
	var excluded []string
	for _, line := range strings.Split(string(exclude), "\n") {
		entry, _, _ := strings.Cut(line, "#")
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "/") || strings.Contains(entry, "..") {
			return "", fmt.Errorf("bad entry in public-exclude.txt: %s", entry)
		}
		excluded = append(excluded, strings.TrimSuffix(entry, "/"))
	}
	root := &treeNode{children: map[string]*treeNode{}}
	sawPublicAgents := false
	for _, rec := range strings.Split(string(lsTree), "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return "", fmt.Errorf("unexpected ls-tree record %q (use git ls-tree -r -z)", rec)
		}
		mode, kind, id := f[0], f[1], f[2]
		if kind == "commit" {
			continue
		}
		if kind != "blob" || len(id) != 40 {
			return "", fmt.Errorf("unexpected ls-tree entry %q (SHA-1 repositories only)", rec)
		}
		if isExcluded(path, excluded) {
			continue
		}
		if path == "docs/public/AGENTS.md" {
			path, sawPublicAgents = "AGENTS.md", true
		}
		raw, err := hex.DecodeString(id)
		if err != nil {
			return "", fmt.Errorf("bad object id in %q", rec)
		}
		if err := root.add(strings.Split(path, "/"), mode, raw); err != nil {
			return "", err
		}
	}
	if !sawPublicAgents {
		return "", fmt.Errorf("the commit has no docs/public/AGENTS.md")
	}
	return hex.EncodeToString(root.hash()), nil
}

// isExcluded applies `rm -rf <entry>`: the path itself or anything below it.
func isExcluded(path string, excluded []string) bool {
	for _, e := range excluded {
		if path == e || strings.HasPrefix(path, e+"/") {
			return true
		}
	}
	return false
}

// treeNode is a directory being rebuilt; leaves carry a mode and blob id.
type treeNode struct {
	mode     string
	id       []byte
	children map[string]*treeNode
}

func (n *treeNode) add(parts []string, mode string, id []byte) error {
	name := parts[0]
	if len(parts) == 1 {
		if _, dup := n.children[name]; dup {
			return fmt.Errorf("duplicate path element %q", name)
		}
		n.children[name] = &treeNode{mode: mode, id: id}
		return nil
	}
	c := n.children[name]
	if c == nil {
		c = &treeNode{children: map[string]*treeNode{}}
		n.children[name] = c
	} else if c.children == nil {
		return fmt.Errorf("%q is both a file and a directory", name)
	}
	return c.add(parts[1:], mode, id)
}

// hash writes the tree object as Git does: entries sorted by name, with a
// directory compared as its name plus "/", each "mode name\0<20-byte id>".
func (n *treeNode) hash() []byte {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	key := func(name string) string {
		if n.children[name].children != nil {
			return name + "/"
		}
		return name
	}
	sort.Slice(names, func(i, j int) bool { return key(names[i]) < key(names[j]) })
	var body bytes.Buffer
	for _, name := range names {
		c := n.children[name]
		if c.children != nil {
			body.WriteString("40000 " + name + "\x00")
			body.Write(c.hash())
			continue
		}
		body.WriteString(c.mode + " " + name + "\x00")
		body.Write(c.id)
	}
	h := sha1.New()
	fmt.Fprintf(h, "tree %d\x00", body.Len())
	h.Write(body.Bytes())
	return h.Sum(nil)
}

// codexVisibleCatalog rewrites the model catalog that `codex debug models
// --bundled` prints so that no model offers the search tool. Codex 0.171.0
// defers every MCP tool behind tool search when the model supports it
// (feature tool_search_always_defer_mcp_tools, stage "removed", fixed on;
// `--disable` cannot turn it off). With supports_search_tool false the
// graphvein tools are exposed directly instead. Every other field is kept
// as printed.
func codexVisibleCatalog(data []byte) ([]byte, int, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cat map[string]any
	if err := dec.Decode(&cat); err != nil {
		return nil, 0, fmt.Errorf("parsing the Codex model catalog: %w", err)
	}
	models, ok := cat["models"].([]any)
	if !ok || len(models) == 0 {
		return nil, 0, fmt.Errorf("the Codex model catalog has no models")
	}
	for i, m := range models {
		obj, ok := m.(map[string]any)
		if !ok {
			return nil, 0, fmt.Errorf("model %d of the Codex catalog is not an object", i)
		}
		obj["supports_search_tool"] = false
	}
	return []byte(marshalIndent(cat)), len(models), nil
}
