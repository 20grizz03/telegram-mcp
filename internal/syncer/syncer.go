// Package syncer orchestrates incremental caching of Telegram messages into the
// local store. Shared by the MCP `sync_chat` tool and the `tgmcp sync` CLI.
package syncer

import (
	"context"

	"github.com/20grizz03/telegram-mcp/internal/store"
	"github.com/20grizz03/telegram-mcp/internal/tgclient"
)

// Result summarizes a sync run.
type Result struct {
	Chat        tgclient.Chat `json:"chat"`
	Fetched     int           `json:"fetched"`      // new messages pulled this run
	TotalCached int           `json:"total_cached"` // total messages cached for this chat
	LastMsgID   int           `json:"last_msg_id"`
}

// Sync pulls messages newer than the last sync for chat/topic into the store
// (and FTS index) and advances the sync cursor.
func Sync(ctx context.Context, tc *tgclient.Client, st *store.Store, chatID int64, topicID, max int) (Result, error) {
	last, err := st.GetSyncState(ctx, chatID, topicID)
	if err != nil {
		return Result{}, err
	}

	chat, msgs, err := tc.FetchSince(ctx, chatID, topicID, last, max)
	if err != nil {
		return Result{}, err
	}

	rows := make([]store.Msg, 0, len(msgs))
	maxID := last
	for _, m := range msgs {
		rows = append(rows, store.Msg{
			ChatID:   chatID,
			MsgID:    m.ID,
			TopicID:  topicID,
			Date:     m.DateUnix,
			SenderID: m.Author.ID,
			Sender:   m.Author.Name,
			Text:     m.Text,
			ReplyTo:  m.ReplyTo,
		})
		if m.ID > maxID {
			maxID = m.ID
		}
	}

	if err := st.UpsertChat(ctx, store.ChatMeta{
		ChatID: chat.ID, Title: chat.Title, Kind: string(chat.Kind),
		Username: chat.Username, IsForum: chat.IsForum,
	}); err != nil {
		return Result{}, err
	}
	if err := st.UpsertMessages(ctx, rows); err != nil {
		return Result{}, err
	}
	if maxID > last {
		if err := st.SetSyncState(ctx, chatID, topicID, maxID); err != nil {
			return Result{}, err
		}
	}

	total, _ := st.CountMessages(ctx, chatID)
	return Result{Chat: chat, Fetched: len(msgs), TotalCached: total, LastMsgID: maxID}, nil
}
