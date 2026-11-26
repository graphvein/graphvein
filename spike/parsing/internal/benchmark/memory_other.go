//go:build !windows

package benchmark

func peakMemory() (uint64, uint64, string) {
	return 0, 0, "unavailable: Windows peak process counters are not supported on this OS"
}
