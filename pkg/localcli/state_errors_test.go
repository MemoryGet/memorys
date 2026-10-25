package localcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStateErrorsKeepOperationWithoutSensitiveContext(t *testing.T) {
	secret := "credential-and-private-path-never-log"
	err := stateFailure("commit state file", &os.PathError{Op: "rename", Path: secret, Err: syscall.EACCES})
	if !errors.Is(err, errState) || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "commit state file") || !strings.Contains(err.Error(), "system code") {
		t.Fatal("state error lost its classification or disclosed sensitive context")
	}
	wrapped := stateFailure("create local state", err)
	if wrapped.Error() != err.Error() {
		t.Fatal("specific failure was replaced with a generic outer stage")
	}
}

func TestAtomicStateFileFailureHasSafeStage(t *testing.T) {
	root := t.TempDir()
	privateText := []byte("synthetic signing material must stay out of errors")
	err := writeAtomic(filepath.Join(root, "missing-private-directory"), "private.pem", privateText)
	if !errors.Is(err, errState) || !strings.Contains(err.Error(), "create temporary state file") || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), string(privateText)) {
		t.Fatal("temporary file failure was not safely diagnosed")
	}
	// A real failed rename must leave the pre-existing target intact and remove
	// its temporary private payload. This exercises the same commit call site.
	if err = os.Mkdir(filepath.Join(root, "private.pem"), 0700); err != nil {
		t.Fatal(err)
	}
	err = writeAtomic(root, "private.pem", privateText)
	if !errors.Is(err, errState) || !strings.Contains(err.Error(), "commit state file") || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), string(privateText)) {
		t.Fatal("rename failure was not safely diagnosed")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 || files[0].Name() != "private.pem" || !files[0].IsDir() {
		t.Fatal("failed commit damaged its target or left a temporary payload")
	}
}

func TestStateParentFailureIsDiagnosedWithoutReplacingExistingFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "existing-sensitive-file")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := createState(filepath.Join(parent, "runtime"))
	if !errors.Is(err, errState) || strings.Contains(err.Error(), parent) {
		t.Fatal("invalid parent was not safely refused")
	}
	raw, readErr := os.ReadFile(parent)
	if readErr != nil || string(raw) != "keep" {
		t.Fatal("existing file was changed")
	}
}

func TestStateDirectoryCommitStaysInsideSelectedParent(t *testing.T) {
	parent := t.TempDir()
	stage, err := os.MkdirTemp(parent, ".runtime-pending-")
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "runtime")
	if err = commitStateDirectory(stage, other); !errors.Is(err, errState) {
		t.Fatal("directory commit escaped the selected parent")
	}
	if _, err = os.Stat(stage); err != nil {
		t.Fatal("refused move changed the source")
	}
	if _, err = os.Lstat(other); !os.IsNotExist(err) {
		t.Fatal("refused move exposed a destination")
	}
}
