//go:build graphveinprofile

package parse

// ProfileDetachedTree observes ownership transitions only in opt-in builds.
// Observers must be concurrency-safe and must not retain trees.
var ProfileDetachedTree func(int)

func profileDetachedTree(delta int) {
	if ProfileDetachedTree != nil {
		ProfileDetachedTree(delta)
	}
}
