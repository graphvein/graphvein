package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds what the web UI's Project view reads beyond B8's
// /api/v0/project body (T022 B9): the manifest's declarations, the pass's
// ambiguous sources grouped by their candidate providers, and the stored
// in-repository links a link scope declaration contradicts.

// projectSourcesCap bounds the sources listed per ambiguity group, so a
// large project's response stays small; Count always has the full number.
const projectSourcesCap = 50

// projectDeclarationsDoc is what the manifest declares. Empty lists mean
// the default rules: every member's routes are candidates for every
// member's calls.
type projectDeclarationsDoc struct {
	// Consumes are the consumes entries of members and of their link
	// scopes, worded as link assumptions word them.
	Consumes []projectConsumeDoc `json:"consumes"`
	Scopes   []projectScopeDoc   `json:"scopes"`
}

type projectConsumeDoc struct {
	// Consumer is the member, or repository/scope for a scope's entry.
	Consumer string `json:"consumer"`
	// Provider is the repository the entry names; Scope its scope (empty
	// for the whole repository), DefaultScope set for default_scope: true.
	Provider     string `json:"provider"`
	Scope        string `json:"scope"`
	DefaultScope bool   `json:"default_scope"`
	Text         string `json:"text"`
}

type projectScopeDoc struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

// projectNodeBrief names a node by (repository, identity) with what a list
// shows of it; Name, File and Line are empty when the member no longer
// stores the node.
type projectNodeBrief struct {
	Repository string `json:"repository"`
	Identity   string `json:"identity"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// projectAmbiguousDoc lists the pass's ambiguous sources: never a link.
type projectAmbiguousDoc struct {
	Total  int                     `json:"total"`
	Groups []projectAmbiguityGroup `json:"groups"`
}

// projectAmbiguityGroup holds the ambiguous sources of one claim row with
// the same candidate providers.
type projectAmbiguityGroup struct {
	// Row is the claim contract row (PL-R10 one provider with tied routes,
	// PL-R11 several providers, PL-S3 an in-repository tie the scope
	// declarations do not resolve).
	Row string `json:"row"`
	// Providers are the repositories of the candidate routes, sorted.
	Providers []string                 `json:"providers"`
	Count     int                      `json:"count"`
	Sources   []projectAmbiguousSource `json:"sources"`
}

type projectAmbiguousSource struct {
	Source         projectNodeBrief   `json:"source"`
	Template       string             `json:"template"`
	CandidateCount int                `json:"candidate_count"`
	Candidates     []projectNodeBrief `json:"candidates"`
}

// projectConflictDoc is projectlink.Conflict with its nodes described.
type projectConflictDoc struct {
	Call        projectNodeBrief `json:"call"`
	Consumer    string           `json:"consumer"`
	Declared    []string         `json:"declared"`
	Target      projectNodeBrief `json:"target"`
	TargetScope string           `json:"target_scope"`
}

// declarationsOf lists the manifest's consumes entries and link scopes.
func declarationsOf(project config.Project) projectDeclarationsDoc {
	out := projectDeclarationsDoc{Consumes: []projectConsumeDoc{}, Scopes: []projectScopeDoc{}}
	add := func(consumer string, list []config.Consume) {
		for _, c := range list {
			out.Consumes = append(out.Consumes, projectConsumeDoc{Consumer: consumer, Provider: c.Repository, Scope: c.Scope, DefaultScope: c.DefaultScope,
				Text: projectlink.DeclarationText(consumer, c)})
		}
	}
	for _, m := range project.Members {
		add(m.Repository, m.Consumes)
		for _, s := range m.Scopes {
			out.Scopes = append(out.Scopes, projectScopeDoc{Repository: m.Repository, Name: s.Name, Path: s.Path})
			add(m.Repository+"/"+s.Name, s.Consumes)
		}
	}
	return out
}

// briefs describes nodes of several members, one store read per member.
type briefs struct {
	ctx   context.Context
	s     store.Store
	refs  map[string]map[string]bool
	nodes map[store.ProjectNodeRef]contract.Node
}

func newBriefs(ctx context.Context, s store.Store) *briefs {
	return &briefs{ctx: ctx, s: s, refs: map[string]map[string]bool{}, nodes: map[store.ProjectNodeRef]contract.Node{}}
}

func (b *briefs) want(r store.ProjectNodeRef) {
	if b.refs[r.Repository] == nil {
		b.refs[r.Repository] = map[string]bool{}
	}
	b.refs[r.Repository][r.Identity] = true
}

func (b *briefs) read() error {
	for repo, set := range b.refs {
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		nodes, err := b.s.NodesByIdentity(b.ctx, repo, ids)
		if err != nil {
			return fmt.Errorf("reading nodes of %s: %w", repo, err)
		}
		for _, n := range nodes {
			b.nodes[store.ProjectNodeRef{Repository: repo, Identity: n.Identity}] = n
		}
	}
	return nil
}

func (b *briefs) of(r store.ProjectNodeRef) projectNodeBrief {
	out := projectNodeBrief{Repository: r.Repository, Identity: r.Identity}
	if n, ok := b.nodes[r]; ok {
		out.Kind, out.Name, out.File = n.Kind, n.Name, n.File
		if n.Span != nil {
			out.Line = n.Span.StartLine
		}
	}
	return out
}

// ambiguityOf groups the view's ambiguous outcomes by row and candidate
// providers, largest group first.
func ambiguityOf(view projectlink.View, b *briefs) projectAmbiguousDoc {
	out := projectAmbiguousDoc{Groups: []projectAmbiguityGroup{}}
	index := map[string]int{}
	for _, o := range view.Set.Outcomes {
		if o.Status != projectlink.StatusAmbiguous {
			continue
		}
		out.Total++
		var providers []string
		for _, c := range o.Candidates {
			if !contains(providers, c.Repository) {
				providers = append(providers, c.Repository)
			}
		}
		sort.Strings(providers)
		if providers == nil {
			providers = []string{}
		}
		key := o.Row + "\x00" + strings.Join(providers, "\x00")
		i, ok := index[key]
		if !ok {
			i = len(out.Groups)
			index[key] = i
			out.Groups = append(out.Groups, projectAmbiguityGroup{Row: o.Row, Providers: providers, Sources: []projectAmbiguousSource{}})
		}
		g := &out.Groups[i]
		g.Count++
		if len(g.Sources) >= projectSourcesCap {
			continue
		}
		count := o.CandidateCount
		if count < len(o.Candidates) {
			count = len(o.Candidates)
		}
		b.want(o.Source)
		src := projectAmbiguousSource{Source: projectNodeBrief{Repository: o.Source.Repository, Identity: o.Source.Identity}, Template: o.Template, CandidateCount: count, Candidates: []projectNodeBrief{}}
		for _, c := range o.Candidates {
			b.want(c)
			src.Candidates = append(src.Candidates, projectNodeBrief{Repository: c.Repository, Identity: c.Identity})
		}
		g.Sources = append(g.Sources, src)
	}
	sort.SliceStable(out.Groups, func(i, j int) bool { return out.Groups[i].Count > out.Groups[j].Count })
	return out
}

// conflictsOf computes the declaration conflicts against the current
// manifest and indexes. It reads the members only when some member
// declares link scopes, the only case a conflict can have.
func (ps *projectState) conflictsOf(ctx context.Context) ([]projectlink.Conflict, error) {
	scoped := false
	for _, m := range ps.project.Members {
		scoped = scoped || len(m.Scopes) > 0
	}
	if !scoped {
		return nil, nil
	}
	in, err := projectlink.Read(ctx, ps.opt.Store, ps.project, projectlink.ManifestHash(ps.project))
	if err != nil {
		return nil, err
	}
	return projectlink.DeclarationConflicts(in, func(repository string, calls []string) (map[string][]string, error) {
		edges, err := ps.opt.Store.Edges(ctx, repository, calls, []string{"calls_route"}, store.Outgoing)
		if err != nil {
			return nil, fmt.Errorf("reading calls_route edges of %s: %w", repository, err)
		}
		targets := map[string][]string{}
		for _, e := range edges {
			targets[e.Source] = append(targets[e.Source], e.Target)
		}
		return targets, nil
	})
}

// projectExplained adds the declarations, the ambiguity groups and the
// declaration conflicts to a /api/v0/project body, with one node read per
// member for all of them.
func (ps *projectState) projectExplained(ctx context.Context, view projectlink.View, out *projectResponse) error {
	out.Declarations = declarationsOf(ps.project)
	b := newBriefs(ctx, ps.opt.Store)
	out.Ambiguous = ambiguityOf(view, b)
	conflicts, err := ps.conflictsOf(ctx)
	if err != nil {
		return err
	}
	for _, c := range conflicts {
		b.want(c.Call)
		b.want(c.Target)
	}
	if err := b.read(); err != nil {
		return err
	}
	out.Ambiguous.describe(b)
	out.Conflicts = []projectConflictDoc{}
	for _, c := range conflicts {
		out.Conflicts = append(out.Conflicts, projectConflictDoc{Call: b.of(c.Call), Consumer: c.Consumer, Declared: c.Declared, Target: b.of(c.Target), TargetScope: c.TargetScope})
	}
	return nil
}

// describe fills the briefs of the ambiguity groups after one read.
func (d *projectAmbiguousDoc) describe(b *briefs) {
	for gi := range d.Groups {
		for si := range d.Groups[gi].Sources {
			s := &d.Groups[gi].Sources[si]
			s.Source = b.of(store.ProjectNodeRef{Repository: s.Source.Repository, Identity: s.Source.Identity})
			for ci := range s.Candidates {
				c := &s.Candidates[ci]
				*c = b.of(store.ProjectNodeRef{Repository: c.Repository, Identity: c.Identity})
			}
		}
	}
}
