//go:build graphveinprofile

package index

import (
	"runtime/metrics"
	"time"
)

// ProfileObserver isolates local cache validation in opt-in measurement builds.
// A nil observer performs no measurement and never changes producer output.
var ProfileObserver func(time.Duration, uint64, uint64)

var profileReplayed map[string]bool

func profileCacheHit(file string) {
	if ProfileObserver == nil {
		return
	}
	if profileReplayed == nil {
		profileReplayed = map[string]bool{}
	}
	profileReplayed[file] = true
}

func profileReplayValidation(file string) func() {
	if ProfileObserver == nil || !profileReplayed[file] {
		return nil
	}
	var started time.Time
	var beforeBytes, beforeObjects uint64
	finish := func() {
		elapsed := time.Since(started)
		v := [2]metrics.Sample{{Name: "/gc/heap/allocs:bytes"}, {Name: "/gc/heap/allocs:objects"}}
		metrics.Read(v[:])
		ProfileObserver(elapsed, v[0].Value.Uint64()-beforeBytes, v[1].Value.Uint64()-beforeObjects)
	}
	v := [2]metrics.Sample{{Name: "/gc/heap/allocs:bytes"}, {Name: "/gc/heap/allocs:objects"}}
	metrics.Read(v[:])
	beforeBytes, beforeObjects = v[0].Value.Uint64(), v[1].Value.Uint64()
	started = time.Now()
	return finish
}

// ProfileBoundary samples phase transitions in opt-in builds only.
var ProfileBoundary func(string)

func profileBoundary(phase string) {
	if ProfileBoundary != nil {
		ProfileBoundary(phase)
	}
}
