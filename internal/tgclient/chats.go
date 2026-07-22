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
	if ok && !p.min {
		return p, nil
	}
	if ok && p.username != "" {
		return c.resolveUsernamePeer(ctx, p.username, id)
	}

	if err := c.refreshDialogs(ctx); err != nil {
		return peerInfo{}, err
	}

	c.mu.Lock()
	p, ok = c.peers[id]
	c.mu.Unlock()
	if !ok || p.min {
		return peerInfo{}, fmt.Errorf("chat %d not found; call list_chats first", id)
	}
	return p, nil
}

// resolveUsernamePeer upgrades a public peer returned as a reduced Telegram
// min constructor into a full peer whose access hash can be used by read APIs.
func (c *Client) resolveUsernamePeer(ctx context.Context, username string, wantID int64) (peerInfo, error) {
	resp, err := c.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: strings.TrimPrefix(username, "@"),
	})
	if err != nil {
		return peerInfo{}, fmt.Errorf("resolve @%s: %w", strings.TrimPrefix(username, "@"), err)
	}

	idx := indexEntities(resp.Chats, resp.Users)
	info, id, ok := idx.peerInfoFor(resp.Peer)
	if !ok || id != wantID || info.min {
		return peerInfo{}, fmt.Errorf("resolve @%s: incomplete peer", strings.TrimPrefix(username, "@"))
	}

	c.mergePeers(map[int64]peerInfo{id: info})
	return info, nil
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

	c.mergePeers(peers)
	c.mu.Lock()
	c.lastChats = ordered
	c.mu.Unlock()
	return nil
}

// mergePeers preserves a full access hash when a later search response only
// contains a lower-priority Telegram min constructor.
func (c *Client) mergePeers(peers map[int64]peerInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peers == nil {
		c.peers = make(map[int64]peerInfo)
	}
	for id, info := range peers {
		if current, ok := c.peers[id]; ok && !current.min && info.min {
			continue
		}
		c.peers[id] = info
	}
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
