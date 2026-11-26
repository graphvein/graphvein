package gorm

import "sort"

// Expansion is a top-level call statement, in a function with migrate
// sites, of a module function with migrate sites: the order rule expands
// it in place.
type Expansion struct {
	// File, Func and Line locate the statement; Statement is its index in
	// the body.
	File, Func      string
	Line, Statement int
	// Call is the callee as written ("users.AutoMigrate"); Callee its key
	// ("<file>#<Func>").
	Call, Callee string
}

// order applies the order rule (row GO-K3, open point O6): within one
// function, migrate sites run in statement order when they sit in an
// unconditional top-level statement or an if header; a top-level call
// statement of a module function that holds migrate sites expands in place
// (depth 2: the expanded function's own call statements are not expanded).
// A root is a function with migrate sites that no other such function
// expands; each root gives one sequence. The order is proven when there is
// one sequence and every migrate site is in it.
func (r *resolver) order() {
	m := r.m
	expanded := map[string]bool{}
	for _, key := range r.keys {
		info := r.funcs[key]
		if len(info.sites) == 0 {
			continue
		}
		for _, st := range info.fn.Statements {
			dir, pkg, name, _, ok := r.scope(info.file, st.Callee)
			if !ok {
				continue
			}
			fn, file := r.function(dir, pkg, name)
			if fn == nil {
				continue
			}
			callee := file + "#" + funcName(fn)
			if c := r.funcs[callee]; c != nil && callee != key && len(c.sites) > 0 {
				info.calls = append(info.calls, stmtCall{key: callee, index: st.Index})
				expanded[callee] = true
				call := name
				if st.Callee.Object != nil {
					call = st.Callee.Object.Name + "." + name
				}
				m.Expansions = append(m.Expansions, Expansion{File: info.file, Func: funcName(info.fn), Line: st.Line, Statement: st.Index, Call: call, Callee: callee})
			}
		}
	}
	for _, key := range r.keys {
		info := r.funcs[key]
		if len(info.sites) == 0 || expanded[key] {
			continue
		}
		m.Sequences = append(m.Sequences, r.sequence(info, len(m.Sequences)))
	}
	m.OrderProven = len(m.Sequences) == 1
	for _, s := range m.Sites {
		if s.Position < 0 {
			m.OrderProven = false
		}
	}
}

// sequence builds one root's sequence: its ordered sites and the ordered
// sites of the functions its call statements expand, by statement index
// (a statement's own sites before the call it makes).
func (r *resolver) sequence(root *funcInfo, n int) []*MigrateSite {
	type item struct {
		index, rank int
		sites       []*MigrateSite
	}
	var items []item
	for i, s := range root.sites {
		if s.Ordered() {
			items = append(items, item{index: s.Statement, rank: i, sites: []*MigrateSite{s}})
		}
	}
	for _, c := range root.calls {
		var sites []*MigrateSite
		for _, s := range r.funcs[c.key].sites {
			if s.Ordered() {
				sites = append(sites, s)
			}
		}
		items = append(items, item{index: c.index, rank: len(root.sites), sites: sites})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].index != items[j].index {
			return items[i].index < items[j].index
		}
		return items[i].rank < items[j].rank
	})
	var out []*MigrateSite
	for _, it := range items {
		for _, s := range it.sites {
			if s.Sequence < 0 {
				s.Sequence, s.Position = n, len(out)
			}
			out = append(out, s)
		}
	}
	return out
}
