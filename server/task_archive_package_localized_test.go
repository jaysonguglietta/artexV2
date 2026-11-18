package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Exercise English archive-delete staging errors with real temporary files and no database. The contradictory original/staged-file error propagates through task_archives.error directly to the archive UI.
func TestTaskArchiveStagedAndOriginalCoexistErrorLocalized(t *testing.T) {
	dataDir := t.TempDir()
	archivePath := taskArchivePath(dataDir, 21, "42")
	if err := os.MkdirAll(filepath.Dir(archivePath), archiveDirMode); err != nil {
		t.Fatal(err)
	}
	// Create both the original package and deletion staging path to trigger the inconsistency.
	if err := os.WriteFile(archivePath, []byte("original"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	staged := archivePath + ".deleting-21"
	if err := os.WriteFile(staged, []byte("archive"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	_, moved, err := stageTaskArchivePackageDelete(archivePath, 21)
	if err == nil {
		t.Fatalf("Coexisting original/staged files must return an error (moved=%v)", moved)
	}
	if moved {
		t.Fatal("Coexistence inconsistency must not report moved=true")
	}
	assertEnglishError(t, "staged_and_original_coexist", err.Error())
}
