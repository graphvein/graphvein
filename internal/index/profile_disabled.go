//go:build !graphveinprofile

package index

func profileCacheHit(string)                {}
func profileReplayValidation(string) func() { return nil }

func profileBoundary(string) {}
