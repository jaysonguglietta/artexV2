package guard

import (
	"context"
	"github.com/Autumn-27/norma/hook"
	"testing"
)

func TestExecutionDisabledByDefault(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_HOST_EXECUTION", "")
	g := New()
	r := g.preToolUse(context.Background(), hook.Event{ToolName: "Bash"})
	if r.Decision != "block" {
		t.Fatal("execution allowed without explicit opt-in")
	}
}
