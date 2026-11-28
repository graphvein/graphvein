//go:build linux

package memlimit

import "os"

// platform reads the cgroup v1/v2 memory limit and /proc/meminfo.
type platform struct{}

func (platform) CgroupLimit() (int64, bool) { return CgroupLimitFS(os.DirFS("/")) }

func (platform) PhysicalMemory() (int64, bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	return MemTotal(b)
}
