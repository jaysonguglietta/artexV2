package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCommandArgumentsStayLiteral(t *testing.T) {
	for _, template := range []string{`printf "%s" {target}`, `printf "%s" "{target}"`, `printf '%s' '{target}'`} {
		words, err := commandWords(template)
		if err != nil {
			t.Fatal(err)
		}
		value := "$(printf INJECTION_CANARY); ' \" `printf CANARY`"
		for i := 1; i < len(words); i++ {
			words[i] = renderTemplate(words[i], map[string]any{"target": value}, identity)
		}
		out, err := exec.Command(words[0], words[1:]...).Output()
		if err != nil || string(out) != value {
			t.Fatalf("argument interpretation: %q %v", out, err)
		}
	}
	for _, s := range []string{"echo $(id)", "echo x | cat", "echo `id`", "echo x; id"} {
		if _, err := commandWords(s); err == nil {
			t.Fatalf("accepted shell syntax: %s", s)
		}
	}
	if got := renderTemplate("{a}", map[string]any{"a": "{b}", "b": "expanded"}, identity); got != "{b}" {
		t.Fatalf("recursive substitution: %s", got)
	}
}

func TestToolEnvironmentAndOutputLimits(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	t.Setenv("ARTEX_PG_DSN", "must-not-leak")
	for _, v := range toolEnvironment(nil, t.TempDir()) {
		if strings.Contains(v, "must-not-leak") {
			t.Fatal("service secret inherited")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b := &boundedToolOutput{cancel: cancel}
	b.Write([]byte(strings.Repeat("x", 2<<20)))
	if !b.exceeded || len(b.String()) != 1<<20 || ctx.Err() == nil {
		t.Fatal("output cap failed")
	}
}
