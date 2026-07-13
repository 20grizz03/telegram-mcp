package tgclient

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/markdown"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/tg"
)

// md renders markdown text into a styled-text option. Inline user-id mentions
// are not resolved (nil resolver) — not needed for channel posts.
func md(text string) styling.StyledTextOption {
	return markdown.String(nil, text)
}

// PublishPost posts a new message (optionally a photo with caption, optionally
// scheduled) to a chat/channel. text is markdown. Exactly one of photoPath /
// photoURL may be set; both empty means a plain text post (text required).
// scheduledAt zero means "send now". Returns the posted Message with a t.me
// link, or — for a scheduled post — a Message whose ID is the scheduled id and
// Link is empty.
func (c *Client) PublishPost(ctx context.Context, chatID int64, text, photoPath, photoURL string, scheduledAt time.Time, silent bool) (Message, error) {
	if photoPath != "" && photoURL != "" {
		return Message{}, fmt.Errorf("provide either photo_path or photo_url, not both")
	}
	if photoPath == "" && photoURL == "" && text == "" {
		return Message{}, fmt.Errorf("text is required when no photo is given")
	}

	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Message{}, err
	}

	b := &c.sender.To(pi.input).Builder
	if silent {
		b = b.Silent()
	}
	if !scheduledAt.IsZero() {
		b = b.Schedule(scheduledAt)
	}

	var caption []styling.StyledTextOption
	if text != "" {
		caption = []styling.StyledTextOption{md(text)}
	}

	var (
		id   int
		uErr error
	)
	switch {
	case photoPath != "":
		f, err := c.uploader.FromPath(ctx, photoPath)
		if err != nil {
			return Message{}, fmt.Errorf("upload %q: %w", photoPath, err)
		}
		id, uErr = publishedMessageID(b.Media(ctx, message.UploadedPhoto(f, caption...)))
	case photoURL != "":
		id, uErr = publishedMessageID(b.Media(ctx, message.PhotoExternal(photoURL, caption...)))
	default:
		id, uErr = publishedMessageID(b.StyledText(ctx, md(text)))
	}
	if uErr != nil {
		return Message{}, fmt.Errorf("publish to %d: %w", chatID, uErr)
	}

	return c.builtMessage(chatID, id, text, pi, scheduledAt), nil
}

// publishedMessageID extracts both regular and scheduled message ids. gotd's
// unpack.MessageID intentionally recognizes only updateNewMessage and
// updateNewChannelMessage, while Telegram returns updateNewScheduledMessage
// after a successful scheduled publish.
func publishedMessageID(updates tg.UpdatesClass, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	if id, unpackErr := unpack.MessageID(updates, nil); unpackErr == nil {
		return id, nil
	} else if id, ok := scheduledMessageID(updates); ok {
		return id, nil
	} else {
		return 0, unpackErr
	}
}

func scheduledMessageID(updates tg.UpdatesClass) (int, bool) {
	var list []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.UpdateShort:
		list = []tg.UpdateClass{u.Update}
	case *tg.UpdatesCombined:
		list = u.Updates
	case *tg.Updates:
		list = u.Updates
	default:
		return 0, false
	}

	for _, update := range list {
		scheduled, ok := update.(*tg.UpdateNewScheduledMessage)
		if ok && scheduled.Message != nil {
			return scheduled.Message.GetID(), true
		}
	}
	return 0, false
}

// EditPost replaces the text of an already-posted message. text is markdown.
func (c *Client) EditPost(ctx context.Context, chatID int64, msgID int, text string) (Message, error) {
	if text == "" {
		return Message{}, fmt.Errorf("text is required")
	}
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Message{}, err
	}
	if _, err := c.sender.To(pi.input).Edit(msgID).StyledText(ctx, md(text)); err != nil {
		return Message{}, fmt.Errorf("edit %d in %d: %w", msgID, chatID, err)
	}
	return c.builtMessage(chatID, msgID, text, pi, time.Time{}), nil
}

// PinPost pins (or, with unpin=true, unpins) a message. silent pins without a
// notification to subscribers.
func (c *Client) PinPost(ctx context.Context, chatID int64, msgID int, silent, unpin bool) error {
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return err
	}
	_, err = c.api.MessagesUpdatePinnedMessage(ctx, &tg.MessagesUpdatePinnedMessageRequest{
		Peer:   pi.input,
		ID:     msgID,
		Silent: silent,
		Unpin:  unpin,
	})
	if err != nil {
		return fmt.Errorf("pin %d in %d: %w", msgID, chatID, err)
	}
	return nil
}

// ForwardPost natively forwards one existing Telegram message to another chat,
// preserving the original source attribution. It performs a real write in the
// destination chat and should only be exposed behind the global write flag.
func (c *Client) ForwardPost(ctx context.Context, fromChatID, toChatID int64, msgID int, silent bool) (Message, error) {
	if msgID <= 0 {
		return Message{}, fmt.Errorf("msg_id must be positive")
	}
	if fromChatID == toChatID {
		return Message{}, fmt.Errorf("source and destination chats must differ")
	}
	from, err := c.resolvePeer(ctx, fromChatID)
	if err != nil {
		return Message{}, fmt.Errorf("resolve source chat: %w", err)
	}
	to, err := c.resolvePeer(ctx, toChatID)
	if err != nil {
		return Message{}, fmt.Errorf("resolve destination chat: %w", err)
	}

	b := &c.sender.To(to.input).Builder
	if silent {
		b = b.Silent()
	}
	forwardedID, err := unpack.MessageID(b.ForwardIDs(from.input, msgID).Send(ctx))
	if err != nil {
		return Message{}, fmt.Errorf("forward %d from %d to %d: %w", msgID, fromChatID, toChatID, err)
	}
	return c.builtMessage(toChatID, forwardedID, "", to, time.Time{}), nil
}

// builtMessage assembles the Message returned by write ops, attaching a t.me
// link for immediate posts (scheduled posts have no public link yet).
func (c *Client) builtMessage(chatID int64, id int, text string, pi peerInfo, scheduledAt time.Time) Message {
	m := Message{ID: id, Text: text}
	if scheduledAt.IsZero() {
		m.Link = BuildLink(LinkArgs{
			Kind:      pi.kind,
			ChannelID: chatID,
			Username:  pi.username,
			MsgID:     id,
		})
	}
	return m
}
