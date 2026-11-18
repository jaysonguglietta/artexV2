package isolation

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyRequiresExplicitTargetsAndManagementDenies(t *testing.T) {
	valid := Policy{Sessions: map[string][]string{"task-1": {"198.18.0.10/32"}}, DNS: []string{"1.1.1.1"}, Management: []string{"192.168.0.0/16"}}
	if err := ValidatePolicy(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Sessions = map[string][]string{"../escape": {"198.18.0.10/32"}}
	if ValidatePolicy(invalid) == nil {
		t.Fatal("unsafe session accepted")
	}
	invalid = valid
	invalid.Sessions = map[string][]string{"task-1": {"0.0.0.0/0"}}
	if ValidatePolicy(invalid) == nil {
		t.Fatal("default route accepted")
	}
	invalid = valid
	invalid.Management = nil
	if ValidatePolicy(invalid) == nil {
		t.Fatal("missing management denial accepted")
	}
}
func TestBrokerRejectsAuthenticationAndUnapprovedSessionsBeforeDocker(t *testing.T) {
	b, err := NewBroker(Policy{Sessions: map[string][]string{}, DNS: []string{"1.1.1.1"}, Management: []string{"192.168.0.0/16"}}, "sha256:"+strings.Repeat("a", 64), "fixture", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		token, body string
		want        int
	}{{"", `{}`, 401}, {b.Token, `{"session":"task-1","tool":"Bash","input":{}}`, 403}, {b.Token, `{"session":"../escape"}`, 403}, {b.Token, `{"unknown":true}`, 400}} {
		r := httptest.NewRequest("POST", "/v1/call", strings.NewReader(test.body))
		r.Header.Set("Authorization", "Bearer "+test.token)
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("status %d; want %d", w.Code, test.want)
		}
	}
}
func TestArtifactConfinementAndLimits(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../canary", "escape/canary", "/absolute", "a\\b"} {
		if WriteFiles(root, map[string][]byte{name: []byte("overwrite")}) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if err := WriteFiles(root, map[string][]byte{"i1/result.txt": []byte("safe")}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "unchanged" {
		t.Fatal("canary changed")
	}
	if _, err = ReadFiles(root, 1); err == nil {
		t.Fatal("size cap ignored")
	}
	files, err := ReadFiles(root, 100)
	if err != nil || string(files["i1/result.txt"]) != "safe" {
		t.Fatal(files, err)
	}
}
func TestSessionIdentityAndNoLocalFallback(t *testing.T) {
	session, root, cwd, err := SessionFor(12, "/data/tasks/12/i7")
	if err != nil || session != "task-12" || root != "/data/tasks/12" || cwd != "i7" {
		t.Fatal(session, root, cwd, err)
	}
	for _, dir := range []string{"/data/tasks/123", "/etc", "/data"} {
		if _, _, _, err := SessionFor(12, dir); err == nil {
			t.Fatal("wrong task root accepted")
		}
	}
	if _, _, _, err := SessionFor(0, "/app/data"); err == nil {
		t.Fatal("service root accepted as a session workspace")
	}
	t.Setenv("ARTEX_WORKER_URL", "")
	t.Setenv("ARTEX_WORKER_TOKEN_FILE", "")
	_, err = Call(context.Background(), Request{Tool: "Bash", Input: json.RawMessage(`{"command":"touch must-not-run"}`)})
	if err == nil {
		t.Fatal("missing broker did not fail closed")
	}
}

func TestSnapshotDeletionPreservesConcurrentUIFiles(t *testing.T) {
	root := t.TempDir()
	original := map[string][]byte{"deleted": []byte("old"), "edited": []byte("before"), "normal": []byte("old")}
	if err := WriteFiles(root, original); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(root, map[string][]byte{"edited": []byte("human edit"), "new-upload": []byte("human upload")}); err != nil {
		t.Fatal(err)
	}
	returned := map[string][]byte{"edited": []byte("stale worker edit"), "normal": []byte("worker output")}
	if err := ApplySnapshot(root, original, returned); err != nil {
		t.Fatal(err)
	}
	files, err := ReadFiles(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := files["deleted"]; exists {
		t.Fatal("worker deletion was resurrected")
	}
	if string(files["edited"]) != "human edit" || string(files["new-upload"]) != "human upload" || string(files["normal"]) != "worker output" {
		t.Fatal("incorrect merge", files)
	}
}

func TestWorkerNetworkCannotUseHostOrSharedNamespaces(t *testing.T) {
	policy := Policy{Sessions: map[string][]string{}, DNS: []string{"1.1.1.1"}, Management: []string{"192.168.0.0/16"}}
	for _, network := range []string{"host", "none", "bridge", "container:service", "--privileged"} {
		if _, err := NewBroker(policy, "sha256:"+strings.Repeat("a", 64), network, strings.Repeat("b", 32)); err == nil {
			t.Fatal("unsafe network accepted", network)
		}
	}
}
