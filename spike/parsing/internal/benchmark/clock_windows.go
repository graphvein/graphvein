//go:build windows

package benchmark

import (
	"fmt"
	"syscall"
	"unsafe"
)

const timingMethod = "Windows QueryPerformanceCounter / QueryPerformanceFrequency (four host calls per parse)"

var performanceCounter = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
var performanceFrequency = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceFrequency")

func timestamp() (int64, error) {
	var count, frequency int64
	ok, _, err := performanceFrequency.Call(uintptr(unsafe.Pointer(&frequency)))
	if ok == 0 || frequency <= 0 {
		return 0, fmt.Errorf("QueryPerformanceFrequency: %v", err)
	}
	ok, _, err = performanceCounter.Call(uintptr(unsafe.Pointer(&count)))
	if ok == 0 {
		return 0, fmt.Errorf("QueryPerformanceCounter: %v", err)
	}
	// Split the quotient to avoid overflowing count * 1e9 on long uptimes.
	return count/frequency*1e9 + (count%frequency)*1e9/frequency, nil
}
