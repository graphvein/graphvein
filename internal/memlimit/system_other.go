//go:build !linux && !darwin && !windows

package memlimit

// platform reads nothing on other systems, so no limit is set.
type platform struct{}

func (platform) CgroupLimit() (int64, bool) { return 0, false }

func (platform) PhysicalMemory() (int64, bool) { return 0, false }
