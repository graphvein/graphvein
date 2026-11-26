package projectlink

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
)

// Reasons a member's links are stale (ADR 0075 "Cross-repository links":
// a link is fresh only while both members' latest runs equal the runs it
// records and the manifest's declarations are those the pass read).
const (
	ReasonManifestChanged = "manifest changed"
	ReasonIndexedAgain    = "indexed again since the pass"
	ReasonIndexFailed     = "failed to index before the pass"
)

// View is a project's last stored link pass as readers show it: the links
// with their freshness, computed from stored runs and the current
// manifest, never written. graphvein status --project, the project-scoped
// callers and impact (T022 B7) and the server read links through it, so
// "stale" means the same everywhere.
type View struct {
	// Stored reports whether a link pass of the project is stored; when it
	// is false Set is empty.
	Stored bool
	Set    store.ProjectLinkSet
	// ManifestChanged: the manifest's declarations differ from those the
	// pass read, so every link is stale.
	ManifestChanged bool
	// Latest maps each stored repository to its latest run.
	Latest map[string]string
	// memberReasons says why a member's links are stale, from the pass's
	// member records (an index failure, a later run).
	memberReasons map[string]string
	project       config.Project
	byTarget      map[store.ProjectNodeRef][]int
	bySource      map[store.ProjectNodeRef][]int
}

// ReadView reads project's last link pass and the repositories' latest
// runs from st. A project without a stored pass gives a View with Stored
// false and no error.
func ReadView(ctx context.Context, st store.Store, project config.Project) (View, error) {
	v := View{Latest: map[string]string{}, memberReasons: map[string]string{}, project: project}
	repos, err := st.Repositories(ctx)
	if err != nil {
		return v, fmt.Errorf("reading the repositories of project %s: %w", project.Name, err)
	}
	for _, r := range repos {
		if r.Latest != nil {
			v.Latest[r.ID] = r.Latest.ID
		}
	}
	set, err := st.ProjectLinks(ctx, project.Name)
	if errors.Is(err, store.ErrProjectUnknown) {
		return v, nil
	}
	if err != nil {
		return v, fmt.Errorf("reading the links of project %s: %w", project.Name, err)
	}
	v.Stored, v.Set = true, set
	v.ManifestChanged = set.ManifestHash != ManifestHash(project)
	for _, m := range set.Members {
		switch {
		case m.IndexError != "":
			v.memberReasons[m.Repository] = ReasonIndexFailed
		case m.Run != "" && m.Run != v.Latest[m.Repository]:
			v.memberReasons[m.Repository] = ReasonIndexedAgain
		}
	}
	v.byTarget = map[store.ProjectNodeRef][]int{}
	v.bySource = map[store.ProjectNodeRef][]int{}
	for i, l := range set.Links {
		t := store.ProjectNodeRef{Repository: l.Target.Repository, Identity: l.Target.Identity}
		s := store.ProjectNodeRef{Repository: l.Source.Repository, Identity: l.Source.Identity}
		v.byTarget[t] = append(v.byTarget[t], i)
		v.bySource[s] = append(v.bySource[s], i)
	}
	return v, nil
}

// StaleReasons says why l is stale, nil when it is fresh: the manifest
// changed, or an endpoint's member failed to index before the pass or was
// indexed again since (its latest run is not the run the link records).
// Each reason names the member it is about.
func (v View) StaleReasons(l store.ProjectLink) []string {
	var why []string
	if v.ManifestChanged {
		why = append(why, ReasonManifestChanged)
	}
	seen := map[string]bool{}
	for _, end := range []store.ProjectEndpoint{l.Source, l.Target} {
		reason := v.memberReasons[end.Repository]
		if reason == "" && end.Run != v.Latest[end.Repository] {
			reason = ReasonIndexedAgain
		}
		if reason == "" {
			continue
		}
		text := end.Repository + " " + reason
		if !seen[text] {
			seen[text] = true
			why = append(why, text)
		}
	}
	return why
}

// PassReasons says why the pass as a whole is stale (it would link
// differently if run now), empty when it is fresh: declarations changed,
// and per member of the current manifest, in order, whether it joined
// the project, failed to index, was indexed for the first time or again
// since the pass; then members that left the project. A member indexed
// for the first time makes the pass stale but none of its links (it had
// none).
func (v View) PassReasons() []string {
	if !v.Stored {
		return nil
	}
	var out []string
	if v.ManifestChanged {
		out = append(out, "the manifest's declarations changed since the pass")
	}
	inPass := map[string]store.ProjectMember{}
	for _, m := range v.Set.Members {
		inPass[m.Repository] = m
	}
	for _, m := range v.project.Members {
		pm, ok := inPass[m.Repository]
		current := v.Latest[m.Repository]
		switch {
		case !ok:
			out = append(out, m.Repository+" joined the project")
		case pm.IndexError != "":
			out = append(out, m.Repository+" failed to index")
		case pm.Run != current && pm.Run == "":
			out = append(out, m.Repository+" indexed since the pass")
		case pm.Run != current:
			out = append(out, m.Repository+" indexed again")
		}
	}
	for _, m := range v.Set.Members {
		found := false
		for _, pm := range v.project.Members {
			found = found || pm.Repository == m.Repository
		}
		if !found {
			out = append(out, m.Repository+" left the project")
		}
	}
	return out
}

// LinksTo returns the stored links whose target is the node (repository,
// identity), in key order.
func (v View) LinksTo(repository, identity string) []store.ProjectLink {
	return v.pick(v.byTarget[store.ProjectNodeRef{Repository: repository, Identity: identity}])
}

// LinksFrom returns the stored links whose source is the node
// (repository, identity), in key order.
func (v View) LinksFrom(repository, identity string) []store.ProjectLink {
	return v.pick(v.bySource[store.ProjectNodeRef{Repository: repository, Identity: identity}])
}

func (v View) pick(indexes []int) []store.ProjectLink {
	out := make([]store.ProjectLink, 0, len(indexes))
	for _, i := range indexes {
		out = append(out, v.Set.Links[i])
	}
	return out
}

// Possible-caller reasons (T022 review). A possible caller is never a
// link: views list it apart, as an unknown ("may be called by"), and
// count it in their blind-spot line.
const (
	// PossibleAmbiguous: the pass left the source ambiguous between several
	// routes of other members, this route among them (PL-R10, PL-R11,
	// PL-S3).
	PossibleAmbiguous = "ambiguous across repositories"
	// PossibleIncomplete: the pass could not decide the source because a
	// candidate provider had no stored index (PL-R7); the route's member
	// was one of its candidate providers.
	PossibleIncomplete = "not linked: a candidate provider had no stored index"
)

// PossibleCaller is a source of another member that the last stored pass
// did not link but that may reach a route: an ambiguous outcome whose
// candidates hold the route, or an incomplete outcome whose candidate
// providers include the route's member.
type PossibleCaller struct {
	Kind     string               `json:"kind"`
	Source   store.ProjectNodeRef `json:"source"`
	Template string               `json:"template"`
	Row      string               `json:"row"`
	Status   string               `json:"status"`
	// Wording is PossibleAmbiguous or PossibleIncomplete; Reason is the
	// outcome's own reason (the providers without a stored index), and
	// CandidateRepositories the members its listed candidates are in.
	Wording               string   `json:"wording"`
	Reason                string   `json:"reason"`
	CandidateCount        int      `json:"candidate_count"`
	CandidateRepositories []string `json:"candidate_repositories"`
}

// PossibleCallers lists the outcomes of the last stored pass that may
// reach the route (repository, identity) without a link to it, in the
// pass's outcome order, one entry per source (its first such outcome: the
// alternatives of one call count once):
//   - an ambiguous outcome (status ambiguous) of another member whose
//     candidates hold the route. A list cut at ten (CandidateCount greater
//     than the listed candidates) is ordered by repository and identity,
//     so an unlisted candidate sorts after the last listed one: a route
//     sorting after it may be among them and is listed too;
//   - an incomplete outcome (PL-R7) of another member whose candidates
//     (the indexed providers' matching routes) may hold the route, by the
//     same rule. One whose reason says no indexed provider matches is not
//     listed. One stored before PL-R7 listed candidates (neither) may
//     reach any route of an indexed candidate provider, and the current
//     manifest decides those as a superset: every other member when the
//     source's member or one of its scopes has no consumes, or when the
//     manifest changed since the pass; else the repositories its consumes
//     entries (member and scopes) name.
//
// Every other status claims nothing about the route and is not listed.
func (v View) PossibleCallers(repository, identity string) []PossibleCaller {
	out := []PossibleCaller{}
	if !v.Stored {
		return out
	}
	route := store.ProjectNodeRef{Repository: repository, Identity: identity}
	seen := map[store.ProjectNodeRef]bool{}
	for _, o := range v.Set.Outcomes {
		if o.Source.Repository == repository || seen[o.Source] {
			continue
		}
		var wording string
		switch o.Status {
		case StatusAmbiguous:
			if !holds(o, route) {
				continue
			}
			wording = PossibleAmbiguous
		case StatusIncomplete:
			switch {
			case len(o.Candidates) > 0:
				if !holds(o, route) {
					continue
				}
			case strings.Contains(o.Reason, NoIndexedCandidate):
				continue
			case !v.mayProvide(o.Source.Repository, repository):
				// A pass stored before PL-R7 listed candidates.
				continue
			}
			wording = PossibleIncomplete
		default:
			continue
		}
		seen[o.Source] = true
		count := o.CandidateCount
		if count < len(o.Candidates) {
			count = len(o.Candidates)
		}
		repos := []string{}
		for _, c := range o.Candidates {
			if len(repos) == 0 || repos[len(repos)-1] != c.Repository {
				repos = append(repos, c.Repository)
			}
		}
		out = append(out, PossibleCaller{Kind: o.Kind, Source: o.Source, Template: o.Template, Row: o.Row, Status: o.Status,
			Wording: wording, Reason: o.Reason, CandidateCount: count, CandidateRepositories: repos})
	}
	return out
}

// holds reports whether an ambiguous outcome's candidates may hold route:
// listed, or sorting after the last listed candidate of a list cut at ten.
func holds(o store.ProjectOutcome, route store.ProjectNodeRef) bool {
	for _, c := range o.Candidates {
		if c == route {
			return true
		}
	}
	if len(o.Candidates) == 0 || o.CandidateCount <= len(o.Candidates) {
		return false
	}
	last := o.Candidates[len(o.Candidates)-1]
	return route.Repository > last.Repository || route.Repository == last.Repository && route.Identity > last.Identity
}

// mayProvide reports whether provider may have been a candidate provider
// of a source of consumer under the current manifest (a superset, see
// PossibleCallers).
func (v View) mayProvide(consumer, provider string) bool {
	if v.ManifestChanged {
		return true
	}
	for _, m := range v.project.Members {
		if m.Repository != consumer {
			continue
		}
		lists := [][]config.Consume{m.Consumes}
		for _, s := range m.Scopes {
			lists = append(lists, s.Consumes)
		}
		for _, list := range lists {
			if len(list) == 0 {
				return true
			}
			for _, c := range list {
				if c.Repository == provider {
					return true
				}
			}
		}
		return false
	}
	// A consumer the manifest no longer names: its outcome is from an older
	// manifest, so nothing narrows it.
	return true
}
