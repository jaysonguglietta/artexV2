package isolation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactMapRejectsInodeAmplificationBeforeMutation(t *testing.T) {
	wide := make(map[string][]byte)
	for index := 0; index < 700; index++ {
		wide[fmt.Sprintf("branch-%d/one/two/empty", index)] = nil
	}
	for name, files := range map[string]map[string][]byte{
		"excessive depth": {strings.Repeat("a/", 2000) + "empty": nil},
		"excessive bytes": {strings.Repeat("long/", 31) + strings.Repeat("x", 901): nil},
		"long component":  {strings.Repeat("x", 256): nil},
		"directory count": wide,
		"parent conflict": {"parent": nil, "parent/child": nil},
		"NUL path":        {"parent/bad\x00name": nil},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			files["valid-parent/safe"] = []byte("must not be partially written")
			if err := WriteFiles(root, files); err == nil {
				t.Fatal("accepted hostile artifact map")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected artifact map mutated workspace", entries, err)
			}
		})
	}
}

func TestInvalidSnapshotCannotDeleteOriginalFiles(t *testing.T) {
	root := t.TempDir()
	before := map[string][]byte{"evidence.txt": []byte("preserve this")}
	if err := WriteFiles(root, before); err != nil {
		t.Fatal(err)
	}
	returned := map[string][]byte{strings.Repeat("a/", 2000) + "empty": nil}
	if err := ApplySnapshot(root, before, returned); err == nil {
		t.Fatal("accepted invalid snapshot")
	}
	actual, err := os.ReadFile(filepath.Join(root, "evidence.txt"))
	if err != nil || string(actual) != "preserve this" {
		t.Fatal("invalid snapshot removed existing evidence", err)
	}
}

func TestArtifactReadBoundsEmptyDirectories(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		root := t.TempDir()
		deep := filepath.Join(root, filepath.FromSlash(strings.Repeat("a/", maxArtifactPathDepth+1)))
		if err := os.MkdirAll(deep, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFiles(root, 4<<20); err == nil {
			t.Fatal("walk accepted excessive empty-directory depth")
		}
	})
	t.Run("count", func(t *testing.T) {
		root := t.TempDir()
		for index := 0; index <= maxArtifactDirectories; index++ {
			if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d-%04d", index)), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ReadFiles(root, 4<<20); err == nil {
			t.Fatal("walk accepted too many empty directories")
		}
	})
}

func TestArtifactReasonableNestedEvidenceStillSynchronizes(t *testing.T) {
	root := t.TempDir()
	name := strings.Repeat("a/", maxArtifactPathDepth-1) + "evidence.txt"
	if err := WriteFiles(root, map[string][]byte{name: []byte("evidence")}); err != nil {
		t.Fatal(err)
	}
	files, err := ReadFiles(root, 4<<20)
	if err != nil || string(files[name]) != "evidence" {
		t.Fatal("valid nested evidence failed to synchronize", err)
	}
}
