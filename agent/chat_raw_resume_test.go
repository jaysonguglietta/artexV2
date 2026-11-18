package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

func chatResumeFixtureHistory() []llm.Message {
	return []llm.Message{llm.UserText("My retained codeword is orchid17"), {
		Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("I will remember orchid17")},
	}}
}

func countChatFixtureText(messages []llm.Message, text string) int {
	count := 0
	for _, message := range messages {
		if message.Text() == text {
			count++
		}
	}
	return count
}

func TestEmptyRawChatSeedsPostgresHistoryAcrossMultipleResumes(t *testing.T) {
	fixtureNoChatTools(t)
	for _, nonStreaming := range []bool{true, false} {
		for _, existingEmptyFile := range []bool{false, true} {
			name := map[bool]string{true: "complete", false: "stream"}[nonStreaming] + map[bool]string{false: "-missing", true: "-empty"}[existingEmptyFile]
			t.Run(name, func(t *testing.T) {
				directory := t.TempDir()
				store := transcript.NewStore(t.TempDir()) // New tmpfs generation after restart.
				const session = "conv-restored"
				if existingEmptyFile {
					if err := os.WriteFile(store.MainPath(session), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				history := chatResumeFixtureHistory()
				for turn, current := range []string{"What was my retained codeword?", "Repeat it one more time"} {
					provider := &historyFixtureProvider{reply: "orchid17"}
					// Recreate the runner each turn to exercise real SDK Resume.
					chat := NewChatAgent(provider, "fixture", directory, store, 100000)
					chat.SetNonStreaming(func() bool { return nonStreaming })
					if _, err := chat.Chat(WithChatHistory(t.Context(), history), "fixture", session, current, 1, 0, false, nil); err != nil {
						t.Fatal(err)
					}
					if len(provider.requests) != 1 {
						t.Fatalf("turn %d: model calls=%d", turn, len(provider.requests))
					}
					messages := provider.requests[0].Messages
					for _, prior := range history {
						// The repeated short answer occurs once per completed turn.
						if prior.Role == llm.RoleUser && countChatFixtureText(messages, prior.Text()) != 1 {
							t.Fatalf("turn %d: retained user context lost or duplicated: %+v", turn, messages)
						}
					}
					if countChatFixtureText(messages, "I will remember orchid17") != 1 || countChatFixtureText(messages, current) != 1 {
						t.Fatalf("turn %d: retained answer/current turn lost or duplicated: %+v", turn, messages)
					}
					history = append(history, llm.UserText(current), llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("orchid17")}})
				}
				records, err := store.Load(store.MainPath(session))
				if err != nil {
					t.Fatal(err)
				}
				for index := 1; index < len(records); index++ {
					if records[index].ParentUUID != records[index-1].UUID {
						t.Fatal("seed/resume did not preserve SDK transcript parent chain")
					}
				}
				entries, err := os.ReadDir(filepath.Dir(store.MainPath(session)))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".history-seed-") {
						t.Fatal("private seed staging survived completion")
					}
				}
			})
		}
	}
}

func TestValidRawChatResumeDoesNotPrependPostgresSeed(t *testing.T) {
	fixtureNoChatTools(t)
	store := transcript.NewStore(t.TempDir())
	writer := store.NewWriter("conv-valid", "")
	writer.RecordMessage(llm.UserText("raw prior user"), llm.Usage{})
	writer.RecordMessage(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("raw prior answer")}}, llm.Usage{})
	if err := writer.Err(); err != nil {
		t.Fatal(err)
	}
	provider := &historyFixtureProvider{reply: "complete"}
	chat := NewChatAgent(provider, "fixture", t.TempDir(), store, 100000)
	chat.SetNonStreaming(func() bool { return true })
	if _, err := chat.Chat(WithChatHistory(t.Context(), chatResumeFixtureHistory()), "fixture", "conv-valid", "current", 1, 0, false, nil); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatal("raw resumed request did not run")
	}
	messages := provider.requests[0].Messages
	if countChatFixtureText(messages, "raw prior user") != 1 || countChatFixtureText(messages, "raw prior answer") != 1 || countChatFixtureText(messages, "My retained codeword is orchid17") != 0 {
		t.Fatalf("raw resume was replaced or received a second PG prefix: %+v", messages)
	}
}

func TestDamagedRawChatTraceFailsBeforeModelWithoutPartialResume(t *testing.T) {
	fixtureNoChatTools(t)
	for _, kind := range []string{"corrupt-after-valid-record", "empty-message-record", "oversized", "unreadable", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			store := transcript.NewStore(t.TempDir())
			const session = "conv-damaged"
			path := store.MainPath(session)
			switch kind {
			case "empty-message-record":
				if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses ordinary file read permissions")
				}
				if err := os.WriteFile(path, []byte("{}\n"), 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
			case "corrupt-after-valid-record":
				writer := store.NewWriter(session, "")
				writer.RecordMessage(llm.UserText("partial history must not be used"), llm.Usage{})
				if err := writer.Err(); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("{invalid JSON\n"); err != nil {
					t.Fatal(err)
				}
				file.Close()
			case "oversized":
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(maxChatRawResumeBytes + 1); err != nil {
					t.Fatal(err)
				}
				file.Close()
			case "symlink":
				canary := filepath.Join(t.TempDir(), "retained")
				if err := os.WriteFile(canary, []byte("retained"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(canary, path); err != nil {
					t.Skip(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			provider := &historyFixtureProvider{reply: "must not run"}
			chat := NewChatAgent(provider, "fixture", t.TempDir(), store, 100000)
			chat.SetNonStreaming(func() bool { return true })
			if _, err := chat.Chat(WithChatHistory(t.Context(), chatResumeFixtureHistory()), "fixture", session, "current", 1, 0, false, nil); err == nil || !strings.Contains(err.Error(), "raw conversation") {
				t.Fatalf("expected actionable raw trace refusal, got %v", err)
			}
			if len(provider.requests) != 0 {
				t.Fatal("model received partial/corrupt/oversized raw history")
			}
		})
	}
}
