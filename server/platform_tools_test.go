package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillTools functionally exercises create_skill + update_skill_file against a
// temp skill dir (no DB needed — these tools only touch the filesystem).
func TestSkillTools(t *testing.T) {
	dir := t.TempDir()
	s := &Server{skillDir: dir}

	// create_skill writes a spec-compliant SKILL.md.
	in, _ := json.Marshal(map[string]any{
		"name": "my-skill", "description": "does X", "instructions": "## do\n1. a",
	})
	res, err := s.toolCreateSkill().Call(context.Background(), in, nil)
	if err != nil || res.IsError {
		t.Fatalf("create_skill: err=%v result=%+v", err, res)
	}
	md, err := os.ReadFile(filepath.Join(dir, "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md not written: %v", err)
	}
	if !strings.Contains(string(md), "name: my-skill") || !strings.Contains(string(md), "description: does X") || !strings.Contains(string(md), "1. a") {
		t.Fatalf("SKILL.md content:\n%s", md)
	}

	// creating the same skill again fails.
	if r, _ := s.toolCreateSkill().Call(context.Background(), in, nil); !r.IsError {
		t.Fatal("duplicate create should error")
	}

	// invalid name rejected.
	bad, _ := json.Marshal(map[string]any{"name": "Bad Name", "description": "x"})
	if r, _ := s.toolCreateSkill().Call(context.Background(), bad, nil); !r.IsError {
		t.Fatal("invalid skill name should error")
	}

	// update_skill_file writes a nested script; path traversal is rejected.
	up, _ := json.Marshal(map[string]any{"name": "my-skill", "file": "scripts/run.py", "content": "print(1)"})
	if r, err := s.toolUpdateSkillFile().Call(context.Background(), up, nil); err != nil || r.IsError {
		t.Fatalf("update_skill_file: err=%v result=%+v", err, r)
	}
	got, err := os.ReadFile(filepath.Join(dir, "my-skill", "scripts", "run.py"))
	if err != nil || string(got) != "print(1)" {
		t.Fatalf("nested file not written: %v %q", err, got)
	}
	evil, _ := json.Marshal(map[string]any{"name": "my-skill", "file": "../escape.txt", "content": "x"})
	if r, _ := s.toolUpdateSkillFile().Call(context.Background(), evil, nil); !r.IsError {
		t.Fatal("path traversal should be rejected")
	}
}

func TestSkillToolsRefuseManagedDataRootOverlap(t *testing.T) {
	dir := t.TempDir()
	for _, relative := range []string{".", "tasks/skills", "evidence/skills", "traffic/skills"} {
		t.Run(relative, func(t *testing.T) {
			skills := filepath.Join(dir, relative)
			if err := os.MkdirAll(filepath.Join(skills, "retained-skill"), 0700); err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(skills, "retained-skill", "SKILL.md")
			if err := os.WriteFile(canary, []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			s := &Server{skillDir: skills, m: &Manager{dir: dir}}
			create := json.RawMessage(`{"name":"new-skill","description":"must not write"}`)
			update := json.RawMessage(`{"name":"retained-skill","content":"must not replace"}`)
			if result, err := s.toolCreateSkill().Call(t.Context(), create, nil); err != nil || !result.IsError {
				t.Fatalf("create admitted managed-root overlap: %+v %v", result, err)
			}
			if result, err := s.toolUpdateSkillFile().Call(t.Context(), update, nil); err != nil || !result.IsError {
				t.Fatalf("update admitted managed-root overlap: %+v %v", result, err)
			}
			if _, err := os.Lstat(filepath.Join(skills, "new-skill")); !os.IsNotExist(err) {
				t.Fatalf("refused tool created data: %v", err)
			}
			if raw, err := os.ReadFile(canary); err != nil || string(raw) != "retained" {
				t.Fatalf("refused tool changed retained data: %q %v", raw, err)
			}
		})
	}
}
