package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Exercise the process-wide setting in a child so the test runner's QoS is not
// changed. Unsupported Windows versions retain the normal startup behavior.
func TestWindowsHighQoSInChild(t *testing.T) {
	const marker = "MEMORYS_TEST_HIGH_QOS_CHILD"
	const verified = "MEMORYS_QOS_READBACK_VERIFIED"
	const unavailable = "MEMORYS_QOS_UNAVAILABLE"
	if os.Getenv(marker) == "1" {
		if !requestWebHighQoS() {
			fmt.Println(unavailable)
			return
		}
		get := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessInformation")
		if err := get.Find(); err != nil {
			t.Fatal(err)
		}
		state := struct{ version, controlMask, stateMask uint32 }{version: 1}
		ok, _, callErr := get.Call(^uintptr(0), 4, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
		runtime.KeepAlive(&state)
		if ok == 0 || state.controlMask&1 == 0 || state.stateMask&1 != 0 {
			t.Fatalf("HighQoS read-back failed: %#v: %v", state, callErr)
		}
		fmt.Println(verified)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsHighQoSInChild$")
	command.Env = append(os.Environ(), marker+"=1")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated QoS check failed: %v\n%s", err, output)
	}
	if bytes.Contains(output, []byte(unavailable)) {
		t.Skip("Windows process QoS is unavailable in the isolated child")
	}
	if !bytes.Contains(output, []byte(verified)) {
		t.Fatalf("child did not confirm QoS read-back: %s", output)
	}
}
