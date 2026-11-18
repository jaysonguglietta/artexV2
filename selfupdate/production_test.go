package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// This is a REAL production binary: testing.Testing is false. Running another
// copy of the Go test executable would not exercise the production guard.
func TestProductionIgnoresUnsignedStagingAndLegacyFlag(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe")
	if runtime.GOOS == "windows" {
		probe += ".exe"
	}
	source := filepath.Join(dir, "probe.go")
	code := `package main
import("context";"encoding/json";"os";"github.com/Autumn-27/artex/selfupdate")
func main(){a,s:=selfupdate.Bootstrap();stage:=selfupdate.Stage(context.Background(),nil,nil,"fixture",nil);rollback:=selfupdate.Rollback();selfupdate.Settle();json.NewEncoder(os.Stdout).Encode(map[string]any{"action":int(a),"pending":s.Pending,"stage_refused":stage!=nil,"rollback_refused":rollback!=nil,"backup":selfupdate.HasBackup()})}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", probe, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build production probe: %v: %s", err, output)
	}
	stem := filepath.Join(dir, "probe")
	staged := stem + ".new"
	if runtime.GOOS == "windows" {
		staged += ".exe"
	}
	backup := stem + ".old"
	if runtime.GOOS == "windows" {
		backup += ".exe"
	}
	content := []byte("unsigned staged executable must remain untouched\n")
	sum := sha256.Sum256(content)
	originals := map[string][]byte{staged: content, staged + ".sha256": []byte(hex.EncodeToString(sum[:])), stem + ".upgrade.json": []byte(`{"from":"1.0.0","to":"9.9.9","attempts":100,"staged_at":1}`), backup: []byte("unsigned prior executable")}
	for name, data := range originals {
		if err := os.WriteFile(name, data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(probe)
	command.Env = append(os.Environ(), "ARTEX_ENABLE_SELF_UPDATE=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v: %s", err, output)
	}
	var result struct {
		Action          int
		Pending         bool
		StageRefused    bool `json:"stage_refused"`
		RollbackRefused bool `json:"rollback_refused"`
		Backup          bool
	}
	if err = json.Unmarshal(output, &result); err != nil {
		t.Fatal(err, string(output))
	}
	if result.Action != 0 || result.Pending || !result.StageRefused || !result.RollbackRefused || result.Backup {
		t.Fatalf("production updater enabled: %+v", result)
	}
	for name, want := range originals {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != string(want) {
			t.Fatalf("legacy staging changed: %s %v", filepath.Base(name), err)
		}
	}
}
