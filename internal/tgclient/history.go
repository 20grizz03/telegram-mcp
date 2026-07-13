package tgclient

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gotd/td/tg"
)

const historyPageSize = 100

// ListTopics returns the forum topics of a supergroup. Errors if the chat is
// not a forum.
func (c *Client) ListTopics(ctx context.Context, chatID int64) ([]Topic, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if info.kind != KindChannel || !info.isForum {
		return nil, fmt.Errorf("chat %d is not a forum (no topics)", chatID)
	}

	resp, err := c.api.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{
		Peer:  info.input,
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("get forum topics: %w", err)
	}

	out := make([]Topic, 0, len(resp.Topics))
	for _, tc := range resp.Topics {
		if t, ok := tc.(*tg.ForumTopic); ok {
			out = append(out, Topic{ID: t.ID, Title: t.Title, TopMessageID: t.TopMessage})
		}
	}
	return out, nil
}

// ListPinnedMessages returns pinned messages from a chat, newest first. Pinned
// messages commonly contain community rules that should be checked before any
// external distribution or self-promotion.
func (c *Client) ListPinnedMessages(ctx context.Context, chatID int64, limit int) (Chat, []Message, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Chat{}, nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	resp, err := c.api.MessagesSearch(ctx, &tg.MessagesSearchRequest{
		Peer:   info.input,
		Q:      "",
		Filter: &tg.InputMessagesFilterPinned{},
		Limit:  limit,
	})
	if err != nil {
		return Chat{}, nil, fmt.Errorf("list pinned messages: %w", err)
	}

	msgs, chats, users := extractMessages(resp)
	idx := indexEntities(chats, users)
	out := make([]Message, 0, len(msgs))
	for _, mc := range msgs {
		m, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		out = append(out, c.mapMessage(m, idx, info, 0))
	}

	chat := Chat{ID: chatID, Title: info.title, Kind: info.kind, Username: info.username, IsForum: info.isForum}
	return chat, out, nil
}

// ListScheduledPosts returns pending scheduled posts, earliest first. Reading
// the queue lets editorial automation verify a publish without risking a
// duplicate when Telegram's write response is ambiguous.
func (c *Client) ListScheduledPosts(ctx context.Context, chatID int64) (Chat, []Message, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Chat{}, nil, err
	}

	resp, err := c.api.MessagesGetScheduledHistory(ctx, &tg.MessagesGetScheduledHistoryRequest{
		Peer: info.input,
	})
	if err != nil {
		return Chat{}, nil, fmt.Errorf("list scheduled posts: %w", err)
	}

	msgs, chats, users := extractMessages(resp)
	idx := indexEntities(chats, users)
	out := make([]Message, 0, len(msgs))
	for _, mc := range msgs {
		m, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		out = append(out, c.mapMessage(m, idx, info, 0))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DateUnix < out[j].DateUnix })

	chat := Chat{ID: chatID, Title: info.title, Kind: info.kind, Username: info.username, IsForum: info.isForum}
	return chat, out, nil
}

// ReadChat returns messages of a chat (optionally a single forum topic) within
// the [from, to] window, oldest first, capped at limit.
func (c *Client) ReadChat(ctx context.Context, chatID int64, topicID int, from, to string, limit int) (Chat, []Message, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Chat{}, nil, err
	}
	r, err := ParseRange(from, to, c.loc, time.Now())
	if err != nil {
		return Chat{}, nil, err
	}
	if limit <= 0 {
		limit = 500
	}

	minUnix := int(r.Min.Unix())
	maxUnix := int(r.Max.Unix())

	var (
		collected []Message
		offsetID  = 0
		offsetDt  = maxUnix + 1 // include messages exactly at the upper bound
	)

pages:
	for {
		resp, err := c.fetchPage(ctx, info.input, topicID, offsetID, offsetDt)
		if err != nil {
			return Chat{}, nil, err
		}
		msgs, chats, users := extractMessages(resp)
		if len(msgs) == 0 {
			break
		}
		idx := indexEntities(chats, users)

		var lastID int
		for _, mc := range msgs { // newest -> oldest
			lastID = mc.GetID()
			m, ok := mc.(*tg.Message)
			if !ok {
				continue // skip service messages
			}
			if m.Date > maxUnix {
				continue
			}
			if m.Date < minUnix {
				break pages // walked past the window
			}
			collected = append(collected, c.mapMessage(m, idx, info, topicID))
			if len(collected) >= limit {
				break pages
			}
		}

		if lastID == 0 || lastID == offsetID {
			break // no progress, avoid an infinite loop
		}
		offsetID = lastID
		offsetDt = 0 // subsequent pages paginate purely by id
	}

	// Return chronological order (oldest first).
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}

	chat := Chat{ID: chatID, Title: info.title, Kind: info.kind, Username: info.username, IsForum: info.isForum}
	return chat, collected, nil
}

// FetchSince returns messages newer than sinceMsgID (newest first on the wire,
// returned oldest first), capped at max. With sinceMsgID=0 it grabs the newest
// `max` messages — used for the first incremental sync of a chat/topic.
func (c *Client) FetchSince(ctx context.Context, chatID int64, topicID, sinceMsgID, max int) (Chat, []Message, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Chat{}, nil, err
	}
	if max <= 0 {
		max = 1000
	}

	var (
		collected []Message
		offsetID  = 0
	)

pages:
	for {
		resp, err := c.fetchPage(ctx, info.input, topicID, offsetID, 0)
		if err != nil {
			return Chat{}, nil, err
		}
		msgs, chats, users := extractMessages(resp)
		if len(msgs) == 0 {
			break
		}
		idx := indexEntities(chats, users)

		var lastID int
		for _, mc := range msgs { // newest -> oldest
			lastID = mc.GetID()
			m, ok := mc.(*tg.Message)
			if !ok {
				continue
			}
			if m.ID <= sinceMsgID {
				break pages // reached already-cached territory
			}
			collected = append(collected, c.mapMessage(m, idx, info, topicID))
			if len(collected) >= max {
				break pages
			}
		}
		if lastID == 0 || lastID == offsetID {
			break
		}
		offsetID = lastID
	}

	// chronological order (oldest first)
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}

	chat := Chat{ID: chatID, Title: info.title, Kind: info.kind, Username: info.username, IsForum: info.isForum}
	return chat, collected, nil
}

func (c *Client) fetchPage(ctx context.Context, peer tg.InputPeerClass, topicID, offsetID, offsetDate int) (tg.MessagesMessagesClass, error) {
	if topicID > 0 {
		return c.api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{
			Peer:       peer,
			MsgID:      topicID,
			OffsetID:   offsetID,
			OffsetDate: offsetDate,
			Limit:      historyPageSize,
		})
	}
	return c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:       peer,
		OffsetID:   offsetID,
		OffsetDate: offsetDate,
		Limit:      historyPageSize,
	})
}

func (c *Client) mapMessage(m *tg.Message, idx entityIndex, info peerInfo, topicID int) Message {
	t := time.Unix(int64(m.Date), 0).In(c.loc)

	media := mediaInfoFor(m.Media)
	text := m.Message
	if text == "" && media != nil {
		text = mediaLabel(media)
	}

	replyTo, _ := func() (int, bool) {
		if rh, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
			return rh.GetReplyToMsgID()
		}
		return 0, false
	}()

	views, _ := m.GetViews()
	forwards, _ := m.GetForwards()
	reactions := 0
	if rr, ok := m.GetReactions(); ok {
		for _, count := range rr.Results {
			reactions += count.Count
		}
	}

	return Message{
		ID:        m.ID,
		DateISO:   t.Format(time.RFC3339),
		TimeLocal: t.Format("15:04"),
		DateUnix:  int64(m.Date),
		Author:    idx.authorFor(m.FromID, info.title),
		Text:      text,
		ReplyTo:   replyTo,
		Media:     media,
		Views:     views,
		Forwards:  forwards,
		Reactions: reactions,
		Link: BuildLink(LinkArgs{
			Kind:      info.kind,
			ChannelID: info.channelID,
			Username:  info.username,
			MsgID:     m.ID,
			TopicID:   topicID,
		}),
	}
}

func extractMessages(resp tg.MessagesMessagesClass) ([]tg.MessageClass, []tg.ChatClass, []tg.UserClass) {
	switch m := resp.(type) {
	case *tg.MessagesMessages:
		return m.Messages, m.Chats, m.Users
	case *tg.MessagesMessagesSlice:
		return m.Messages, m.Chats, m.Users
	case *tg.MessagesChannelMessages:
		return m.Messages, m.Chats, m.Users
	default:
		return nil, nil, nil
	}
}
