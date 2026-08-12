//go:build windows

package perfharness

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	psapiDLL             = syscall.NewLazyDLL("psapi.dll")
	getProcessMemoryInfo = psapiDLL.NewProc("GetProcessMemoryInfo")
)

type processMemoryCountersEx struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
	privateUsage               uintptr
}

func sampleProcessMetrics() (processMetrics, error) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return processMetrics{}, fmt.Errorf("get Windows process handle: %w", err)
	}
	memory := processMemoryCountersEx{cb: uint32(unsafe.Sizeof(processMemoryCountersEx{}))}
	result, _, callErr := getProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&memory)),
		uintptr(memory.cb),
	)
	if result == 0 {
		return processMetrics{}, fmt.Errorf("read Windows process memory: %w", callErr)
	}
	return processMetrics{
		windowsWorkingSet: uint64(memory.peakWorkingSetSize),
		committed:         uint64(memory.privateUsage),
		swapOrPagefile:    uint64(memory.peakPagefileUsage),
	}, nil
}
