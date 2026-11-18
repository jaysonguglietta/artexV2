package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

type conversationBeforeKey struct{}

func withConversationBefore(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, conversationBeforeKey{}, id)
}

// Bounded SQL projection: do not fetch full raw activity blobs and truncate only
// after allocation. Replay human messages and final assistant replies, not tool
// preambles/results/thinking. Long tool-heavy turns cannot displace every user
// row from the bounded page with intermediate assistant activities.
func loadConversationHistory(ctx context.Context, pg *db.DB, id, before int64, budget int) ([]llm.Message, error) {
	if pg == nil || pg.DB == nil {
		return nil, errors.New("conversation database is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if before <= 0 {
		// Legacy callers (the initial retest) already persisted their current user
		// activity. Explicit HTTP/scheduler callers carry that exact row ID instead.
		var latest int64
		var kind string
		err := pg.QueryRowContext(ctx, `SELECT id,COALESCE(kind,'') FROM conversation_activities WHERE conversation_id=$1 ORDER BY id DESC LIMIT 1`, id).Scan(&latest, &kind)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == sql.ErrNoRows {
			return nil, nil
		}
		before = latest + 1
		if kind == "user" {
			before = latest
		}
	}
	chars := max(1, min(4096, budget/4))
	rows, err := pg.QueryContext(ctx, `SELECT kind,left(COALESCE(NULLIF(detail,''),summary,''),$3),left(COALESCE(summary,''),800)
FROM conversation_activities WHERE conversation_id=$1 AND id<$2
AND kind IN ('user','result') AND NOT is_error
ORDER BY id DESC LIMIT $4`, id, before, chars, agent.ChatHistoryMaxRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reverse := make([]llm.Message, 0, agent.ChatHistoryMaxRows)
	for rows.Next() {
		var kind, text, summary string
		if err = rows.Scan(&kind, &text, &summary); err != nil {
			return nil, err
		}
		role := llm.RoleAssistant
		if kind == "user" {
			role = llm.RoleUser
			text = conversationVisibleUserText(text, summary)
		}
		if strings.TrimSpace(text) != "" {
			reverse = append(reverse, llm.Message{Role: role, Content: []llm.ContentBlock{llm.TextBlock(text)}})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]llm.Message, len(reverse))
	for i, message := range reverse {
		out[len(reverse)-1-i] = message
	}
	return out, nil
}

// Only unwrap the application's attachment representation when its original
// visible summary agrees. A user pasting literal JSON must retain that JSON.
func conversationVisibleUserText(detail, summary string) string {
	if strings.HasPrefix(strings.TrimSpace(detail), "{") {
		var attachment struct {
			Text        *string         `json:"text"`
			Attachments json.RawMessage `json:"attachments"`
		}
		if json.Unmarshal([]byte(detail), &attachment) == nil && attachment.Text != nil && len(attachment.Attachments) > 0 && attachment.Attachments[0] == '[' && summary == firstLine(*attachment.Text, 200) {
			return *attachment.Text
		}
	}
	return detail
}
