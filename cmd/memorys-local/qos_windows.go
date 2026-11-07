package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// requestWebHighQoS affects only this process. Windows may still choose its
// processors; failure leaves the OS-managed default and does not prevent start.
func requestWebHighQoS() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetProcessInformation")
	if proc.Find() != nil {
		return false
	}
	// PROCESS_POWER_THROTTLING_STATE: disable execution-speed throttling.
	state := struct{ version, controlMask, stateMask uint32 }{1, 1, 0}
	result, _, _ := proc.Call(^uintptr(0), 4, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
	runtime.KeepAlive(&state)
	return result != 0
}
