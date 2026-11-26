//go:build windows

package benchmark

import (
	"fmt"
	"syscall"
	"unsafe"
)

// PROCESS_MEMORY_COUNTERS uses SIZE_T for the eight counters on either word size.
type memoryCounters struct {
	Size, PageFaults                         uint32
	PeakWorkingSet, WorkingSet               uintptr
	QuotaPeakPagedPool, QuotaPagedPool       uintptr
	QuotaPeakNonPagedPool, QuotaNonPagedPool uintptr
	Pagefile, PeakPagefile                   uintptr
}

func peakMemory() (uint64, uint64, string) {
	getMemory := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0, fmt.Sprintf("unavailable: GetCurrentProcess: %v", err)
	}
	counters := memoryCounters{Size: uint32(unsafe.Sizeof(memoryCounters{}))}
	ok, _, err := getMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		return 0, 0, fmt.Sprintf("unavailable: GetProcessMemoryInfo: %v", err)
	}
	return uint64(counters.PeakWorkingSet), uint64(counters.PeakPagefile), "Windows GetProcessMemoryInfo: PeakWorkingSetSize / PeakPagefileUsage"
}
