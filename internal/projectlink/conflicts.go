package projectlink

import (
	"sort"

	"github.com/graphvein/graphvein/internal/config"
)

// Conflict is a stored in-repository link that a link scope declaration
// contradicts (T022 B9, decision recorded in the task's B6b
// notes): a call its own index linked to a route of one scope, while the
// consumes entries of the call's consumer that cover it name other scopes
// of the same repository. Scopes never override an in-repository link
// (docs/extract/project-links.md, "Known gaps"), so a view flags the link
// as conflicting with the declaration instead of changing it.
type Conflict struct {
	// Call is the client call whose own index made the link.
	Call Endpoint `json:"call"`
	// Consumer is the declaration's consumer: repository/scope, or the
	// repository for a call in the member's default scope.
	Consumer string `json:"consumer"`
	// Declared are the covering entries naming the call's own repository,
	// worded as link assumptions word them ("`monorepo/web` consumes
	// `monorepo/orders` (bases: VITE_ORDERS)").
	Declared []string `json:"declared"`
	// Target is the route the in-repository link reaches; TargetScope its
	// declared scope, empty for the member's default scope.
	Target      Endpoint `json:"target"`
	TargetScope string   `json:"target_scope"`
}

// DeclarationConflicts lists the conflicts of in's members against their
// current declarations. targets returns the stored in-repository
// calls_route targets of the named calls of one repository. Only calls
// with one unconditional alternative are checked: the stored edges of a
// call with several alternatives do not say which alternative reached which
// route, so no conflict is claimed for them (a known gap, never a guess).
// Members without declared scopes, or whose consumers name no scope of
// their own repository, have none, and targets is not called for them.
func DeclarationConflicts(in Input, targets func(repository string, calls []string) (map[string][]string, error)) ([]Conflict, error) {
	out := []Conflict{}
	for i := range in.Members {
		m := &in.Members[i]
		if len(m.Scopes) == 0 {
			continue
		}
		type pending struct {
			node     string
			consumer string
			entries  []entryText
		}
		var calls []pending
		routes := map[string]string{}
		for _, n := range m.Nodes {
			if n.Kind == "route" {
				routes[n.Identity] = n.File
			}
		}
		for _, n := range m.Nodes {
			if n.Kind != "client_call" {
				continue
			}
			var a callAttributes
			if !decode(n.Attributes, &a) || a.Paths == nil || a.Method == nil || a.Status != "linked" {
				continue
			}
			paths := *a.Paths
			if len(paths) != 1 || paths[0].Conditional || paths[0].Template == nil {
				continue
			}
			s := callSource(m, n, a, paths[0], a.Base, a.BaseExpression)
			name, list := consumer(s)
			var own []entryText
			for _, c := range list {
				if c.Repository == m.Repository && (c.Scope != "" || c.DefaultScope) && covers(c, s, list) {
					own = append(own, entryText{entry: c, text: declarationText(name, c)})
				}
			}
			if len(own) > 0 {
				calls = append(calls, pending{node: n.Identity, consumer: name, entries: own})
			}
		}
		if len(calls) == 0 {
			continue
		}
		ids := make([]string, 0, len(calls))
		for _, c := range calls {
			ids = append(ids, c.node)
		}
		got, err := targets(m.Repository, ids)
		if err != nil {
			return nil, err
		}
		for _, c := range calls {
			for _, target := range distinctSorted(got[c.node]) {
				file, ok := routes[target]
				if !ok {
					continue
				}
				scope := ""
				if sc := scopeOf(m, file); sc != nil {
					scope = sc.Name
				}
				endpoint := &provider{member: m, scope: scope}
				named := false
				declared := make([]string, 0, len(c.entries))
				for _, e := range c.entries {
					named = named || names(e.entry, endpoint)
					declared = append(declared, e.text)
				}
				if !named {
					out = append(out, Conflict{Call: Endpoint{Repository: m.Repository, Identity: c.node}, Consumer: c.consumer, Declared: declared,
						Target: Endpoint{Repository: m.Repository, Identity: target}, TargetScope: scope})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Call != b.Call {
			return a.Call.Repository < b.Call.Repository || a.Call.Repository == b.Call.Repository && a.Call.Identity < b.Call.Identity
		}
		return a.Target.Identity < b.Target.Identity
	})
	return out, nil
}

// entryText is a covering consumes entry with its declaration wording.
type entryText struct {
	entry config.Consume
	text  string
}

func distinctSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// DeclarationText words consumer's consumes entry c as a declared link's
// assumption names it, without the "declared in graphvein-project.yaml: "
// lead: "`frontend` consumes `express/user` (bases: VITE_API)".
func DeclarationText(consumer string, c config.Consume) string {
	return declarationText(consumer, c)
}
