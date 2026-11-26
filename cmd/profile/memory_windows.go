//go:build graphveinprofile && windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

type counters struct {
	Size, Faults                                                               uint32
	PeakWS, WS, PeakPaged, Paged, PeakNonPaged, NonPaged, Private, PeakPrivate uintptr
}

func processMemory() (uint64, uint64, error) {
	c := counters{Size: uint32(unsafe.Sizeof(counters{}))}
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0, err
	}
	ok, _, err := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo").Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.Size))
	if ok == 0 {
		return 0, 0, fmt.Errorf("GetProcessMemoryInfo: %w", err)
	}
	return uint64(c.PeakWS), uint64(c.PeakPrivate), nil
}

func hostEnvironment() map[string]any {
	host := map[string]any{"cpu_model": os.Getenv("PROCESSOR_IDENTIFIER"), "ram_bytes": "not measured"}
	b, err := exec.Command("reg.exe", "query", `HKLM\HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "/v", "ProcessorNameString").Output()
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if pos := strings.Index(line, "REG_SZ"); pos >= 0 {
				host["cpu_model"] = strings.TrimSpace(line[pos+len("REG_SZ"):])
			}
		}
	}
	type status struct {
		Length, Load                                                                                  uint32
		Physical, AvailablePhysical, Pagefile, AvailablePagefile, Virtual, AvailableVirtual, Extended uint64
	}
	m := status{Length: uint32(unsafe.Sizeof(status{}))}
	ok, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&m)))
	if ok != 0 {
		host["ram_bytes"] = m.Physical
	}
	return host
}
