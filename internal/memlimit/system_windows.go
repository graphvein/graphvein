//go:build windows

package memlimit

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// platform reads GlobalMemoryStatusEx's total physical memory. A job
// object's memory limit is not read (ADR 0094, known gaps).
type platform struct{}

func (platform) CgroupLimit() (int64, bool) { return 0, false }

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length, MemoryLoad                                                                                   uint32
	TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func (platform) PhysicalMemory() (int64, bool) {
	m := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if globalMemoryStatusEx.Find() != nil {
		return 0, false
	}
	ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if ok == 0 || m.TotalPhys == 0 || m.TotalPhys >= uint64(unlimitedAbove) {
		return 0, false
	}
	return int64(m.TotalPhys), true
}
