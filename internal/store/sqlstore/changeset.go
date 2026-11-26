package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/store"
)

// ChangeSet composes the raw difference between a base and the head in one
// read transaction (ADR 0056 "Raw change sets"). Run and pin bases rebuild
// the base state of each changed key from the journal or the pin overlay and
// read the head state from the current graph; ref bases compare two
// repository namespaces directly in SQL.
func (s *DB) ChangeSet(ctx context.Context, repo string, base store.ChangeBase) (store.ChangeSet, error) {
	var out store.ChangeSet
	given := 0
	for _, v := range []string{base.Run, base.Pin, base.Ref} {
		if v != "" {
			given++
		}
	}
	if given != 1 || (base.Head != "" && base.Ref == "") {
		return out, fmt.Errorf("a comparison base names exactly one run, pin or ref")
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if base.Ref != "" {
		return s.refChangeSet(ctx, tx, repo, base)
	}
	runs, err := changeRuns(ctx, tx, repo)
	if err != nil {
		return out, err
	}
	if len(runs) == 0 {
		return out, fmt.Errorf("comparing %s: %w", repo, store.ErrNoJournal)
	}
	out.Head = store.ChangeSide{Kind: store.SideCurrent, Repository: repo, Run: runs[len(runs)-1]}
	var states map[changeKey]*string
	if base.Run != "" {
		var after int64
		out.Base, after, err = runBase(ctx, tx, repo, runs, base.Run)
		if err != nil {
			return out, err
		}
		states, err = journalStates(ctx, tx, repo, after)
	} else {
		var pin store.Pin
		pin, err = findPin(ctx, tx, repo, base.Pin)
		if err != nil {
			return out, err
		}
		out.Base = store.ChangeSide{Kind: store.SidePin, Repository: repo, Pin: pin.Name, Run: pin.Run}
		states, err = pinStates(ctx, tx, repo, pin.ID)
	}
	if err != nil {
		return out, err
	}
	current, err := currentPayloads(ctx, tx, repo, states)
	if err != nil {
		return out, err
	}
	out.Changes = []store.RecordChange{}
	for k, before := range states {
		after, has := current[k]
		if before == nil && !has || before != nil && has && *before == after {
			continue
		}
		c := store.RecordChange{Record: k.record, Key: k.key}
		if before != nil {
			c.Before = json.RawMessage(*before)
		}
		if has {
			c.After = json.RawMessage(after)
		}
		out.Changes = append(out.Changes, c)
	}
	sortChanges(out.Changes)
	return out, nil
}

type changeKey struct{ record, key string }

func sortChanges(c []store.RecordChange) {
	sort.Slice(c, func(i, j int) bool {
		if c[i].Record != c[j].Record {
			return c[i].Record < c[j].Record
		}
		return c[i].Key < c[j].Key
	})
}

// runBase resolves a run base. A kept journal run is covered; so is the run
// the oldest kept run started from, since that run's entries hold its state.
// The returned seq is the one after which entries describe changes since the
// base.
func runBase(ctx context.Context, tx *sql.Tx, repo string, runs []store.ChangeRun, id string) (store.ChangeSide, int64, error) {
	for _, r := range runs {
		if r.RunID == id {
			return store.ChangeSide{Kind: store.SideRun, Repository: repo, Run: r}, r.Seq, nil
		}
	}
	oldest := runs[0]
	if !oldest.Initial && oldest.PreviousRun == id {
		side := store.ChangeSide{Kind: store.SideRun, Repository: repo, Run: store.ChangeRun{RunID: id}}
		if r, err := indexRun(ctx, tx, repo, id); err == nil && r != nil {
			side.Run.Indexer = store.Indexer{Name: r.Header.Indexer.Name, Version: r.Header.Indexer.Version}
			side.Run.Commit = r.Header.Commit
			side.Run.FinishedAt = r.FinishedAt
		}
		// The run is older than the journal, so its gaps were never
		// recorded.
		side.Run.GapsUnknown = true
		side.Run.Gaps = []string{}
		return side, oldest.Seq - 1, nil
	}
	r, err := indexRun(ctx, tx, repo, id)
	if err != nil {
		return store.ChangeSide{}, 0, err
	}
	if r != nil {
		return store.ChangeSide{}, 0, fmt.Errorf("comparing with run %s: %w", id, store.ErrBaseExpired)
	}
	return store.ChangeSide{}, 0, fmt.Errorf("comparing with run %s: %w", id, store.ErrBaseUnknown)
}

func indexRun(ctx context.Context, tx *sql.Tx, repo, id string) (*store.Run, error) {
	runs, err := read[store.Run](ctx, tx, "SELECT payload FROM index_runs WHERE repository=$1 AND id=$2", repo, id)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

// journalStates returns, for every key changed after seq, its state right
// after seq: the before payload of its earliest later entry.
func journalStates(ctx context.Context, tx *sql.Tx, repo string, seq int64) (map[changeKey]*string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT record,key,before FROM change_entries WHERE repository=$1 AND seq>$2 ORDER BY seq", repo, seq)
	if err != nil {
		return nil, fmt.Errorf("reading the change journal of %s: %w", repo, err)
	}
	return scanStates(rows, true)
}

func pinStates(ctx context.Context, tx *sql.Tx, repo, pin string) (map[changeKey]*string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT record,key,before FROM change_pin_entries WHERE repository=$1 AND pin=$2", repo, pin)
	if err != nil {
		return nil, fmt.Errorf("reading the pin overlay of %s: %w", repo, err)
	}
	return scanStates(rows, false)
}

// scanStates reads (record, key, before) rows; with first, a key keeps its
// first row's state.
func scanStates(rows *sql.Rows, first bool) (map[changeKey]*string, error) {
	defer rows.Close()
	out := map[changeKey]*string{}
	for rows.Next() {
		var k changeKey
		var before sql.NullString
		if err := rows.Scan(&k.record, &k.key, &before); err != nil {
			return nil, fmt.Errorf("reading change entry: %w", err)
		}
		if _, done := out[k]; done && first {
			continue
		}
		if before.Valid {
			v := before.String
			out[k] = &v
		} else {
			out[k] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading change entries: %w", err)
	}
	return out, nil
}

func findPin(ctx context.Context, tx *sql.Tx, repo, name string) (store.Pin, error) {
	list, err := pins(ctx, tx, repo)
	if err != nil {
		return store.Pin{}, err
	}
	for _, p := range list {
		if p.Name == name {
			return p, nil
		}
	}
	return store.Pin{}, fmt.Errorf("comparing with pin %s: %w", name, store.ErrBaseUnknown)
}

// currentPayloads reads the stored payload of each key, chunked by primary
// key so parameter counts stay bounded. Absent keys are missing from the map.
func currentPayloads(ctx context.Context, tx *sql.Tx, repo string, keys map[changeKey]*string) (map[changeKey]string, error) {
	byRecord := map[string][]string{}
	for k := range keys {
		byRecord[k.record] = append(byRecord[k.record], k.key)
	}
	out := map[changeKey]string{}
	for record, list := range byRecord {
		sort.Strings(list)
		for start := 0; start < len(list); start += 64 {
			end := min(start+64, len(list))
			args := []any{repo}
			var q string
			switch record {
			case store.RecordFile, store.RecordNode:
				table, column := "files", "path"
				if record == store.RecordNode {
					table, column = "nodes", "identity"
				}
				placeholders := []string{}
				for _, key := range list[start:end] {
					args = append(args, key)
					placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
				}
				q = "SELECT " + column + ",payload FROM " + table + " WHERE repository=$1 AND " + column + " IN (" + strings.Join(placeholders, ",") + ")"
			case store.RecordEdge:
				// A row-value list keeps one primary-key lookup per key. An
				// OR of 64 (kind AND source AND target) terms made SQLite
				// scan every edge of the repository instead (about 0.35 s
				// per chunk on Langflow).
				tuples := []string{}
				for _, key := range list[start:end] {
					kind, source, target, err := store.ParseEdgeKey(key)
					if err != nil {
						return nil, err
					}
					args = append(args, kind, source, target)
					n := len(args)
					tuples = append(tuples, fmt.Sprintf("($%d,$%d,$%d)", n-2, n-1, n))
				}
				q = "SELECT kind,source,target,payload FROM edges WHERE repository=$1 AND (kind,source,target) IN (VALUES " + strings.Join(tuples, ",") + ")"
			default:
				return nil, fmt.Errorf("unknown journal record %q", record)
			}
			if err := scanPayloads(ctx, tx, record, q, args, func(k changeKey, data string) { out[k] = data }); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// scanPayloads runs q, whose rows are the record's key columns then a
// payload, and passes each row on with its journal key.
func scanPayloads(ctx context.Context, tx *sql.Tx, record, q string, args []any, add func(changeKey, string)) error {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("reading current %s payloads: %w", record, err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, data string
		if record == store.RecordEdge {
			var kind, source, target string
			if err := rows.Scan(&kind, &source, &target, &data); err != nil {
				return fmt.Errorf("reading current edge payload: %w", err)
			}
			key = store.EdgeKey(kind, source, target)
		} else if err := rows.Scan(&key, &data); err != nil {
			return fmt.Errorf("reading current %s payload: %w", record, err)
		}
		add(changeKey{record, key}, data)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading current %s payloads: %w", record, err)
	}
	return nil
}

// refChangeSet compares two namespaces (or a namespace and the current
// graph) with an inequality join and an anti-join per table.
func (s *DB) refChangeSet(ctx context.Context, tx *sql.Tx, repo string, base store.ChangeBase) (store.ChangeSet, error) {
	var out store.ChangeSet
	snaps, err := refSnapshots(ctx, tx, repo)
	if err != nil {
		return out, err
	}
	side := func(namespace string) (store.ChangeSide, bool) {
		for _, snap := range snaps {
			if snap.Namespace == namespace {
				return store.ChangeSide{Kind: store.SideRef, Repository: namespace, Run: snap.Run}, true
			}
		}
		return store.ChangeSide{}, false
	}
	var ok bool
	if out.Base, ok = side(base.Ref); !ok {
		return out, fmt.Errorf("comparing with %s: %w", base.Ref, store.ErrRefUnknown)
	}
	if base.Head != "" {
		if out.Head, ok = side(base.Head); !ok {
			return out, fmt.Errorf("comparing %s: %w", base.Head, store.ErrRefUnknown)
		}
	} else {
		runs, err := changeRuns(ctx, tx, repo)
		if err != nil {
			return out, err
		}
		if len(runs) == 0 {
			return out, fmt.Errorf("comparing %s: %w", repo, store.ErrNoJournal)
		}
		out.Head = store.ChangeSide{Kind: store.SideCurrent, Repository: repo, Run: runs[len(runs)-1]}
	}
	b, h := out.Base.Repository, out.Head.Repository
	out.Changes = []store.RecordChange{}
	for _, t := range []struct {
		record, table string
		columns       []string
	}{
		{store.RecordFile, "files", []string{"path"}},
		{store.RecordNode, "nodes", []string{"identity"}},
		{store.RecordEdge, "edges", []string{"kind", "source", "target"}},
	} {
		join := []string{}
		keys := []string{}
		for _, c := range t.columns {
			join = append(join, "b."+c+"=h."+c)
			keys = append(keys, "h."+c)
		}
		baseKeys := strings.ReplaceAll(strings.Join(keys, ","), "h.", "b.")
		q := "SELECT " + strings.Join(keys, ",") + ",b.payload,h.payload FROM " + t.table + " h LEFT JOIN " + t.table + " b ON b.repository=$1 AND " + strings.Join(join, " AND ") +
			" WHERE h.repository=$2 AND (b.payload IS NULL OR b.payload<>h.payload)" +
			" UNION ALL SELECT " + baseKeys + ",b.payload,NULL FROM " + t.table + " b WHERE b.repository=$1 AND NOT EXISTS (SELECT 1 FROM " + t.table + " h WHERE h.repository=$2 AND " + strings.Join(join, " AND ") + ")"
		rows, err := tx.QueryContext(ctx, q, b, h)
		if err != nil {
			return out, fmt.Errorf("comparing %s with %s: %w", h, b, err)
		}
		for rows.Next() {
			parts := make([]string, len(t.columns))
			dest := []any{}
			for i := range parts {
				dest = append(dest, &parts[i])
			}
			var before, after sql.NullString
			dest = append(dest, &before, &after)
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return out, fmt.Errorf("reading %s difference: %w", t.record, err)
			}
			key := parts[0]
			if t.record == store.RecordEdge {
				key = store.EdgeKey(parts[0], parts[1], parts[2])
			}
			c := store.RecordChange{Record: t.record, Key: key}
			if before.Valid {
				c.Before = json.RawMessage(before.String)
			}
			if after.Valid {
				c.After = json.RawMessage(after.String)
			}
			out.Changes = append(out.Changes, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, fmt.Errorf("reading %s differences: %w", t.record, err)
		}
	}
	sortChanges(out.Changes)
	return out, nil
}
