package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
)

const ChatHistoryMaxRows = 64
const ChatHistoryMaxBytes = 16 << 10

type chatHistoryKey struct{}

// WithChatHistory carries only trusted role attribution, never a new system
// prompt. Chat sanitizes blocks again before sending persisted visible text.
func WithChatHistory(ctx context.Context, messages []llm.Message) context.Context {
	return context.WithValue(ctx, chatHistoryKey{}, messages)
}
func (c *ChatAgent) HistoryReplayBudget() int {
	window := c.window
	if window <= 0 {
		window = defaultWindowK * 1000
	}
	return min(ChatHistoryMaxBytes, max(0, window/4))
}

func historyTextLimit(text string, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= limit {
		return text
	}
	const note = "\n[Earlier message truncated]"
	if limit <= len(note) {
		return ""
	}
	text = text[:limit-len(note)]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + note
}
func boundedChatHistory(input []llm.Message, budget int) []llm.Message {
	if budget <= 64 {
		return nil
	}
	normalized := make([]llm.Message, 0, min(len(input), ChatHistoryMaxRows))
	start := max(0, len(input)-ChatHistoryMaxRows)
	for _, message := range input[start:] {
		if message.Role != llm.RoleUser && message.Role != llm.RoleAssistant {
			continue
		}
		var text strings.Builder
		for _, block := range message.Content {
			if block.Type == llm.BlockText {
				text.WriteString(block.Text)
			}
		}
		value := historyTextLimit(text.String(), min(4096, (budget-32)/2))
		if strings.TrimSpace(value) == "" {
			continue
		}
		if len(normalized) > 0 && message.Role == llm.RoleAssistant && normalized[len(normalized)-1].Role == message.Role && normalized[len(normalized)-1].Text() == value {
			continue
		}
		normalized = append(normalized, llm.Message{Role: message.Role, Content: []llm.ContentBlock{llm.TextBlock(value)}})
	}
	used, first := 0, len(normalized)
	for i := len(normalized) - 1; i >= 0; i-- {
		cost := len(normalized[i].Text()) + 16
		if cost > budget-used {
			break
		}
		used += cost
		first = i
	}
	normalized = normalized[first:]
	// A reverse DB page can begin in the middle of a turn. Do not invent a
	// preceding user or replay orphan tool/assistant history as system authority.
	for len(normalized) > 0 && normalized[0].Role != llm.RoleUser {
		normalized = normalized[1:]
	}
	return normalized
}

type chatHistoryProvider struct {
	llm.Provider
	history        []llm.Message
	system         []string
	window, budget int
}

func (p *chatHistoryProvider) project(request llm.CompletionRequest) (llm.CompletionRequest, error) {
	// The SDK also uses its provider for compaction. Those independent summarizer
	// prompts must not receive a second copy of conversation history.
	if !slices.Equal(request.System, p.system) {
		return request, nil
	}
	estimate := compaction.EstimateTokens(request.Messages)
	for _, segment := range request.System {
		estimate += (len(segment) + 3) / 4
	}
	if raw, err := json.Marshal(request.Tools); err == nil {
		estimate += (len(raw) + 3) / 4
	}
	reserved := max(request.MaxTokens, p.window/4)
	available := min(p.budget, max(0, p.window-estimate-reserved))
	history := boundedChatHistory(p.history, available)
	if len(history) == 0 && len(p.history) > 0 {
		return request, errors.New("Cannot restore prior conversation within the configured model context budget; shorten the message/tools or increase the context window")
	}
	messages := make([]llm.Message, 0, len(history)+len(request.Messages))
	messages = append(messages, history...)
	messages = append(messages, request.Messages...)
	request.Messages = messages
	return request, nil
}
func (p *chatHistoryProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	request, err := p.project(request)
	if err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	return p.Provider.Complete(ctx, request)
}
func (p *chatHistoryProvider) Stream(ctx context.Context, request llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		request, err := p.project(request)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		for event, err := range p.Provider.Stream(ctx, request) {
			if !yield(event, err) {
				return
			}
		}
	}
}
