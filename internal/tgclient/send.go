package tgclient

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
)

// SendMessage posts a message to a chat/channel. When asHTML is true, text is
// parsed as Telegram-flavored HTML (<b>, <i>, <a href>, <code>, ...) into
// message entities; otherwise it is sent verbatim as plain text. When topicID > 0
// the message is routed into that forum topic. Link previews are disabled
// (digests are link-heavy). Returns the new message id (0 if it can't be
// extracted from the update response).
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, topicID int, asHTML bool) (int, error) {
	if text == "" {
		return 0, fmt.Errorf("empty message text")
	}
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return 0, err
	}

	b := c.sender.To(info.input).NoWebpage()
	if topicID > 0 {
		b = b.Reply(topicID) // route into the forum topic
	}

	var upd tg.UpdatesClass
	if asHTML {
		upd, err = b.StyledText(ctx, html.String(nil, text))
	} else {
		upd, err = b.Text(ctx, text)
	}
	if err != nil {
		return 0, fmt.Errorf("send message to %d: %w", chatID, err)
	}
	return newMessageID(upd), nil
}

// newMessageID best-effort extracts the id of the message just sent from the
// Updates response.
func newMessageID(upd tg.UpdatesClass) int {
	var list []tg.UpdateClass
	switch u := upd.(type) {
	case *tg.Updates:
		list = u.Updates
	case *tg.UpdatesCombined:
		list = u.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{u.Update}
	case *tg.UpdateShortSentMessage:
		return u.ID
	}
	for _, up := range list {
		switch m := up.(type) {
		case *tg.UpdateNewChannelMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		case *tg.UpdateNewMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		case *tg.UpdateMessageID:
			return m.ID
		}
	}
	return 0
}
