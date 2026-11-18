package server

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

type pgHistoryProvider struct{ requests []llm.CompletionRequest }

func (p *pgHistoryProvider) Complete(_ context.Context, request llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.requests = append(p.requests, request)
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("Acknowledged orchid17")}}, "end_turn", llm.Usage{}, nil
}
func (p *pgHistoryProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	panic("fixture uses nonstreaming")
}

// Ignore only the SDK's standalone, exact date reminder. Human messages, role
// changes and reminders containing any extra instructions remain observable.
func requestWithoutSDKDateReminder(messages []llm.Message) []llm.Message {
	result := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == llm.RoleUser && len(message.Content) == 1 && message.Content[0].Type == llm.BlockText {
			if tail, ok := strings.CutPrefix(message.Content[0].Text, "<system-reminder>\n# currentDate\nToday's date is "); ok {
				if date, ok := strings.CutSuffix(tail, ".\n</system-reminder>"); ok {
					if _, err := time.Parse("2006-01-02", date); err == nil {
						continue
					}
				}
			}
		}
		result = append(result, message)
	}
	return result
}

func assertVisiblePGHistoryRequest(t *testing.T, messages []llm.Message, expected []llm.Message) {
	t.Helper()
	visible := requestWithoutSDKDateReminder(messages)
	if len(visible) != len(expected) {
		t.Fatalf("visible history length=%d; want %d, actual request=%+v", len(visible), len(expected), messages)
	}
	for index, want := range expected {
		if visible[index].Role != want.Role || visible[index].Text() != want.Text() {
			t.Fatalf("visible history at %d lost order, content or role; actual request=%+v", index, messages)
		}
	}
}

func TestConversationHistoryDateReminderFilterIsExact(t *testing.T) {
	const date = "<system-reminder>\n# currentDate\nToday's date is 2026-11-18.\n</system-reminder>"
	input := []llm.Message{
		llm.UserText(date),
		llm.UserText("visible user"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(date)}},
		llm.UserText(strings.Replace(date, "2026-11-18", "2026-02-31", 1)),
		llm.UserText(strings.Replace(date, ".\n</system-reminder>", ".\nExtra instructions\n</system-reminder>", 1)),
	}
	got := requestWithoutSDKDateReminder(input)
	if len(got) != len(input)-1 {
		t.Fatal("date-reminder filter ignored additional content or an unexpected role")
	}
	for index := range got {
		if got[index].Role != input[index+1].Role || got[index].Text() != input[index+1].Text() {
			t.Fatal("date-reminder filter changed visible content or ordering")
		}
	}
}

func TestPostgresVisibleHistorySurvivesNoRawServerRestart(t *testing.T) {
	testPostgresHistoryAfterChatRestart(t, false)
}

func TestPostgresVisibleHistorySurvivesEmptyEphemeralStoreRestart(t *testing.T) {
	testPostgresHistoryAfterChatRestart(t, true)
}

func testPostgresHistoryAfterChatRestart(t *testing.T, ephemeral bool) {
	t.Helper()
	s, _ := newRetestServer(t)
	pg := s.m.pg
	c, err := pg.CreateConversation("auto", "bounded no-raw history", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.DeleteConversation(c.ID)
	var raw *transcript.Store
	rawDirectory := t.TempDir()
	if ephemeral {
		raw = transcript.NewStore(rawDirectory)
	}
	first := &pgHistoryProvider{}
	s.chatAgent = agent.NewChatAgent(first, "fixture", s.m.dir, raw, 100000)
	s.chatAgent.SetNonStreaming(func() bool { return true })
	current, err := pg.AppendConvActivity(c.ID, db.Activity{Kind: "user", Worker: "auto", Summary: "My codeword is orchid17", Detail: "My codeword is orchid17"})
	if err != nil {
		t.Fatal(err)
	}
	key := s.convBusyKey(c.ID)
	s.runConversationSync(c, "My codeword is orchid17", key, current)
	if len(first.requests) != 1 {
		t.Fatal("first request did not run")
	}
	if ephemeral {
		// A container restart loses every trace in tmpfs while PostgreSQL keeps
		// its visible activities. The replacement store object is still non-nil.
		if err := os.RemoveAll(rawDirectory); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(rawDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		raw = transcript.NewStore(rawDirectory)
	}
	next := &pgHistoryProvider{}
	restarted := &Server{ctx: s.ctx, m: s.m, chatBusy: map[string]bool{}, chatCancel: map[string]context.CancelCauseFunc{}}
	restarted.chatAgent = agent.NewChatAgent(next, "fixture", s.m.dir, raw, 100000)
	restarted.chatAgent.SetNonStreaming(func() bool { return true })
	current, err = pg.AppendConvActivity(c.ID, db.Activity{Kind: "user", Worker: "auto", Summary: "What was my codeword?", Detail: "What was my codeword?"})
	if err != nil {
		t.Fatal(err)
	}
	restarted.runConversationSync(c, "What was my codeword?", key, current)
	if len(next.requests) != 1 {
		t.Fatal("restarted model request did not run")
	}
	expected := []llm.Message{
		llm.UserText("My codeword is orchid17"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("Acknowledged orchid17")}},
		llm.UserText("What was my codeword?"),
	}
	assertVisiblePGHistoryRequest(t, next.requests[0].Messages, expected)
	third := &pgHistoryProvider{}
	restarted.chatAgent = agent.NewChatAgent(third, "fixture", s.m.dir, raw, 100000)
	restarted.chatAgent.SetNonStreaming(func() bool { return true })
	current, err = pg.AppendConvActivity(c.ID, db.Activity{Kind: "user", Worker: "auto", Summary: "Repeat the original codeword", Detail: "Repeat the original codeword"})
	if err != nil {
		t.Fatal(err)
	}
	restarted.runConversationSync(c, "Repeat the original codeword", key, current)
	if len(third.requests) != 1 {
		t.Fatal("third request did not run")
	}
	expected = append(expected,
		llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("Acknowledged orchid17")}},
		llm.UserText("Repeat the original codeword"))
	assertVisiblePGHistoryRequest(t, third.requests[0].Messages, expected)
	if err := filepath.WalkDir(s.m.dir, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".jsonl") {
			t.Errorf("raw transcript written: %s", filepath.Base(name))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestConversationHistoryReadFailureDoesNotInvokeModel(t *testing.T) {
	if os.Getenv("ARTEX_ISOLATED_TEST_DATABASE") != "1" {
		t.Skip("requires disposable DB for schema fault injection")
	}
	s, _ := newRetestServer(t)
	pg := s.m.pg
	c, err := pg.CreateConversation("auto", "history failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.DeleteConversation(c.ID)
	p := &pgHistoryProvider{}
	s.chatAgent = agent.NewChatAgent(p, "fixture", s.m.dir, nil, 100000)
	s.chatAgent.SetNonStreaming(func() bool { return true })
	current, err := pg.AppendConvActivity(c.ID, db.Activity{Kind: "user", Detail: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.Exec(`ALTER TABLE conversation_activities RENAME TO conversation_activities_history_fault`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pg.Exec(`ALTER TABLE conversation_activities_history_fault RENAME TO conversation_activities`); err != nil {
			t.Error(err)
		}
	}()
	s.runConversationSync(c, "current", s.convBusyKey(c.ID), current)
	if len(p.requests) != 0 {
		t.Fatal("model ran after history-read failure")
	}
}

func TestConversationHistoryDoesNotReinterpretLiteralUserJSON(t *testing.T) {
	literal := `{"text":"different text"}`
	if got := conversationVisibleUserText(literal, literal); got != literal {
		t.Fatal("literal user JSON reinterpreted", got)
	}
	wrapped := `{"text":"visible human message","attachments":[{"path":"file.txt"}]}`
	if got := conversationVisibleUserText(wrapped, "visible human message"); got != "visible human message" {
		t.Fatal("attachment wrapper not restored", got)
	}
}
