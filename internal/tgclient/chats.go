package tgclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
)

const (
	dialogPageSize = 100
	maxDialogPages = 10 // bound the worst case at ~1000 dialogs
)

// ListChats refreshes the dialog list and returns it, optionally filtered by a
// case-insensitive substring over title/username and capped at limit (<=0 means
// no cap).
func (c *Client) ListChats(ctx context.Context, query string, limit int) ([]Chat, error) {
	if err := c.refreshDialogs(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	all := append([]Chat(nil), c.lastChats...)
	c.mu.Unlock()

	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]Chat, 0, len(all))
	for _, ch := range all {
		if query != "" &&
			!strings.Contains(strings.ToLower(ch.Title), query) &&
			!strings.Contains(strings.ToLower(ch.Username), query) {
			continue
		}
		out = append(out, ch)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// resolvePeer returns the cached peer for id, refreshing dialogs once on a miss.
func (c *Client) resolvePeer(ctx context.Context, id int64) (peerInfo, error) {
	c.mu.Lock()
	p, ok := c.peers[id]
	c.mu.Unlock()
	if ok {
		return p, nil
	}

	if err := c.refreshDialogs(ctx); err != nil {
		return peerInfo{}, err
	}

	c.mu.Lock()
	p, ok = c.peers[id]
	c.mu.Unlock()
	if !ok {
		return peerInfo{}, fmt.Errorf("chat %d not found; call list_chats first", id)
	}
	return p, nil
}

// refreshDialogs pulls (a bounded prefix of) the dialog list and rebuilds the
// in-memory peer registry and ordered chat snapshot.
func (c *Client) refreshDialogs(ctx context.Context) error {
	peers := make(map[int64]peerInfo)
	var ordered []Chat

	var (
		offsetDate int
		offsetID   int
		offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}
	)

	for page := 0; page < maxDialogPages; page++ {
		resp, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			OffsetDate: offsetDate,
			OffsetID:   offsetID,
			OffsetPeer: offsetPeer,
			Limit:      dialogPageSize,
		})
		if err != nil {
			return fmt.Errorf("get dialogs: %w", err)
		}

		var (
			dialogs  []tg.DialogClass
			messages []tg.MessageClass
			chats    []tg.ChatClass
			users    []tg.UserClass
			complete bool
		)
		switch d := resp.(type) {
		case *tg.MessagesDialogs:
			dialogs, messages, chats, users, complete = d.Dialogs, d.Messages, d.Chats, d.Users, true
		case *tg.MessagesDialogsSlice:
			dialogs, messages, chats, users = d.Dialogs, d.Messages, d.Chats, d.Users
			complete = len(dialogs) < dialogPageSize
		case *tg.MessagesDialogsNotModified:
			complete = true
		default:
			return fmt.Errorf("unexpected dialogs type %T", resp)
		}

		idx := indexEntities(chats, users)
		for _, dc := range dialogs {
			d, ok := dc.(*tg.Dialog)
			if !ok {
				continue
			}
			info, id, ok := idx.peerInfoFor(d.Peer)
			if !ok {
				continue
			}
			peers[id] = info
			ordered = append(ordered, Chat{
				ID:       id,
				Title:    info.title,
				Kind:     info.kind,
				Username: info.username,
				IsForum:  info.isForum,
				Unread:   d.UnreadCount,
			})
		}

		if complete || len(dialogs) == 0 {
			break
		}

		// Advance the offset using the last dialog of this page.
		last, ok := dialogs[len(dialogs)-1].(*tg.Dialog)
		if !ok {
			break
		}
		offsetID = last.TopMessage
		offsetDate = msgDate(messages, last.TopMessage)
		if p, _, ok := idx.peerInfoFor(last.Peer); ok {
			offsetPeer = p.input
		} else {
			break
		}
	}

	c.mu.Lock()
	c.peers = peers
	c.lastChats = ordered
	c.mu.Unlock()
	return nil
}

func msgDate(messages []tg.MessageClass, id int) int {
	for _, m := range messages {
		if m.GetID() == id {
			if msg, ok := m.(*tg.Message); ok {
				return msg.Date
			}
		}
	}
	return 0
}
