package tgclient

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/tg"
)

// CreateInviteLink creates one administrator invite link that can be used to
// attribute subscriber growth to a placement or partner.
func (c *Client) CreateInviteLink(ctx context.Context, chatID int64, title string, expiresAt time.Time, usageLimit int, requestNeeded bool) (InviteLink, error) {
	if usageLimit < 0 {
		return InviteLink{}, fmt.Errorf("usage_limit must not be negative")
	}
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > 32 {
		return InviteLink{}, fmt.Errorf("title must not exceed 32 characters")
	}
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return InviteLink{}, err
	}
	if pi.kind != KindChannel && pi.kind != KindChat {
		return InviteLink{}, fmt.Errorf("chat %d does not support invite links", chatID)
	}

	req := &tg.MessagesExportChatInviteRequest{
		Peer:          pi.input,
		RequestNeeded: requestNeeded,
	}
	if title != "" {
		req.SetTitle(title)
	}
	if usageLimit > 0 {
		req.SetUsageLimit(usageLimit)
	}
	if !expiresAt.IsZero() {
		if !expiresAt.After(time.Now()) {
			return InviteLink{}, fmt.Errorf("expires_at must be in the future")
		}
		req.SetExpireDate(int(expiresAt.Unix()))
	}

	created, err := c.api.MessagesExportChatInvite(ctx, req)
	if err != nil {
		return InviteLink{}, fmt.Errorf("create invite link for %d: %w", chatID, err)
	}
	link, ok := exportedInvite(created)
	if !ok {
		return InviteLink{}, fmt.Errorf("unexpected invite link type %T", created)
	}
	return link, nil
}

// ListInviteLinks returns invite links created by the current account. It is a
// read-only operation and exposes Telegram's joined/requested counters.
func (c *Client) ListInviteLinks(ctx context.Context, chatID int64, revoked bool, limit int) ([]InviteLink, int, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	pi, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return nil, 0, err
	}
	if pi.kind != KindChannel && pi.kind != KindChat {
		return nil, 0, fmt.Errorf("chat %d does not support invite links", chatID)
	}

	result, err := c.api.MessagesGetExportedChatInvites(ctx, &tg.MessagesGetExportedChatInvitesRequest{
		Revoked: revoked,
		Peer:    pi.input,
		AdminID: &tg.InputUserSelf{},
		Limit:   limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list invite links for %d: %w", chatID, err)
	}
	links := make([]InviteLink, 0, len(result.Invites))
	for _, raw := range result.Invites {
		if link, ok := exportedInvite(raw); ok {
			links = append(links, link)
		}
	}
	return links, result.Count, nil
}

func exportedInvite(raw tg.ExportedChatInviteClass) (InviteLink, bool) {
	invite, ok := raw.(*tg.ChatInviteExported)
	if !ok {
		return InviteLink{}, false
	}
	out := InviteLink{
		Link:          invite.Link,
		CreatedAt:     time.Unix(int64(invite.Date), 0).UTC(),
		RequestNeeded: invite.RequestNeeded,
		Permanent:     invite.Permanent,
		Revoked:       invite.Revoked,
	}
	out.Title, _ = invite.GetTitle()
	out.UsageLimit, _ = invite.GetUsageLimit()
	out.Joined, _ = invite.GetUsage()
	out.Requested, _ = invite.GetRequested()
	if unix, ok := invite.GetExpireDate(); ok {
		value := time.Unix(int64(unix), 0).UTC()
		out.ExpiresAt = &value
	}
	return out, true
}

// ListChannelFolders returns custom Telegram folders. It never changes them.
func (c *Client) ListChannelFolders(ctx context.Context) ([]ChannelFolder, error) {
	result, err := c.api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channel folders: %w", err)
	}
	folders := make([]ChannelFolder, 0, len(result.Filters))
	for _, raw := range result.Filters {
		if folder, ok := channelFolder(raw); ok {
			folders = append(folders, folder)
		}
	}
	return folders, nil
}

// CreateSharedFolder creates a new custom folder and exports a shareable folder
// link. On export failure it removes the newly created folder best-effort.
func (c *Client) CreateSharedFolder(ctx context.Context, title, shareTitle string, chatIDs []int64) (SharedFolder, error) {
	title = strings.TrimSpace(title)
	shareTitle = strings.TrimSpace(shareTitle)
	if title == "" || utf8.RuneCountInString(title) > 12 {
		return SharedFolder{}, fmt.Errorf("folder title must contain 1 to 12 characters")
	}
	if shareTitle == "" {
		shareTitle = title
	}
	if len(chatIDs) < 2 || len(chatIDs) > 100 {
		return SharedFolder{}, fmt.Errorf("folder must contain between 2 and 100 chats")
	}

	peers := make([]tg.InputPeerClass, 0, len(chatIDs))
	seen := make(map[int64]bool, len(chatIDs))
	for _, chatID := range chatIDs {
		if seen[chatID] {
			continue
		}
		seen[chatID] = true
		pi, err := c.resolvePeer(ctx, chatID)
		if err != nil {
			return SharedFolder{}, err
		}
		if pi.kind != KindChannel && pi.kind != KindChat {
			return SharedFolder{}, fmt.Errorf("chat %d cannot be shared in a channel folder", chatID)
		}
		peers = append(peers, pi.input)
	}
	if len(peers) < 2 {
		return SharedFolder{}, fmt.Errorf("folder must contain at least 2 distinct chats")
	}

	existing, err := c.api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return SharedFolder{}, fmt.Errorf("read folders before create: %w", err)
	}
	folderID, err := nextFolderID(existing.Filters)
	if err != nil {
		return SharedFolder{}, err
	}
	filter := &tg.DialogFilter{
		ID:           folderID,
		Title:        tg.TextWithEntities{Text: title},
		IncludePeers: peers,
	}
	updated, err := c.api.MessagesUpdateDialogFilter(ctx, &tg.MessagesUpdateDialogFilterRequest{
		ID:     folderID,
		Filter: filter,
	})
	if err != nil || !updated {
		if err == nil {
			err = fmt.Errorf("Telegram rejected folder update")
		}
		return SharedFolder{}, fmt.Errorf("create folder: %w", err)
	}

	exported, err := c.api.ChatlistsExportChatlistInvite(ctx, &tg.ChatlistsExportChatlistInviteRequest{
		Chatlist: tg.InputChatlistDialogFilter{FilterID: folderID},
		Title:    shareTitle,
		Peers:    peers,
	})
	if err != nil {
		_, _ = c.api.MessagesUpdateDialogFilter(ctx, &tg.MessagesUpdateDialogFilterRequest{ID: folderID})
		return SharedFolder{}, fmt.Errorf("export folder link: %w", err)
	}
	return SharedFolder{
		ChannelFolder: ChannelFolder{ID: folderID, Title: title, Shared: true, ChatIDs: inputPeerIDs(peers)},
		ShareTitle:    exported.Invite.Title,
		URL:           exported.Invite.URL,
	}, nil
}

func channelFolder(raw tg.DialogFilterClass) (ChannelFolder, bool) {
	switch folder := raw.(type) {
	case *tg.DialogFilter:
		return ChannelFolder{ID: folder.ID, Title: folder.Title.Text, ChatIDs: inputPeerIDs(folder.IncludePeers)}, true
	case *tg.DialogFilterChatlist:
		return ChannelFolder{ID: folder.ID, Title: folder.Title.Text, Shared: folder.HasMyInvites, ChatIDs: inputPeerIDs(folder.IncludePeers)}, true
	default:
		return ChannelFolder{}, false
	}
}

func nextFolderID(filters []tg.DialogFilterClass) (int, error) {
	used := make(map[int]bool, len(filters))
	for _, raw := range filters {
		switch folder := raw.(type) {
		case *tg.DialogFilter:
			used[folder.ID] = true
		case *tg.DialogFilterChatlist:
			used[folder.ID] = true
		}
	}
	for id := 2; id <= 255; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no free Telegram folder id")
}

func inputPeerIDs(peers []tg.InputPeerClass) []int64 {
	ids := make([]int64, 0, len(peers))
	for _, raw := range peers {
		switch peer := raw.(type) {
		case *tg.InputPeerChannel:
			ids = append(ids, peer.ChannelID)
		case *tg.InputPeerChat:
			ids = append(ids, peer.ChatID)
		case *tg.InputPeerUser:
			ids = append(ids, peer.UserID)
		}
	}
	return ids
}
