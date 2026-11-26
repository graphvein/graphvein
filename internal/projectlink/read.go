package projectlink

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// linkKinds are the node kinds the pass reads from each member.
var linkKinds = []string{"route", "client_call", "api_operation"}

// Read loads the pass input for project from st: each member's latest run
// and its route, client_call and api_operation nodes. A member without a
// stored index gets an empty run and no nodes. Reads are not one
// transaction across members; ReplaceProjectLinks refuses the result with
// store.ErrProjectStale when a member was published in between.
func Read(ctx context.Context, st store.Store, project config.Project, manifestHash string) (Input, error) {
	in := Input{Project: project.Name, ManifestHash: manifestHash}
	repos, err := st.Repositories(ctx)
	if err != nil {
		return in, fmt.Errorf("reading the repositories of project %s: %w", project.Name, err)
	}
	runs := map[string]string{}
	for _, r := range repos {
		if r.Latest != nil {
			runs[r.ID] = r.Latest.ID
		}
	}
	for _, m := range project.Members {
		member := Member{Repository: m.Repository, Run: runs[m.Repository], Consumes: m.Consumes, Scopes: m.Scopes, Nodes: []contract.Node{}}
		if member.Run != "" {
			for _, kind := range linkKinds {
				nodes, err := st.Nodes(ctx, m.Repository, store.Query{Kind: kind})
				if err != nil {
					return in, fmt.Errorf("reading %s nodes of %s: %w", kind, m.Repository, err)
				}
				member.Nodes = append(member.Nodes, nodes...)
			}
		}
		in.Members = append(in.Members, member)
	}
	return in, nil
}

// Run reads the project's members from st, links them and stores the link
// set with ReplaceProjectLinks. The caller holds the project lock (ADR 0075
// "Locks"). A member indexed while the pass ran makes it fail with an
// error wrapping store.ErrProjectStale; nothing is written then.
func Run(ctx context.Context, st store.Store, project config.Project, manifestHash string) (Result, store.ProjectLinkRun, error) {
	return RunWithIndexErrors(ctx, st, project, manifestHash, nil)
}

// RunWithIndexErrors is Run for graphvein index --project: indexErrors maps
// a member repository to why indexing its checkout failed just before the
// pass. Such a member is linked from its previous stored index, and the
// error is stored with the member (store.ProjectMember.IndexError) so views
// show its links as stale.
func RunWithIndexErrors(ctx context.Context, st store.Store, project config.Project, manifestHash string, indexErrors map[string]string) (Result, store.ProjectLinkRun, error) {
	in, err := Read(ctx, st, project, manifestHash)
	if err != nil {
		return Result{}, store.ProjectLinkRun{}, err
	}
	result := Link(in)
	for i := range result.Set.Members {
		result.Set.Members[i].IndexError = indexErrors[result.Set.Members[i].Repository]
	}
	run, err := st.ReplaceProjectLinks(ctx, result.Set)
	if err != nil {
		return result, run, fmt.Errorf("linking project %s: %w", project.Name, err)
	}
	return result, run, nil
}

// ManifestHash identifies what the link pass reads from a manifest: the
// project name, the members in order and their consumes and scopes
// declarations. Member paths and comments are left out: a path names
// where a checkout is on this machine, and what was indexed from it is
// tracked by the member's run, so moving a checkout or editing a comment
// does not mark links stale. Any declaration change does.
func ManifestHash(project config.Project) string {
	type selector struct {
		Bases        []string `json:"bases"`
		Origins      []string `json:"origins"`
		PathPrefixes []string `json:"path_prefixes"`
	}
	type consume struct {
		Repository string `json:"repository"`
		Scope      string `json:"scope"`
		// DefaultScope is left out when false, so manifests without it
		// keep the hash they had before the field existed.
		DefaultScope bool     `json:"default_scope,omitempty"`
		Select       selector `json:"select"`
	}
	type scope struct {
		Name     string    `json:"name"`
		Path     string    `json:"path"`
		Consumes []consume `json:"consumes"`
	}
	type member struct {
		Repository string    `json:"repository"`
		Consumes   []consume `json:"consumes"`
		Scopes     []scope   `json:"scopes"`
	}
	consumes := func(in []config.Consume) []consume {
		out := []consume{}
		for _, c := range in {
			out = append(out, consume{Repository: c.Repository, Scope: c.Scope, DefaultScope: c.DefaultScope, Select: selector{Bases: c.Select.Bases, Origins: c.Select.Origins, PathPrefixes: c.Select.PathPrefixes}})
		}
		return out
	}
	canonical := struct {
		Name    string   `json:"name"`
		Members []member `json:"members"`
	}{Name: project.Name, Members: []member{}}
	for _, m := range project.Members {
		out := member{Repository: m.Repository, Consumes: consumes(m.Consumes), Scopes: []scope{}}
		for _, s := range m.Scopes {
			out.Scopes = append(out.Scopes, scope{Name: s.Name, Path: s.Path, Consumes: consumes(s.Consumes)})
		}
		canonical.Members = append(canonical.Members, out)
	}
	// Marshalling plain strings, slices and structs cannot fail.
	data, _ := json.Marshal(canonical)
	return contract.ContentHash(data)
}
