package localcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLoadParseTasksMakesInterruptedRunsRetryable(t *testing.T) {
	root := t.TempDir()
	task := parseTask{ID: "parse-1", State: "running", Stage: "document.parse", Attempt: 1, RowVersion: 2, InputPath: filepath.Join(root, "input.txt"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal([]parseTask{task})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, parseTaskFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadParseTasks(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[task.ID].State != "failed" || loaded[task.ID].Error == "" || loaded[task.ID].RowVersion != 3 {
		t.Fatalf("interrupted task was not made retryable: %+v", loaded[task.ID])
	}
}

func TestParseTaskPersistenceAndPublicProjectionOmitInputPath(t *testing.T) {
	root := t.TempDir()
	task := parseTask{ID: contracts.ID("parse-2"), State: "completed", Stage: "document.parse", Attempt: 1, RowVersion: 3, InputPath: filepath.Join(root, "secret-input.pdf"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := persistParseTasks(root, map[contracts.ID]parseTask{task.ID: task}); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadParseTasks(root)
	if err != nil || loaded[task.ID].InputPath != task.InputPath {
		t.Fatalf("task persistence failed: %+v %v", loaded, err)
	}
	public, err := json.Marshal(publicParseTask(task))
	if err != nil || string(public) == "" {
		t.Fatal(err)
	}
	if string(public) == "" || containsBytes(public, []byte("secret-input.pdf")) {
		t.Fatal("internal input path leaked in public task")
	}
}

func TestSafeParseTaskErrorDoesNotExposeProviderDetails(t *testing.T) {
	if got := safeParseTaskError(context.DeadlineExceeded); got != "解析任务已取消或超时，可重试。" {
		t.Fatal(got)
	}
	if got := safeParseTaskError(os.ErrPermission); got != "解析任务失败，可重试。" {
		t.Fatal(got)
	}
}

func containsBytes(raw, needle []byte) bool {
	for i := 0; i+len(needle) <= len(raw); i++ {
		match := true
		for j := range needle {
			if raw[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
