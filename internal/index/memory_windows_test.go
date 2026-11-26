//go:build windows

package index_test

import (
	"fmt"
	"syscall"
	"unsafe"
)

type memoryCounters struct {
	Size, PageFaults                                                                                                                 uint32
	PeakWorkingSet, WorkingSet, QuotaPeakPagedPool, QuotaPagedPool, QuotaPeakNonPagedPool, QuotaNonPagedPool, Pagefile, PeakPagefile uintptr
}

func peakMemory() (uint64, uint64, string) {
	proc := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0, fmt.Sprint(err)
	}
	c := memoryCounters{Size: uint32(unsafe.Sizeof(memoryCounters{}))}
	ok, _, err := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&c)), uintptr(c.Size))
	if ok == 0 {
		return 0, 0, fmt.Sprint(err)
	}
	return uint64(c.PeakWorkingSet), uint64(c.PeakPagefile), "GetProcessMemoryInfo: PeakWorkingSetSize / PeakPagefileUsage"
}
