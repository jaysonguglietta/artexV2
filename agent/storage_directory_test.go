package agent

import (
	"errors"
	"github.com/Autumn-27/artex/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDirectoryCreationRefusesExhaustedEntryBudget(t *testing.T) {
	directory := t.TempDir()
	if _, err := storage.Open(directory, storage.Workspace, storage.Limits{Bytes: 1024, Entries: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "already-full"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRunDir(directory, 42, 99); !errors.Is(err, storage.ErrQuota) {
		t.Fatalf("intent directory bypassed metadata quota: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "tasks")); !os.IsNotExist(err) {
		t.Fatal("run directory was created before admission")
	}
}
