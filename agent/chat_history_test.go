package agent

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/storage"
	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

type historyFixtureProvider struct {
	requests []llm.CompletionRequest
	reply    string
}

func (p *historyFixtureProvider) Complete(_ context.Context, request llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.requests = append(p.requests, request)
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(p.reply)}}, "end_turn", llm.Usage{}, nil
}
func (p *historyFixtureProvider) Stream(_ context.Context, request llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		p.requests = append(p.requests, request)
		if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: p.reply}, nil) {
			return
		}
		if !yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "end_turn"}, nil) {
			return
		}
		yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	}
}
func fixtureNoChatTools(t *testing.T) {
	t.Helper()
	oldAugment, oldResolve := ToolAugment, ToolResolve
	ToolAugment = nil
	ToolResolve = func(context.Context, string, []actool.CoreTool) []actool.CoreTool { return []actool.CoreTool{} }
	t.Cleanup(func() { ToolAugment, ToolResolve = oldAugment, oldResolve })
}

func TestChatWorkspaceQuotaRefusesNewSessionBeforeProvider(t *testing.T) {
	fixtureNoChatTools(t)
	dir := t.TempDir()
	if _, err := storage.Open(dir, storage.Workspace, storage.Limits{Bytes: 1024, Entries: 2}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"retained-a", "retained-b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("retained"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	provider := &historyFixtureProvider{reply: "must not run"}
	chat := NewChatAgent(provider, "fixture", dir, nil, 100000)
	chat.SetNonStreaming(func() bool { return true })
	if _, err := chat.Chat(t.Context(), "fixture", "new-session", "hello", 1, 0, false, nil); !errors.Is(err, storage.ErrQuota) {
		t.Fatalf("expected workspace quota refusal, got %v", err)
	}
	if len(provider.requests) != 0 {
		t.Fatal("provider was invoked after workspace admission failed")
	}
	if _, err := os.Lstat(filepath.Join(dir, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("failed admission created a session directory: %v", err)
	}
}

func TestNoRawChatReplaysVisibleRolesAfterAgentRestart(t *testing.T) {
	fixtureNoChatTools(t)
	for _, nonStreaming := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete", false: "stream"}[nonStreaming], func(t *testing.T) {
			dir := t.TempDir()
			first := &historyFixtureProvider{reply: "I will remember orchid17"}
			chat := NewChatAgent(first, "fixture", dir, nil, 100000)
			chat.SetNonStreaming(func() bool { return nonStreaming })
			var activities []db.Activity
			if _, err := chat.Chat(t.Context(), "fixture", "conv-1", "My codeword is orchid17", 1, 0, false, func(a db.Activity) { activities = append(activities, a) }); err != nil {
				t.Fatal(err)
			}
			history := []llm.Message{llm.UserText("My codeword is orchid17"), {Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("I will remember orchid17")}}}
			second := &historyFixtureProvider{reply: "orchid17"}
			restarted := NewChatAgent(second, "fixture", dir, nil, 100000)
			restarted.SetNonStreaming(func() bool { return nonStreaming })
			ctx := WithChatHistory(t.Context(), history)
			if _, err := restarted.Chat(ctx, "fixture", "conv-1", "What was my codeword?", 1, 0, false, nil); err != nil {
				t.Fatal(err)
			}
			if len(second.requests) != 1 {
				t.Fatalf("requests=%d", len(second.requests))
			}
			messages := second.requests[0].Messages
			if len(messages) < 3 || messages[0].Role != llm.RoleUser || messages[1].Role != llm.RoleAssistant || messages[len(messages)-1].Role != llm.RoleUser || !strings.Contains(messages[0].Text(), "orchid17") || messages[len(messages)-1].Text() != "What was my codeword?" {
				t.Fatalf("prior roles/currentturn were not preserved: %+v", messages)
			}
			if err := filepath.WalkDir(dir, func(name string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() && (strings.HasSuffix(name, ".jsonl") || strings.Contains(name, "transcript")) {
					t.Errorf("raw transcript created: %s", filepath.Base(name))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestChatHistoryIsBoundedTextOnlyAndNeverPromotesSystemOrTools(t *testing.T) {
	input := []llm.Message{{Role: llm.Role("system"), Content: []llm.ContentBlock{llm.TextBlock("must not promote")}}, llm.UserText(strings.Repeat("u", 100000)), {Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse, Name: "Bash", ID: "old"}, llm.TextBlock(strings.Repeat("a", 100000))}}}
	got := boundedChatHistory(input, 1024)
	used := 0
	for _, message := range got {
		if message.Role != llm.RoleUser && message.Role != llm.RoleAssistant {
			t.Fatal("role promoted")
		}
		for _, block := range message.Content {
			if block.Type != llm.BlockText {
				t.Fatal("tool state replayed")
			}
		}
		used += len(message.Text()) + 16
	}
	if len(got) != 2 || used > 1024 {
		t.Fatal("history budget not enforced", len(got), used)
	}
}
func TestChatHistoryProjectionDoesNotDuplicateOrContaminateCompaction(t *testing.T) {
	history := []llm.Message{llm.UserText("earlier user"), {Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("earlier answer")}}}
	p := &chatHistoryProvider{history: history, system: []string{"current system"}, window: 10000, budget: 2000}
	original := llm.CompletionRequest{System: []string{"current system"}, Messages: []llm.Message{llm.UserText("current")}}
	for range 2 {
		got, err := p.project(original)
		if err != nil || len(got.Messages) != 3 || len(original.Messages) != 1 {
			t.Fatal("projection mutated or duplicated history", err, got)
		}
	}
	got, err := p.project(llm.CompletionRequest{System: []string{"independent summarizer"}, Messages: []llm.Message{llm.UserText("summarize only this")}})
	if err != nil || len(got.Messages) != 1 {
		t.Fatal("compaction request was contaminated")
	}
	p.window = 10
	if _, err = p.project(original); err == nil {
		t.Fatal("history silently lost instead of refusing context overflow")
	}
}
