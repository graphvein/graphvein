package changes

import (
	"context"
	"fmt"

	"github.com/graphvein/graphvein/internal/store"
)

// Bases lists what a comparison can start from: the journal runs still
// covered (newest first), the pins and the ref snapshots.
type Bases struct {
	Runs []store.ChangeRun   `json:"runs"`
	Pins []store.Pin         `json:"pins"`
	Refs []store.RefSnapshot `json:"refs"`
}

// ListBases reads the bases of repository.
func ListBases(ctx context.Context, s store.Store, repository string) (Bases, error) {
	out := Bases{Runs: []store.ChangeRun{}, Pins: []store.Pin{}, Refs: []store.RefSnapshot{}}
	runs, err := s.ChangeRuns(ctx, repository)
	if err != nil {
		return out, fmt.Errorf("reading the journal of %s: %w", repository, err)
	}
	for i := len(runs) - 1; i >= 0; i-- {
		out.Runs = append(out.Runs, runs[i])
	}
	if out.Pins, err = s.Pins(ctx, repository); err != nil {
		return out, fmt.Errorf("reading the pins of %s: %w", repository, err)
	}
	if out.Refs, err = s.RefSnapshots(ctx, repository); err != nil {
		return out, fmt.Errorf("reading the ref snapshots of %s: %w", repository, err)
	}
	if out.Pins == nil {
		out.Pins = []store.Pin{}
	}
	if out.Refs == nil {
		out.Refs = []store.RefSnapshot{}
	}
	return out, nil
}

// PinRun pins the journal run runID (empty: the newest) under name, keeping
// at most limit pins. The caller holds the index lock.
func PinRun(ctx context.Context, s store.Store, repository, runID, name string, limit int) (store.Pin, error) {
	runs, err := s.ChangeRuns(ctx, repository)
	if err != nil {
		return store.Pin{}, fmt.Errorf("pinning in %s: reading the journal: %w", repository, err)
	}
	if len(runs) == 0 {
		return store.Pin{}, fmt.Errorf("pinning %s: %w", repository, store.ErrNoJournal)
	}
	seq := runs[len(runs)-1].Seq
	if runID != "" {
		seq = -1
		for _, r := range runs {
			if r.RunID == runID {
				seq = r.Seq
			}
		}
		if seq < 0 {
			all, err := s.Runs(ctx, repository)
			if err != nil {
				return store.Pin{}, fmt.Errorf("pinning run %s: reading runs: %w", runID, err)
			}
			for _, r := range all {
				if r.ID == runID {
					return store.Pin{}, fmt.Errorf("pinning run %s: %w", runID, store.ErrBaseExpired)
				}
			}
			return store.Pin{}, fmt.Errorf("pinning run %s: %w", runID, store.ErrBaseUnknown)
		}
	}
	return s.Pin(ctx, repository, seq, name, limit)
}

// UnpinName removes the pin called name. The caller holds the index lock.
func UnpinName(ctx context.Context, s store.Store, repository, name string) error {
	pins, err := s.Pins(ctx, repository)
	if err != nil {
		return fmt.Errorf("unpinning %s: reading pins: %w", name, err)
	}
	for _, p := range pins {
		if p.Name == name {
			return s.Unpin(ctx, repository, p.ID)
		}
	}
	return fmt.Errorf("unpinning %s: %w", name, store.ErrPinUnknown)
}
