package angular

import (
	"sort"
	"strings"
)

// Compilation scopes consume the existing declaration leaves after duplicate
// suppression. Their counts retain uncertainty that has no corresponding edge.
type compilationScope struct {
	status string
	reason string
	local  map[string]bool
	units  []pipeUnit
}

type pipeUnit struct {
	open  bool
	local map[string]bool
}

// pipeProvenAfterOpen applies rule P to the first unit contributing this class.
// A later repeat cannot move a pipe past an external contributor in Angular.
func (s compilationScope) pipeProvenAfterOpen(id string) bool {
	first, lastOpen := -1, -1
	for i, unit := range s.units {
		if unit.open {
			lastOpen = i
		}
		if first < 0 && unit.local[id] {
			first = i
		}
	}
	return first >= 0 && lastOpen < first
}

func (b *scopeBuilder) pipeUnits(d *declaration, declared bool) []pipeUnit {
	units := []pipeUnit{}
	seen := map[string]bool{}
	for _, target := range d.orderedImports {
		if strings.HasPrefix(target, "external:") {
			units = append(units, pipeUnit{open: true})
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		t := b.l.classes[target]
		if t == nil {
			continue
		}
		if t.role == "ng_module" {
			exported := b.module(target).exports
			units = append(units, pipeUnit{open: exported.status == "open", local: exported.local})
		} else {
			units = append(units, pipeUnit{local: map[string]bool{target: true}})
		}
	}
	if declared {
		local := map[string]bool{}
		for _, target := range b.leaves[d.identity]["declarations"] {
			local[target] = true
		}
		units = append(units, pipeUnit{local: local})
	}
	return units
}

func closedScope() compilationScope {
	return compilationScope{status: "complete", local: map[string]bool{}}
}

func (s *compilationScope) degrade(status, reason string) {
	rank := map[string]int{"complete": 0, "open": 1, "unknown": 2, "invalid": 3}
	if rank[status] > rank[s.status] {
		s.status, s.reason = status, reason
	}
}

func (s *compilationScope) merge(other compilationScope) {
	for id := range other.local {
		s.local[id] = true
	}
	s.degrade(other.status, other.reason)
}

type moduleScopes struct{ compilation, exports compilationScope }

type scopeBuilder struct {
	l        *linker
	leaves   map[string]map[string][]string
	owners   map[string][]string
	modules  map[string]moduleScopes
	visiting map[string]bool
}

func newScopeBuilder(l *linker) *scopeBuilder {
	b := &scopeBuilder{l: l, leaves: map[string]map[string][]string{}, owners: map[string][]string{}, modules: map[string]moduleScopes{}, visiting: map[string]bool{}}
	for _, e := range l.edges {
		key := map[string]string{"declares": "declarations", "scope_imports": "imports", "scope_exports": "exports"}[e.Kind]
		if key == "" {
			continue
		}
		if b.leaves[e.Source] == nil {
			b.leaves[e.Source] = map[string][]string{}
		}
		b.leaves[e.Source][key] = append(b.leaves[e.Source][key], e.Target)
		if key == "declarations" {
			seen := false
			for _, owner := range b.owners[e.Target] {
				if owner == e.Source {
					seen = true
				}
			}
			if !seen {
				b.owners[e.Target] = append(b.owners[e.Target], e.Source)
			}
		}
	}
	return b
}

func (b *scopeBuilder) leafScope(d *declaration, key string) compilationScope {
	s := closedScope()
	if counts, ok := d.attrs[key].(map[string]any); ok {
		if counts["external"].(int) > 0 {
			s.degrade("open", "external entry")
		}
		if counts["unresolved"].(int) > 0 {
			s.degrade("unknown", "unresolved entry")
		}
		if counts["invalid"].(int) > 0 {
			s.degrade("invalid", "invalid entry or duplicate declaration")
		}
	}
	return s
}

func (b *scopeBuilder) module(id string) moduleScopes {
	if s, ok := b.modules[id]; ok {
		return s
	}
	if b.visiting[id] {
		s := closedScope()
		s.degrade("invalid", "cycle")
		return moduleScopes{s, s}
	}
	b.visiting[id] = true
	d := b.l.classes[id]
	c := b.leafScope(d, "imports")
	c.merge(b.leafScope(d, "declarations"))
	for _, target := range b.leaves[id]["declarations"] {
		if b.l.classes[target] != nil {
			c.local[target] = true
		}
	}
	for _, target := range b.leaves[id]["imports"] {
		if t := b.l.classes[target]; t != nil {
			if t.role == "ng_module" {
				c.merge(b.module(target).exports)
			} else {
				c.local[target] = true
			}
		} else if strings.HasPrefix(target, "external:") {
			c.degrade("open", "external entry")
		}
	}
	e := b.leafScope(d, "exports")
	// Compilation failures also affect exports, including empty exports. An
	// external import alone does not re-export its declarations.
	if c.status == "invalid" || c.status == "unknown" {
		e.degrade(c.status, c.reason)
	}
	for _, target := range b.leaves[id]["exports"] {
		if t := b.l.classes[target]; t != nil {
			if t.role == "ng_module" {
				e.merge(b.module(target).exports)
			} else if c.local[target] {
				e.local[target] = true
			} else {
				e.degrade("invalid", "export outside compilation scope")
			}
		} else if strings.HasPrefix(target, "external:") {
			e.degrade("open", "external entry")
		}
	}
	// Invalid exports poison component compilation scopes as Angular does.
	if e.status == "invalid" {
		c.degrade(e.status, e.reason)
	}
	delete(b.visiting, id)
	b.modules[id] = moduleScopes{c, e}
	return b.modules[id]
}

func (b *scopeBuilder) host(d *declaration) compilationScope {
	s := closedScope()
	switch d.attrs["standalone"] {
	case true:
		s.local[d.identity] = true
		s.merge(b.leafScope(d, "imports"))
		for _, target := range b.leaves[d.identity]["imports"] {
			if t := b.l.classes[target]; t != nil {
				if t.role == "ng_module" {
					s.merge(b.module(target).exports)
				} else {
					s.local[target] = true
				}
			}
		}
		if _, exists := d.metadata["deferredImports"]; exists {
			s.degrade("unknown", "deferredImports")
		}
		s.units = b.pipeUnits(d, false)
	case false:
		owners := b.owners[d.identity]
		if len(owners) == 1 {
			s = b.module(owners[0]).compilation
			s.units = b.pipeUnits(b.l.classes[owners[0]], true)
			return s
		}
		s.status, s.reason = "none", "no declaring module or duplicate declaration"
	default:
		s.status, s.reason = "none", "standalone unknown"
	}
	return s
}

func sortedScope(s compilationScope) []string {
	ids := make([]string, 0, len(s.local))
	for id := range s.local {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
