package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestPlatformSkillToolsRejectSymlinkEscapes(t *testing.T) {
	skills, outside := t.TempDir(), t.TempDir()
	canary := filepath.Join(outside, "canary.txt")
	if err := os.WriteFile(canary, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skills, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(skills, "valid"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skills, "valid", "escape")); err != nil {
		t.Fatal(err)
	}
	s := &Server{skillDir: skills}
	for _, tc := range []struct {
		name  string
		tool  actool.CoreTool
		input string
	}{
		{"create existing escape", s.toolCreateSkill(), `{"name":"escape","description":"test"}`},
		{"update escaped skill", s.toolUpdateSkillFile(), `{"name":"escape","file":"canary.txt","content":"changed"}`},
		{"update escaped parent", s.toolUpdateSkillFile(), `{"name":"valid","file":"escape/canary.txt","content":"changed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.tool.Call(t.Context(), json.RawMessage(tc.input), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("escape accepted: %s", result.Flatten())
			}
			got, err := os.ReadFile(canary)
			if err != nil || string(got) != "unchanged" {
				t.Fatalf("outside canary changed: %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); !os.IsNotExist(err) {
				t.Fatalf("created outside skill: %v", err)
			}
		})
	}
}

func TestPlatformSkillToolsKeepNormalWorkflow(t *testing.T) {
	skills := t.TempDir()
	s := &Server{skillDir: skills}
	for _, tc := range []struct {
		tool  actool.CoreTool
		input string
	}{
		{s.toolCreateSkill(), `{"name":"reviewed","description":"Reviewed local skill","instructions":"Use only approved targets."}`},
		{s.toolUpdateSkillFile(), `{"name":"reviewed","file":"references/notes.md","content":"Reviewed guidance"}`},
	} {
		result, err := tc.tool.Call(t.Context(), json.RawMessage(tc.input), nil)
		if err != nil || result.IsError {
			t.Fatalf("normal skill operation failed: %+v %v", result, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(skills, "reviewed", "references", "notes.md"))
	if err != nil || string(got) != "Reviewed guidance" {
		t.Fatalf("wrong content: %q %v", got, err)
	}
}
