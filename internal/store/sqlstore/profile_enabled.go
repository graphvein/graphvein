//go:build graphveinprofile

package sqlstore

import (
	"sort"
	"time"
	"unsafe"
)

// ProfileObserver observes exclusive transaction phases in serial profiling.
// Nil is the default; no production transaction decision depends on it.
var ProfileObserver func(string, time.Duration)

// ProfileMapObserver receives numeric lower bounds without retaining graph maps.
var ProfileMapObserver func(ProfileMapSnapshot)

// ProfileMapSnapshot distinguishes live referenced string bytes from map overhead.
// Rows are ordered files, nodes, edges; unique bytes merge overlapping addresses.
type ProfileMapSnapshot struct {
	Stage                              string
	OldRows, SeenRows                  [3]int
	OldPayloadBytes, UniqueStringBytes uint64
}

var profileActivePhase string

func profilePhase(phase string) func() {
	observer := ProfileObserver
	if observer == nil {
		return nil
	}
	previous := profileActivePhase
	// SQL under transaction setup, pruning or publication belongs to that phase.
	if phase == "writes" && previous != "" {
		return nil
	}
	profileActivePhase = phase
	started := time.Now()
	return func() { elapsed := time.Since(started); profileActivePhase = previous; observer(phase, elapsed) }
}

func profileMaps(stage string, t *transaction) {
	if ProfileMapObserver == nil {
		return
	}
	ProfileMapObserver(profileMapStrings(stage, t))
}

func profileMapStrings(stage string, t *transaction) ProfileMapSnapshot {
	type interval struct{ start, end uintptr }
	ranges := []interval{}
	add := func(s string) {
		if len(s) > 0 {
			start := uintptr(unsafe.Pointer(unsafe.StringData(s)))
			ranges = append(ranges, interval{start, start + uintptr(len(s))})
		}
	}
	result := ProfileMapSnapshot{Stage: stage}
	for i, table := range []string{"files", "nodes", "edges"} {
		result.OldRows[i], result.SeenRows[i] = len(t.old[table]), len(t.seen[table])
		for key, payload := range t.old[table] {
			add(key)
			add(payload)
			result.OldPayloadBytes += uint64(len(payload))
		}
		for key := range t.seen[table] {
			add(key)
		}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	var start, end uintptr
	for i, r := range ranges {
		if i == 0 {
			start, end = r.start, r.end
			continue
		}
		if r.start <= end {
			if r.end > end {
				end = r.end
			}
			continue
		}
		result.UniqueStringBytes += uint64(end - start)
		start, end = r.start, r.end
	}
	result.UniqueStringBytes += uint64(end - start)
	return result
}
