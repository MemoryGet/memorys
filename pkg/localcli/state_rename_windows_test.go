//go:build windows

package localcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func lockedStateDirectory(t *testing.T) (string, string, windows.Handle) {
	t.Helper()
	parent := t.TempDir()
	stage, err := os.MkdirTemp(parent, ".runtime-pending-")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(stage, "private.pem")
	if err = os.WriteFile(file, []byte("synthetic private material"), 0600); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	// A scanner or other reader can open an initialized file without sharing
	// delete access, which also blocks renaming its containing directory.
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return stage, filepath.Join(parent, "runtime"), handle
}

func TestStateDirectoryCommitRecoversFromTemporaryWindowsReader(t *testing.T) {
	stage, target, handle := lockedStateDirectory(t)
	if err := os.Rename(stage, target); err == nil || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		windows.CloseHandle(handle)
		t.Fatal("the original directory rename did not reproduce access denied", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(80 * time.Millisecond)
		windows.CloseHandle(handle)
		close(released)
	}()
	err := commitStateDirectory(stage, target)
	<-released
	if err != nil {
		t.Fatal("transient reader prevented state commit", err)
	}
	bytes, err := os.ReadFile(filepath.Join(target, "private.pem"))
	if err != nil || string(bytes) != "synthetic private material" {
		t.Fatal("committed private file changed")
	}
}

func TestStateDirectoryCommitRetainsPrivateStateOnPersistentWindowsReader(t *testing.T) {
	stage, target, handle := lockedStateDirectory(t)
	defer windows.CloseHandle(handle)
	started := time.Now()
	err := commitStateDirectory(stage, target)
	if !errors.Is(err, errState) || time.Since(started) > 5*time.Second {
		t.Fatal("persistent denial was not bounded", err)
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("failed commit exposed a final state directory")
	}
	bytes, err := os.ReadFile(filepath.Join(stage, "private.pem"))
	if err != nil || string(bytes) != "synthetic private material" {
		t.Fatal("failed commit lost private staged state")
	}
}

func TestStateDirectoryMoveDoesNotReplaceConcurrentDestination(t *testing.T) {
	parent := t.TempDir()
	stage, err := os.MkdirTemp(parent, ".runtime-pending-")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "runtime")
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	// Exercise the syscall itself, after another initializer could win the
	// race; the target is intentionally empty, so no-overwrite is meaningful.
	if err = renameStateDirectory(stage, target); err == nil {
		t.Fatal("directory move replaced an existing destination")
	}
	if err = commitStateDirectory(stage, target); err == nil {
		t.Fatal("staged directory overwrote another initializer")
	}
	if _, err = os.Stat(stage); err != nil {
		t.Fatal("losing staged state was removed")
	}
}

func TestStateDirectoryCommitSupportsLongWindowsPath(t *testing.T) {
	parent := t.TempDir()
	for i := 0; i < 6; i++ {
		parent = filepath.Join(parent, strings.Repeat("segment-", 8))
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(parent, ".runtime-pending-")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "runtime")
	if err = commitStateDirectory(stage, target); err != nil {
		t.Fatal("long path state commit failed", err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatal("committed directory is missing")
	}
}
