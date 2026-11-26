//go:build !windows

package index_test

func peakMemory() (uint64, uint64, string) { return 0, 0, "unavailable on this OS" }
