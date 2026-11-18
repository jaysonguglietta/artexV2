package agent

import (
	"context"
	"github.com/Autumn-27/norma/agentcore"
	actool "github.com/Autumn-27/norma/tool"
	"os"
	"strings"
	"testing"
)

func TestIsolatedSDKOutputCannotSpillIntoAPIWorkspace(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_HOST_EXECUTION", "")
	directory := t.TempDir()
	opts := isolatedOptions(context.Background(), agentcore.Options{ToolOutputDir: directory, Tools: []actool.CoreTool{}})
	if opts.ToolOutputDir != "" {
		t.Fatal("production SDK retained an unadmitted spill directory")
	}
	output := actool.CaptureOnce(&actool.ToolContext{WorkingDir: directory, OutputDir: opts.ToolOutputDir, MaxOutputChars: 128}, strings.Repeat("untrusted-output", 1000))
	if len(output) > 1000 || !strings.Contains(output, "truncated") {
		t.Fatal("SDK did not use bounded output truncation")
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 0 {
		t.Fatal("SDK wrote API-side spill files", err)
	}
}
