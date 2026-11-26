//go:build graphveinprofile

package ingest

import "time"

// ProfileObserver times wire reading and validation only in opt-in builds.
// It defaults to nil and cannot affect records or transaction decisions.
var ProfileObserver func(time.Duration)

func profileRead() func() {
	if ProfileObserver == nil {
		return nil
	}
	started := time.Now()
	return func() { ProfileObserver(time.Since(started)) }
}
