package store

import (
	"encoding/json"
	"fmt"
	"sort"
)

// DiffSnapshots is the key-wise difference of two Dumps in ChangeSet form:
// every file, node and edge whose encoded record differs, sorted by record
// and key. Diagnostics are not compared (ADR 0056: they are republished
// every run and not journaled). It is the reference the journal is checked
// against (the T019 oracle and the store tests): a run transaction stores
// exactly json.Marshal of each record, so the encoded records of two Dumps
// are the stored payloads.
func DiffSnapshots(a, b Snapshot) ([]RecordChange, error) {
	before, err := snapshotPayloads(a)
	if err != nil {
		return nil, err
	}
	after, err := snapshotPayloads(b)
	if err != nil {
		return nil, err
	}
	out := []RecordChange{}
	for k, old := range before {
		c := RecordChange{Record: k[0], Key: k[1], Before: json.RawMessage(old)}
		if now, ok := after[k]; ok {
			if now == old {
				continue
			}
			c.After = json.RawMessage(now)
		}
		out = append(out, c)
	}
	for k, now := range after {
		if _, ok := before[k]; !ok {
			out = append(out, RecordChange{Record: k[0], Key: k[1], After: json.RawMessage(now)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Record != out[j].Record {
			return out[i].Record < out[j].Record
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func snapshotPayloads(s Snapshot) (map[[2]string]string, error) {
	out := map[[2]string]string{}
	add := func(record, key string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encoding %s %s: %w", record, key, err)
		}
		out[[2]string{record, key}] = string(data)
		return nil
	}
	for _, f := range s.Files {
		if err := add(RecordFile, f.Path, f); err != nil {
			return nil, err
		}
	}
	for _, n := range s.Nodes {
		if err := add(RecordNode, n.Identity, n); err != nil {
			return nil, err
		}
	}
	for _, e := range s.Edges {
		if err := add(RecordEdge, EdgeKey(e.Kind, e.Source, e.Target), e); err != nil {
			return nil, err
		}
	}
	return out, nil
}
