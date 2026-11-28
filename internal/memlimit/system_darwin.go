//go:build darwin

package memlimit

import "golang.org/x/sys/unix"

// platform reads hw.memsize; macOS has no cgroups.
type platform struct{}

func (platform) CgroupLimit() (int64, bool) { return 0, false }

func (platform) PhysicalMemory() (int64, bool) {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil || n == 0 || n >= uint64(unlimitedAbove) {
		return 0, false
	}
	return int64(n), true
}
