package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// projectRecord is the payload of a projects row.
type projectRecord struct {
	Project      string `json:"project"`
	ManifestHash string `json:"manifest_hash"`
}

// Repositories lists the stored repositories, ref namespaces excluded, with
// their latest runs, read in one read transaction.
func (s *DB) Repositories(ctx context.Context) ([]store.Repository, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT repository FROM repositories ORDER BY repository")
	if err != nil {
		return nil, fmt.Errorf("reading repositories: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reading repositories: %w", err)
		}
		if !strings.Contains(id, store.RefMarker) {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("reading repositories: %w", err)
	}
	sort.Strings(ids)
	out := []store.Repository{}
	for _, id := range ids {
		latest, err := latestRun(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, store.Repository{ID: id, Latest: latest})
	}
	return out, nil
}

// latestRun reads the repository's runs in Runs order and picks the latest.
func latestRun(ctx context.Context, db queryer, repo string) (*store.Run, error) {
	runs, err := read[store.Run](ctx, db, "SELECT payload FROM index_runs WHERE repository=$1 ORDER BY id", repo)
	if err != nil {
		return nil, fmt.Errorf("reading runs of %s: %w", repo, err)
	}
	return store.LatestRun(runs), nil
}

// projectNameProblem returns why a project or member repository ID cannot
// be stored, or "" when it can.
func projectNameProblem(id string) string {
	switch {
	case id == "":
		return "is empty"
	case strings.ContainsRune(id, 0):
		return "contains NUL"
	case strings.Contains(id, store.RefMarker):
		return "contains " + store.RefMarker
	}
	return ""
}

// validateProjectLinks checks a link set before anything is read or
// written, and normalizes nil evidence to an empty list so a stored link
// reads back equal to what was written.
func validateProjectLinks(set *store.ProjectLinkSet) error {
	if p := projectNameProblem(set.Project); p != "" {
		return invalid(fmt.Errorf("project name %s", p))
	}
	if set.ManifestHash == "" {
		return invalid(fmt.Errorf("project %s: manifest hash is empty", set.Project))
	}
	if len(set.Members) == 0 {
		return invalid(fmt.Errorf("project %s has no members", set.Project))
	}
	runs := map[string]string{}
	for _, m := range set.Members {
		if p := projectNameProblem(m.Repository); p != "" {
			return invalid(fmt.Errorf("project %s: member repository %q %s", set.Project, m.Repository, p))
		}
		if _, ok := runs[m.Repository]; ok {
			return invalid(fmt.Errorf("project %s: member %s is listed twice", set.Project, m.Repository))
		}
		runs[m.Repository] = m.Run
	}
	seen := map[store.ProjectLinkKey]bool{}
	for i := range set.Links {
		l := &set.Links[i]
		key := l.Key()
		name := fmt.Sprintf("link %s %s:%s -> %s:%s", l.Kind, l.Source.Repository, l.Source.Identity, l.Target.Repository, l.Target.Identity)
		if l.Kind == "" || strings.ContainsRune(l.Kind, 0) {
			return invalid(fmt.Errorf("%s: kind must be nonempty without NUL", name))
		}
		for _, end := range []store.ProjectEndpoint{l.Source, l.Target} {
			run, member := runs[end.Repository]
			switch {
			case !member:
				return invalid(fmt.Errorf("%s: %s is not a project member", name, end.Repository))
			case end.Identity == "" || strings.ContainsRune(end.Identity, 0):
				return invalid(fmt.Errorf("%s: identity must be nonempty without NUL", name))
			case end.Run == "":
				return invalid(fmt.Errorf("%s: endpoint in %s records no run", name, end.Repository))
			case end.Run != run:
				return invalid(fmt.Errorf("%s: endpoint run %s is not the run the pass read for %s (%q)", name, end.Run, end.Repository, run))
			case end.ContentHash == "":
				return invalid(fmt.Errorf("%s: endpoint in %s records no content hash", name, end.Repository))
			}
		}
		if l.Provenance == "" {
			return invalid(fmt.Errorf("%s: provenance is empty", name))
		}
		if math.IsNaN(l.Confidence) || l.Confidence <= 0 || l.Confidence > 1 {
			return invalid(fmt.Errorf("%s: confidence %v is outside (0, 1]", name, l.Confidence))
		}
		if strings.TrimSpace(l.Assumption) == "" {
			return invalid(fmt.Errorf("%s: assumption is empty", name))
		}
		if seen[key] {
			return invalid(fmt.Errorf("%s is listed twice", name))
		}
		seen[key] = true
		if l.Evidence == nil {
			l.Evidence = []contract.Evidence{}
		}
	}
	for i := range set.Outcomes {
		o := &set.Outcomes[i]
		name := fmt.Sprintf("outcome %d (%s %s:%s)", i, o.Row, o.Source.Repository, o.Source.Identity)
		if _, member := runs[o.Source.Repository]; !member {
			return invalid(fmt.Errorf("%s: %s is not a project member", name, o.Source.Repository))
		}
		if o.Source.Identity == "" || strings.ContainsRune(o.Source.Identity, 0) {
			return invalid(fmt.Errorf("%s: identity must be nonempty without NUL", name))
		}
		if o.Kind == "" || o.Row == "" || o.Status == "" {
			return invalid(fmt.Errorf("%s: kind, row and status must be nonempty", name))
		}
		if o.Candidates == nil {
			o.Candidates = []store.ProjectNodeRef{}
		}
	}
	return nil
}

// comparablePayload returns l's stored payload with both endpoint runs
// blanked. A member indexed again with the same content gets a new run,
// and a link whose payload differs only there is the same link
// (T022 B4): it is rewritten with the new runs but counted unchanged, and
// its journal hash does not change.
func comparablePayload(l store.ProjectLink) (string, error) {
	l.Source.Run, l.Target.Run = "", ""
	return payload(l)
}

// ReplaceProjectLinks stores one link pass: members, the full link set and
// a journal entry, in one transaction, after proving the members and link
// endpoints are still stored as the pass read them.
func (s *DB) ReplaceProjectLinks(ctx context.Context, set store.ProjectLinkSet) (store.ProjectLinkRun, error) {
	var run store.ProjectLinkRun
	set.Links = append([]store.ProjectLink(nil), set.Links...)
	set.Outcomes = append([]store.ProjectOutcome(nil), set.Outcomes...)
	if err := validateProjectLinks(&set); err != nil {
		return run, fmt.Errorf("storing project links: %w", err)
	}
	sort.Slice(set.Links, func(i, j int) bool { return set.Links[i].Key().Less(set.Links[j].Key()) })
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return run, fmt.Errorf("storing links of project %s: %w", set.Project, err)
	}
	defer tx.Rollback()
	fail := func(err error) (store.ProjectLinkRun, error) {
		return store.ProjectLinkRun{}, fmt.Errorf("storing links of project %s: %w", set.Project, err)
	}

	// The pass must have read every member's latest run: a member indexed
	// since then could add, remove or move the nodes the links name.
	for _, m := range set.Members {
		latest, err := latestRun(ctx, tx, m.Repository)
		if err != nil {
			return fail(err)
		}
		current := ""
		if latest != nil {
			current = latest.ID
		}
		if current != m.Run {
			return fail(fmt.Errorf("%w: member %s was read at run %q, its latest run is %q", store.ErrProjectStale, m.Repository, m.Run, current))
		}
	}
	for _, l := range set.Links {
		for _, end := range []store.ProjectEndpoint{l.Source, l.Target} {
			var hash string
			err := tx.QueryRowContext(ctx, "SELECT content_hash FROM nodes WHERE repository=$1 AND identity=$2", end.Repository, end.Identity).Scan(&hash)
			if errors.Is(err, sql.ErrNoRows) {
				return fail(fmt.Errorf("%w: %s %s is not stored", store.ErrProjectStale, end.Repository, end.Identity))
			}
			if err != nil {
				return fail(fmt.Errorf("reading %s %s: %w", end.Repository, end.Identity, err))
			}
			if hash != end.ContentHash {
				return fail(fmt.Errorf("%w: %s %s is stored with content hash %s, the link records %s", store.ErrProjectStale, end.Repository, end.Identity, hash, end.ContentHash))
			}
		}
	}

	old, err := storedProjectLinks(ctx, tx, set.Project)
	if err != nil {
		return fail(err)
	}
	record, err := payload(projectRecord{Project: set.Project, ManifestHash: set.ManifestHash})
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO projects(project,payload) VALUES($1,$2) ON CONFLICT(project) DO UPDATE SET payload=excluded.payload", set.Project, record); err != nil {
		return fail(fmt.Errorf("writing project: %w", err))
	}
	members := map[string]bool{}
	for i, m := range set.Members {
		members[m.Repository] = true
		data, err := payload(m)
		if err != nil {
			return fail(err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO project_members(project,repository,ordinal,payload) VALUES($1,$2,$3,$4) ON CONFLICT(project,repository) DO UPDATE SET ordinal=excluded.ordinal,payload=excluded.payload", set.Project, m.Repository, i, data); err != nil {
			return fail(fmt.Errorf("writing member %s: %w", m.Repository, err))
		}
	}

	run = store.ProjectLinkRun{ID: rand.Text(), ManifestHash: set.ManifestHash, Members: set.Members, Links: []store.ProjectLinkEntry{}}
	kept := map[store.ProjectLinkKey]bool{}
	for _, l := range set.Links {
		key := l.Key()
		kept[key] = true
		data, err := payload(l)
		if err != nil {
			return fail(err)
		}
		comparable, err := comparablePayload(l)
		if err != nil {
			return fail(err)
		}
		run.Links = append(run.Links, store.ProjectLinkEntry{ProjectLinkKey: key, PayloadHash: contract.ContentHash([]byte(comparable))})
		before, existed := old[key]
		switch {
		case !existed:
			run.Added++
			_, err = tx.ExecContext(ctx, "INSERT INTO project_links(project,kind,source_repository,source_identity,target_repository,target_identity,payload) VALUES($1,$2,$3,$4,$5,$6,$7)", set.Project, key.Kind, key.SourceRepository, key.SourceIdentity, key.TargetRepository, key.TargetIdentity, data)
		case before.payload != data:
			if before.comparable != comparable {
				run.Changed++
			}
			_, err = tx.ExecContext(ctx, "UPDATE project_links SET payload=$7 WHERE project=$1 AND kind=$2 AND source_repository=$3 AND source_identity=$4 AND target_repository=$5 AND target_identity=$6", set.Project, key.Kind, key.SourceRepository, key.SourceIdentity, key.TargetRepository, key.TargetIdentity, data)
		}
		if err != nil {
			return fail(fmt.Errorf("writing link: %w", err))
		}
	}
	for key := range old {
		if kept[key] {
			continue
		}
		run.Removed++
		if _, err := tx.ExecContext(ctx, "DELETE FROM project_links WHERE project=$1 AND kind=$2 AND source_repository=$3 AND source_identity=$4 AND target_repository=$5 AND target_identity=$6", set.Project, key.Kind, key.SourceRepository, key.SourceIdentity, key.TargetRepository, key.TargetIdentity); err != nil {
			return fail(fmt.Errorf("deleting link: %w", err))
		}
	}
	// Outcomes are recomputed in full by every pass, never patched.
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_outcomes WHERE project=$1", set.Project); err != nil {
		return fail(fmt.Errorf("deleting outcomes: %w", err))
	}
	for i, o := range set.Outcomes {
		data, err := payload(o)
		if err != nil {
			return fail(err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO project_outcomes(project,ordinal,source_repository,source_identity,status,payload) VALUES($1,$2,$3,$4,$5,$6)", set.Project, i, o.Source.Repository, o.Source.Identity, o.Status, data); err != nil {
			return fail(fmt.Errorf("writing outcome: %w", err))
		}
	}
	// Former members go last: every link and outcome naming them is
	// already deleted.
	stored, err := storedMembers(ctx, tx, set.Project)
	if err != nil {
		return fail(err)
	}
	for _, m := range stored {
		if members[m.Repository] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM project_members WHERE project=$1 AND repository=$2", set.Project, m.Repository); err != nil {
			return fail(fmt.Errorf("deleting member %s: %w", m.Repository, err))
		}
	}

	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(seq) FROM project_link_runs WHERE project=$1", set.Project).Scan(&last); err != nil {
		return fail(fmt.Errorf("reading the link journal: %w", err))
	}
	run.Seq = last.Int64 + 1
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := payload(run)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO project_link_runs(project,seq,payload) VALUES($1,$2,$3)", set.Project, run.Seq, data); err != nil {
		return fail(fmt.Errorf("writing the link journal: %w", err))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_link_runs WHERE project=$1 AND seq<=$2", set.Project, run.Seq-store.DefaultProjectLinkRuns); err != nil {
		return fail(fmt.Errorf("trimming the link journal: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return run, nil
}

// storedLink is a stored link payload and its comparablePayload.
type storedLink struct {
	payload, comparable string
}

// storedProjectLinks maps each stored link key of project to its payload.
func storedProjectLinks(ctx context.Context, db queryer, project string) (map[store.ProjectLinkKey]storedLink, error) {
	rows, err := db.QueryContext(ctx, "SELECT kind,source_repository,source_identity,target_repository,target_identity,payload FROM project_links WHERE project=$1", project)
	if err != nil {
		return nil, fmt.Errorf("reading links of project %s: %w", project, err)
	}
	defer rows.Close()
	out := map[store.ProjectLinkKey]storedLink{}
	for rows.Next() {
		var k store.ProjectLinkKey
		var data string
		if err := rows.Scan(&k.Kind, &k.SourceRepository, &k.SourceIdentity, &k.TargetRepository, &k.TargetIdentity, &data); err != nil {
			return nil, fmt.Errorf("reading links of project %s: %w", project, err)
		}
		var l store.ProjectLink
		if err := decodeStored(data, &l); err != nil {
			return nil, fmt.Errorf("reading link %s %s:%s of project %s: %w", k.Kind, k.SourceRepository, k.SourceIdentity, project, err)
		}
		comparable, err := comparablePayload(l)
		if err != nil {
			return nil, err
		}
		out[k] = storedLink{payload: data, comparable: comparable}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading links of project %s: %w", project, err)
	}
	return out, nil
}

func storedMembers(ctx context.Context, db queryer, project string) ([]store.ProjectMember, error) {
	out, err := read[store.ProjectMember](ctx, db, "SELECT payload FROM project_members WHERE project=$1 ORDER BY ordinal", project)
	if err != nil {
		return nil, fmt.Errorf("reading members of project %s: %w", project, err)
	}
	return out, nil
}

// ProjectLinks reads the stored project, its members and links in one read
// transaction.
func (s *DB) ProjectLinks(ctx context.Context, project string) (store.ProjectLinkSet, error) {
	var set store.ProjectLinkSet
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return set, err
	}
	defer tx.Rollback()
	records, err := read[projectRecord](ctx, tx, "SELECT payload FROM projects WHERE project=$1", project)
	if err != nil {
		return set, fmt.Errorf("reading project %s: %w", project, err)
	}
	if len(records) == 0 {
		return set, fmt.Errorf("reading project %s: %w", project, store.ErrProjectUnknown)
	}
	set = store.ProjectLinkSet{Project: records[0].Project, ManifestHash: records[0].ManifestHash}
	if set.Members, err = storedMembers(ctx, tx, project); err != nil {
		return store.ProjectLinkSet{}, err
	}
	if set.Links, err = read[store.ProjectLink](ctx, tx, "SELECT payload FROM project_links WHERE project=$1", project); err != nil {
		return store.ProjectLinkSet{}, fmt.Errorf("reading links of project %s: %w", project, err)
	}
	sort.Slice(set.Links, func(i, j int) bool { return set.Links[i].Key().Less(set.Links[j].Key()) })
	if set.Outcomes, err = read[store.ProjectOutcome](ctx, tx, "SELECT payload FROM project_outcomes WHERE project=$1 ORDER BY ordinal", project); err != nil {
		return store.ProjectLinkSet{}, fmt.Errorf("reading outcomes of project %s: %w", project, err)
	}
	if len(set.Outcomes) == 0 {
		set.Outcomes = nil
	}
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(seq) FROM project_link_runs WHERE project=$1", project).Scan(&last); err != nil {
		return store.ProjectLinkSet{}, fmt.Errorf("reading the link journal of project %s: %w", project, err)
	}
	set.Pass = last.Int64
	return set, nil
}

// ProjectLinkRuns lists the kept link passes of project, oldest first.
func (s *DB) ProjectLinkRuns(ctx context.Context, project string) ([]store.ProjectLinkRun, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out, err := read[store.ProjectLinkRun](ctx, tx, "SELECT payload FROM project_link_runs WHERE project=$1 ORDER BY seq", project)
	if err != nil {
		return nil, fmt.Errorf("reading the link journal of project %s: %w", project, err)
	}
	return out, nil
}

// RetireProjectMember removes a repository renamed away from project and
// its stored index, in one transaction (store.ProjectStore).
func (s *DB) RetireProjectMember(ctx context.Context, project, repository string) error {
	fail := func(err error) error {
		return fmt.Errorf("retiring %s from project %s: %w", repository, project, err)
	}
	if repository == "" {
		return fail(fmt.Errorf("%w: empty repository", store.ErrInvalid))
	}
	if strings.Contains(repository, store.RefMarker) {
		return fail(store.ErrReservedRepository)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var others int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM project_members WHERE repository=$1 AND project<>$2", repository, project).Scan(&others); err != nil {
		return fail(fmt.Errorf("reading project members: %w", err))
	}
	if others > 0 {
		return fail(store.ErrRepositoryShared)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_links WHERE project=$1 AND (source_repository=$2 OR target_repository=$2)", project, repository); err != nil {
		return fail(fmt.Errorf("deleting links: %w", err))
	}
	// An outcome names a member as its source, its target or a candidate;
	// only the source has a column, so the payloads are read.
	ordinals, err := outcomesNaming(ctx, tx, project, repository)
	if err != nil {
		return fail(err)
	}
	for _, o := range ordinals {
		if _, err := tx.ExecContext(ctx, "DELETE FROM project_outcomes WHERE project=$1 AND ordinal=$2", project, o); err != nil {
			return fail(fmt.Errorf("deleting outcome: %w", err))
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_members WHERE project=$1 AND repository=$2", project, repository); err != nil {
		return fail(fmt.Errorf("deleting member: %w", err))
	}
	namespaces, err := tx.QueryContext(ctx, "SELECT namespace FROM ref_snapshots WHERE repository=$1", repository)
	if err != nil {
		return fail(fmt.Errorf("reading ref snapshots: %w", err))
	}
	var refs []string
	for namespaces.Next() {
		var ns string
		if err := namespaces.Scan(&ns); err != nil {
			namespaces.Close()
			return fail(fmt.Errorf("reading ref snapshots: %w", err))
		}
		refs = append(refs, ns)
	}
	err = namespaces.Err()
	namespaces.Close()
	if err != nil {
		return fail(fmt.Errorf("reading ref snapshots: %w", err))
	}
	for _, ns := range refs {
		if err := dropNamespace(ctx, tx, repository, ns); err != nil {
			return fail(err)
		}
	}
	// Children before parents, so both backends' foreign keys hold at
	// every statement.
	for _, q := range []string{
		"DELETE FROM change_pin_entries WHERE repository=$1",
		"DELETE FROM change_pins WHERE repository=$1",
		"DELETE FROM change_entries WHERE repository=$1",
		"DELETE FROM change_runs WHERE repository=$1",
		"DELETE FROM edges WHERE repository=$1",
		"DELETE FROM nodes WHERE repository=$1",
		"DELETE FROM diagnostics WHERE repository=$1",
		"DELETE FROM files WHERE repository=$1",
		"DELETE FROM fact_cache WHERE repository=$1",
		"DELETE FROM index_runs WHERE repository=$1",
		"DELETE FROM repositories WHERE repository=$1",
	} {
		if _, err := tx.ExecContext(ctx, q, repository); err != nil {
			return fail(fmt.Errorf("deleting the stored index: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return nil
}

// outcomesNaming returns the ordinals of project's outcomes whose source,
// target or candidates name repository.
func outcomesNaming(ctx context.Context, tx *sql.Tx, project, repository string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT ordinal,payload FROM project_outcomes WHERE project=$1", project)
	if err != nil {
		return nil, fmt.Errorf("reading outcomes: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var ordinal int64
		var data string
		if err := rows.Scan(&ordinal, &data); err != nil {
			return nil, fmt.Errorf("reading outcome: %w", err)
		}
		var o store.ProjectOutcome
		if err := decodeStored(data, &o); err != nil {
			return nil, err
		}
		names := o.Source.Repository == repository || (o.Target != nil && o.Target.Repository == repository)
		for _, c := range o.Candidates {
			names = names || c.Repository == repository
		}
		if names {
			out = append(out, ordinal)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading outcomes: %w", err)
	}
	return out, nil
}

// RepositoryFootprint reads what retiring repository would delete
// (store.ProjectStore), in one read transaction.
func (s *DB) RepositoryFootprint(ctx context.Context, repository string) (store.RepositoryFootprint, error) {
	out := store.RepositoryFootprint{Repository: repository, Projects: []string{}}
	fail := func(err error) (store.RepositoryFootprint, error) {
		return out, fmt.Errorf("reading the stored index of %s: %w", repository, err)
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var runs int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM index_runs WHERE repository=$1", repository).Scan(&runs); err != nil {
		return fail(err)
	}
	out.Stored = runs > 0
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes WHERE repository=$1", repository).Scan(&out.Nodes); err != nil {
		return fail(err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT project FROM project_members WHERE repository=$1 ORDER BY project", repository)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return fail(err)
		}
		out.Projects = append(out.Projects, p)
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	// Go byte order on both backends (a Postgres collation may differ).
	sort.Strings(out.Projects)
	return out, nil
}
