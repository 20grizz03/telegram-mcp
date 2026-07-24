package tgclient

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/markdown"
	"github.com/gotd/td/telegram/message/rich"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/tg"
)

// md renders markdown text into a styled-text option. Inline user-id mentions
// are not resolved (nil resolver) — not needed for channel posts.
func md(text string) styling.StyledTextOption {
	return markdown.String(nil, text)
}

// RichPhoto identifies a local photo that can be embedded into rich Markdown
// or HTML with tg://photo?id=<ID>.
type RichPhoto struct {
	ID   string
	Path string
}

var richFileIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// UpdateChatDescription changes the description of a basic group,
// supergroup or channel. An empty description clears the existing value.
func (c *Client) UpdateChatDescription(ctx context.Context, chatID int64, description string) error {
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return err
	}
	if pi.kind != KindChat && pi.kind != KindChannel {
		return fmt.Errorf("chat %d is not a group, supergroup or channel", chatID)
	}

	ok, err := c.api.MessagesEditChatAbout(ctx, &tg.MessagesEditChatAboutRequest{
		Peer:  pi.input,
		About: description,
	})
	if err != nil {
		return fmt.Errorf("update description for chat %d: %w", chatID, err)
	}
	if !ok {
		return fmt.Errorf("telegram did not confirm description update for chat %d", chatID)
	}
	return nil
}

// UpdateProfileBio changes the bio of the authenticated Telegram account. An
// empty bio clears it. SetAbout must be used even for an empty value, otherwise
// Telegram interprets the field as omitted instead of clearing it.
func (c *Client) UpdateProfileBio(ctx context.Context, bio string) error {
	_, err := c.api.AccountUpdateProfile(ctx, profileBioRequest(bio))
	if err != nil {
		return fmt.Errorf("update profile bio: %w", err)
	}
	return nil
}

func profileBioRequest(bio string) *tg.AccountUpdateProfileRequest {
	req := &tg.AccountUpdateProfileRequest{}
	req.SetAbout(bio)
	return req
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

// PublishRichPost publishes a native Telegram rich message. Exactly one of
// markdownText and htmlText must be set. Local photos can be referenced from
// the source as tg://photo?id=<ID>. HTTP(S) media URLs can be embedded directly
// using Telegram's rich Markdown/HTML syntax.
func (c *Client) PublishRichPost(
	ctx context.Context,
	chatID int64,
	markdownText, htmlText string,
	photos []RichPhoto,
	rtl, disableAutoLink bool,
	scheduledAt time.Time,
	silent bool,
) (Message, error) {
	if err := validateRichSource(markdownText, htmlText, photos); err != nil {
		return Message{}, err
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

	source, err := c.prepareRichSource(ctx, b, markdownText, htmlText, photos, rtl, disableAutoLink)
	if err != nil {
		return Message{}, err
	}
	id, err := publishedMessageID(b.RichMessage(ctx, source))
	if err != nil {
		return Message{}, fmt.Errorf("publish rich message to %d: %w", chatID, err)
	}

	text := markdownText
	if text == "" {
		text = htmlText
	}
	return c.builtMessage(chatID, id, text, pi, scheduledAt), nil
}

func validateRichSource(markdownText, htmlText string, photos []RichPhoto) error {
	hasMarkdown := strings.TrimSpace(markdownText) != ""
	hasHTML := strings.TrimSpace(htmlText) != ""
	if hasMarkdown == hasHTML {
		return fmt.Errorf("provide exactly one of markdown or html")
	}

	seen := make(map[string]struct{}, len(photos))
	for i, photo := range photos {
		if !richFileIDPattern.MatchString(photo.ID) {
			return fmt.Errorf("photos[%d].id must be 1-64 characters using only A-Z, a-z, 0-9, _ or -", i)
		}
		if strings.TrimSpace(photo.Path) == "" {
			return fmt.Errorf("photos[%d].path is required", i)
		}
		if _, ok := seen[photo.ID]; ok {
			return fmt.Errorf("duplicate rich photo id %q", photo.ID)
		}
		seen[photo.ID] = struct{}{}
	}
	return nil
}

func (c *Client) prepareRichSource(
	ctx context.Context,
	b *message.Builder,
	markdownText, htmlText string,
	photos []RichPhoto,
	rtl, disableAutoLink bool,
) (tg.InputRichMessageClass, error) {
	source := rich.Rich()
	if rtl {
		source = source.RTL()
	}
	if disableAutoLink {
		source = source.NoAutoLink()
	}

	for _, photo := range photos {
		file, err := c.uploader.FromPath(ctx, photo.Path)
		if err != nil {
			return nil, fmt.Errorf("upload rich photo %q from %q: %w", photo.ID, photo.Path, err)
		}
		uploaded, err := b.UploadMedia(ctx, message.UploadedPhoto(file))
		if err != nil {
			return nil, fmt.Errorf("prepare rich photo %q: %w", photo.ID, err)
		}
		input, err := inputPhotoFromMedia(uploaded)
		if err != nil {
			return nil, fmt.Errorf("prepare rich photo %q: %w", photo.ID, err)
		}
		source = source.Photo(photo.ID, input)
	}

	if strings.TrimSpace(markdownText) != "" {
		return source.Markdown(markdownText), nil
	}
	return source.HTML(htmlText), nil
}

func inputPhotoFromMedia(media tg.MessageMediaClass) (*tg.InputPhoto, error) {
	photoMedia, ok := media.(*tg.MessageMediaPhoto)
	if !ok {
		return nil, fmt.Errorf("telegram returned %T instead of photo media", media)
	}
	photo, ok := photoMedia.Photo.(*tg.Photo)
	if !ok {
		return nil, fmt.Errorf("telegram returned %T instead of a reusable photo", photoMedia.Photo)
	}
	return photo.AsInput(), nil
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

// EditScheduledPost replaces the text of a pending scheduled message while
// preserving its publication time. Telegram requires schedule_date when
// editing a scheduled message; using EditPost can otherwise return success
// without changing the pending item.
func (c *Client) EditScheduledPost(ctx context.Context, chatID int64, msgID int, text string) (Message, error) {
	if msgID <= 0 {
		return Message{}, fmt.Errorf("msg_id must be positive")
	}
	if text == "" {
		return Message{}, fmt.Errorf("text is required")
	}
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Message{}, err
	}

	resp, err := c.api.MessagesGetScheduledMessages(ctx, &tg.MessagesGetScheduledMessagesRequest{
		Peer: pi.input,
		ID:   []int{msgID},
	})
	if err != nil {
		return Message{}, fmt.Errorf("get scheduled message %d in %d: %w", msgID, chatID, err)
	}
	msgs, _, _ := extractMessages(resp)
	var scheduledAt time.Time
	for _, mc := range msgs {
		if m, ok := mc.(*tg.Message); ok && m.ID == msgID {
			scheduledAt = time.Unix(int64(m.Date), 0)
			break
		}
	}
	if scheduledAt.IsZero() {
		return Message{}, fmt.Errorf("scheduled message %d not found in %d", msgID, chatID)
	}

	b := c.sender.To(pi.input).Builder.Schedule(scheduledAt)
	if _, err := b.Edit(msgID).StyledText(ctx, md(text)); err != nil {
		return Message{}, fmt.Errorf("edit scheduled %d in %d: %w", msgID, chatID, err)
	}
	return c.builtMessage(chatID, msgID, text, pi, scheduledAt), nil
}

// DeleteScheduledPost removes one pending scheduled message. It never deletes
// an already-published message.
func (c *Client) DeleteScheduledPost(ctx context.Context, chatID int64, msgID int) error {
	if msgID <= 0 {
		return fmt.Errorf("msg_id must be positive")
	}
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return err
	}
	if _, err := c.sender.To(pi.input).Scheduled().Delete(ctx, msgID); err != nil {
		return fmt.Errorf("delete scheduled %d in %d: %w", msgID, chatID, err)
	}
	return nil
}

// EditRichPost replaces a message with native Telegram rich content.
func (c *Client) EditRichPost(
	ctx context.Context,
	chatID int64,
	msgID int,
	markdownText, htmlText string,
	photos []RichPhoto,
	rtl, disableAutoLink bool,
) (Message, error) {
	if msgID <= 0 {
		return Message{}, fmt.Errorf("msg_id must be positive")
	}
	if err := validateRichSource(markdownText, htmlText, photos); err != nil {
		return Message{}, err
	}

	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return Message{}, err
	}
	b := &c.sender.To(pi.input).Builder
	source, err := c.prepareRichSource(ctx, b, markdownText, htmlText, photos, rtl, disableAutoLink)
	if err != nil {
		return Message{}, err
	}
	if _, err := b.Edit(msgID).RichMessage(ctx, source); err != nil {
		return Message{}, fmt.Errorf("edit rich message %d in %d: %w", msgID, chatID, err)
	}

	text := markdownText
	if text == "" {
		text = htmlText
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
