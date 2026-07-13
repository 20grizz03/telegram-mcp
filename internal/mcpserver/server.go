// Package mcpserver exposes the Telegram reader as MCP tools over stdio.
package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/20grizz03/telegram-mcp/internal/store"
	"github.com/20grizz03/telegram-mcp/internal/syncer"
	"github.com/20grizz03/telegram-mcp/internal/tgclient"
)

// handlers binds the tool handlers to a live Telegram client and the local store.
type handlers struct {
	tc *tgclient.Client
	st *store.Store
}

// Build constructs the MCP server with all tools registered. Local CRM/outbox
// tools never contact Telegram. When enableWrite is true, the posting tools
// (publish_post, edit_post, pin_post, forward_post) are also exposed.
func Build(tc *tgclient.Client, st *store.Store, enableWrite bool) *mcp.Server {
	h := &handlers{tc: tc, st: st}
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "telegram-mcp",
		Version: "0.4.3",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_chats",
		Description: "List the user's Telegram dialogs (chats, groups, channels). " +
			"Use this first to find a chat's numeric id before reading it.",
	}, h.listChats)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_topics",
		Description: "List forum topics of a supergroup by chat id. " +
			"Only works for chats where is_forum=true.",
	}, h.listTopics)

	mcp.AddTool(s, &mcp.Tool{
		Name: "read_chat",
		Description: "Read messages of a chat for a time window. Optionally restrict to a single " +
			"forum topic. Returns each message with author, local time and a clickable t.me link. " +
			"Defaults to today if no range is given.",
	}, h.readChat)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_pinned_messages",
		Description: "List pinned messages of a chat, newest first. Use this together with get_channel_profile " +
			"to inspect community rules before posting links, forwarding content or doing self-promotion. Read-only.",
	}, h.listPinnedMessages)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_scheduled_posts",
		Description: "List pending scheduled posts in a channel or chat, earliest first. " +
			"Use it to inspect the editorial queue and prevent duplicate scheduling. Read-only.",
	}, h.listScheduledPosts)

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_channel_profile",
		Description: "Get growth-relevant metadata for a channel or supergroup: description, subscriber count " +
			"and linked discussion chat when Telegram exposes them. Read-only; use list_chats first.",
	}, h.getChannelProfile)

	mcp.AddTool(s, &mcp.Tool{
		Name: "capture_growth_snapshot",
		Description: "Read a channel profile and recent post metrics, then save one timestamped growth snapshot " +
			"to the local database. Read-only in Telegram: never publishes or changes channel state.",
	}, h.captureGrowthSnapshot)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_growth_snapshots",
		Description: "List locally stored channel growth snapshots and compare the oldest and newest values. " +
			"Does not contact Telegram.",
	}, h.listGrowthSnapshots)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_invite_links",
		Description: "List administrator invite links for a channel/group with joined and requested counters. " +
			"Read-only; useful for attributing subscriber growth to placements.",
	}, h.listInviteLinks)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_channel_folders",
		Description: "List custom Telegram channel/chat folders and their included chat ids. Read-only.",
	}, h.listChannelFolders)

	mcp.AddTool(s, &mcp.Tool{
		Name: "sync_chat",
		Description: "Incrementally cache a chat's messages into the local database (and full-text index) " +
			"so search and whole-chat research work fast and offline. Only fetches messages newer than the " +
			"last sync. Optionally restrict to a forum topic. Run this before search_chat on a chat.",
	}, h.syncChat)

	mcp.AddTool(s, &mcp.Tool{
		Name: "search_chat",
		Description: "Full-text search the cached messages (run sync_chat first). Returns matching " +
			"messages with sender, date and a clickable t.me link. Pass chat_id to scope to one chat, " +
			"omit for a global search. Terms match by prefix.",
	}, h.searchChat)

	mcp.AddTool(s, &mcp.Tool{
		Name: "save_preference",
		Description: "Remember a user instruction about what to include or exclude when summarizing " +
			"chats (e.g. \"don't show me job spam\", \"focus on salary numbers\"). Call this whenever " +
			"the user gives feedback on a summary. Pass chat_id to scope the rule to one chat, omit it " +
			"for a global rule. Saved rules are auto-applied to future read_chat results.",
	}, h.savePreference)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_preferences",
		Description: "List saved preference rules. Pass chat_id to see global + that chat's rules; omit for all.",
	}, h.listPreferences)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_preference",
		Description: "Delete a saved preference rule by its id (from list_preferences).",
	}, h.deletePreference)

	mcp.AddTool(s, &mcp.Tool{
		Name: "download_media",
		Description: "Download the media (photo/document/video/...) attached to a specific message to a local file. " +
			"Returns the saved path. Read-only.",
	}, h.downloadMedia)

	mcp.AddTool(s, &mcp.Tool{
		Name: "save_partner",
		Description: "Create or update a local cross-promotion partner record. Reuses an existing record by " +
			"chat_id or username when id is omitted. Local-only: does not contact Telegram.",
	}, h.savePartner)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_partners",
		Description: "List locally tracked cross-promotion partners, optionally filtered by status. " +
			"Does not contact Telegram.",
	}, h.listPartners)

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_outreach_draft",
		Description: "Save a draft message for a potential partner in the local outbox. This tool never sends " +
			"the message and does not contact Telegram.",
	}, h.createOutreachDraft)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_outreach_drafts",
		Description: "List local partner-outreach drafts. Draft statuses are draft, ready and archived; " +
			"there is intentionally no sent status.",
	}, h.listOutreachDrafts)

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_outreach_draft",
		Description: "Edit a local outreach draft or mark it ready/archived. This tool never sends messages " +
			"and does not contact Telegram.",
	}, h.updateOutreachDraft)

	if enableWrite {
		mcp.AddTool(s, &mcp.Tool{
			Name: "create_invite_link",
			Description: "Create a REAL administrator invite link for attribution. This changes Telegram state and is " +
				"only exposed when TGMCP_ENABLE_WRITE is enabled.",
		}, h.createInviteLink)

		mcp.AddTool(s, &mcp.Tool{
			Name: "create_shared_folder",
			Description: "Create a REAL Telegram folder and export its shareable link. This changes the account's " +
				"folder list and is only exposed when TGMCP_ENABLE_WRITE is enabled.",
		}, h.createSharedFolder)

		mcp.AddTool(s, &mcp.Tool{
			Name: "publish_post",
			Description: "Publish a NEW post to a channel/chat under YOUR account — real subscribers " +
				"will receive it. text is markdown. Optionally attach one photo (photo_path = local file, " +
				"or photo_url = external URL) and/or schedule with scheduled_at. Returns the post's t.me link.",
		}, h.publishPost)

		mcp.AddTool(s, &mcp.Tool{
			Name: "edit_post",
			Description: "Edit the text of a post you already published (e.g. fix a typo). text is markdown. " +
				"Identify the post by chat_id + msg_id.",
		}, h.editPost)

		mcp.AddTool(s, &mcp.Tool{
			Name: "pin_post",
			Description: "Pin (or, with unpin=true, unpin) a post in a channel/chat by chat_id + msg_id. " +
				"Set silent=true to pin without notifying subscribers.",
		}, h.pinPost)

		mcp.AddTool(s, &mcp.Tool{
			Name: "forward_post",
			Description: "Natively forward one REAL Telegram post to another chat while preserving source attribution. " +
				"This writes to the destination and is only exposed when TGMCP_ENABLE_WRITE is enabled.",
		}, h.forwardPost)
	}

	return s
}

// ---- list_chats ----

type listChatsIn struct {
	Query string `json:"query,omitempty" jsonschema:"case-insensitive substring filter over title/username"`
	Limit int    `json:"limit,omitempty" jsonschema:"max chats to return (0 = all)"`
}

type listChatsOut struct {
	Chats []tgclient.Chat `json:"chats,omitempty"`
	Count int             `json:"count"`
}

func (h *handlers) listChats(ctx context.Context, _ *mcp.CallToolRequest, in listChatsIn) (*mcp.CallToolResult, listChatsOut, error) {
	chats, err := h.tc.ListChats(ctx, in.Query, in.Limit)
	if err != nil {
		return nil, listChatsOut{}, err
	}
	return nil, listChatsOut{Chats: chats, Count: len(chats)}, nil
}

// ---- list_topics ----

type listTopicsIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
}

type listTopicsOut struct {
	Topics []tgclient.Topic `json:"topics,omitempty"`
	Count  int              `json:"count"`
}

func (h *handlers) listTopics(ctx context.Context, _ *mcp.CallToolRequest, in listTopicsIn) (*mcp.CallToolResult, listTopicsOut, error) {
	topics, err := h.tc.ListTopics(ctx, in.ChatID)
	if err != nil {
		return nil, listTopicsOut{}, err
	}
	return nil, listTopicsOut{Topics: topics, Count: len(topics)}, nil
}

// ---- read_chat ----

type readChatIn struct {
	ChatID  int64  `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	TopicID int    `json:"topic_id,omitempty" jsonschema:"forum topic id to restrict to (from list_topics); 0 = whole chat"`
	From    string `json:"from,omitempty" jsonschema:"window start: today/yesterday, Nd/Nh/Nm ago, YYYY-MM-DD, or RFC3339"`
	To      string `json:"to,omitempty" jsonschema:"window end, same formats; defaults to now"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max messages (default 500)"`
}

type readChatOut struct {
	Chat     tgclient.Chat      `json:"chat"`
	Messages []tgclient.Message `json:"messages,omitempty"`
	Count    int                `json:"count"`
}

func (h *handlers) readChat(ctx context.Context, _ *mcp.CallToolRequest, in readChatIn) (*mcp.CallToolResult, readChatOut, error) {
	chat, msgs, err := h.tc.ReadChat(ctx, in.ChatID, in.TopicID, in.From, in.To, in.Limit)
	if err != nil {
		return nil, readChatOut{}, err
	}
	out := readChatOut{Chat: chat, Messages: msgs, Count: len(msgs)}

	// Auto-inject learned preferences so Claude applies them while summarizing.
	prefs, _ := h.st.ListPreferences(ctx, &in.ChatID)
	text := renderPreferences(prefs) + renderMessages(chat, msgs)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
	return res, out, nil
}

// ---- list_pinned_messages ----

type listPinnedMessagesIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	Limit  int   `json:"limit,omitempty" jsonschema:"max pinned messages (default 20, max 100)"`
}

type listPinnedMessagesOut struct {
	Chat     tgclient.Chat      `json:"chat"`
	Messages []tgclient.Message `json:"messages,omitempty"`
	Count    int                `json:"count"`
}

func (h *handlers) listPinnedMessages(ctx context.Context, _ *mcp.CallToolRequest, in listPinnedMessagesIn) (*mcp.CallToolResult, listPinnedMessagesOut, error) {
	chat, msgs, err := h.tc.ListPinnedMessages(ctx, in.ChatID, in.Limit)
	if err != nil {
		return nil, listPinnedMessagesOut{}, err
	}
	out := listPinnedMessagesOut{Chat: chat, Messages: msgs, Count: len(msgs)}
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: renderMessages(chat, msgs)}}}
	return res, out, nil
}

// ---- list_scheduled_posts ----

type listScheduledPostsIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric channel or chat id from list_chats"`
}

type listScheduledPostsOut struct {
	Chat     tgclient.Chat      `json:"chat"`
	Messages []tgclient.Message `json:"messages,omitempty"`
	Count    int                `json:"count"`
}

func (h *handlers) listScheduledPosts(ctx context.Context, _ *mcp.CallToolRequest, in listScheduledPostsIn) (*mcp.CallToolResult, listScheduledPostsOut, error) {
	chat, msgs, err := h.tc.ListScheduledPosts(ctx, in.ChatID)
	if err != nil {
		return nil, listScheduledPostsOut{}, err
	}
	out := listScheduledPostsOut{Chat: chat, Messages: msgs, Count: len(msgs)}
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: renderMessages(chat, msgs)}}}
	return res, out, nil
}

// ---- get_channel_profile ----

type getChannelProfileIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric channel or supergroup id from list_chats"`
}

type getChannelProfileOut struct {
	Profile tgclient.ChannelProfile `json:"profile"`
}

func (h *handlers) getChannelProfile(ctx context.Context, _ *mcp.CallToolRequest, in getChannelProfileIn) (*mcp.CallToolResult, getChannelProfileOut, error) {
	profile, err := h.tc.GetChannelProfile(ctx, in.ChatID)
	if err != nil {
		return nil, getChannelProfileOut{}, err
	}
	return nil, getChannelProfileOut{Profile: profile}, nil
}

// ---- channel growth snapshots ----

type captureGrowthSnapshotIn struct {
	ChatID           int64 `json:"chat_id" jsonschema:"numeric channel id from list_chats"`
	WindowHours      int   `json:"window_hours,omitempty" jsonschema:"post-analysis window in hours; default 336 (14 days), max 2160"`
	MatureAfterHours int   `json:"mature_after_hours,omitempty" jsonschema:"exclude newer posts from median metrics; default 72 hours"`
}

type captureGrowthSnapshotOut struct {
	Snapshot store.GrowthSnapshot `json:"snapshot"`
}

func (h *handlers) captureGrowthSnapshot(ctx context.Context, _ *mcp.CallToolRequest, in captureGrowthSnapshotIn) (*mcp.CallToolResult, captureGrowthSnapshotOut, error) {
	windowHours := in.WindowHours
	if windowHours == 0 {
		windowHours = 14 * 24
	}
	if windowHours < 1 || windowHours > 90*24 {
		return nil, captureGrowthSnapshotOut{}, fmt.Errorf("window_hours must be between 1 and 2160")
	}
	matureAfterHours := in.MatureAfterHours
	if matureAfterHours == 0 {
		matureAfterHours = 72
	}
	if matureAfterHours < 0 || matureAfterHours >= windowHours {
		return nil, captureGrowthSnapshotOut{}, fmt.Errorf("mature_after_hours must be non-negative and less than window_hours")
	}

	profile, err := h.tc.GetChannelProfile(ctx, in.ChatID)
	if err != nil {
		return nil, captureGrowthSnapshotOut{}, err
	}
	_, messages, err := h.tc.ReadChat(ctx, in.ChatID, 0, fmt.Sprintf("%dh", windowHours), "", 500)
	if err != nil {
		return nil, captureGrowthSnapshotOut{}, err
	}

	cutoff := time.Now().Add(-time.Duration(matureAfterHours) * time.Hour).Unix()
	views := make([]int, 0, len(messages))
	forwards := make([]int, 0, len(messages))
	reactions := make([]int, 0, len(messages))
	for _, msg := range messages {
		if msg.DateUnix > cutoff {
			continue
		}
		views = append(views, msg.Views)
		forwards = append(forwards, msg.Forwards)
		reactions = append(reactions, msg.Reactions)
	}

	snap, err := h.st.SaveGrowthSnapshot(ctx, store.GrowthSnapshot{
		ChatID:           in.ChatID,
		Title:            profile.Title,
		Username:         profile.Username,
		Subscribers:      profile.Subscribers,
		Posts:            len(messages),
		MaturePosts:      len(views),
		MedianViews:      medianInt(views),
		MedianForwards:   medianInt(forwards),
		MedianReactions:  medianInt(reactions),
		WindowHours:      windowHours,
		MatureAfterHours: matureAfterHours,
	})
	if err != nil {
		return nil, captureGrowthSnapshotOut{}, err
	}
	return nil, captureGrowthSnapshotOut{Snapshot: snap}, nil
}

type listGrowthSnapshotsIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric channel id"`
	Days   int   `json:"days,omitempty" jsonschema:"only snapshots from the last N days; 0 = all"`
	Limit  int   `json:"limit,omitempty" jsonschema:"max snapshots to return (default 100, max 500)"`
}

type growthComparison struct {
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	SubscribersDelta int       `json:"subscribers_delta"`
	MedianViewsDelta int       `json:"median_views_delta"`
}

type listGrowthSnapshotsOut struct {
	Snapshots  []store.GrowthSnapshot `json:"snapshots,omitempty"`
	Count      int                    `json:"count"`
	Comparison *growthComparison      `json:"comparison,omitempty"`
}

func (h *handlers) listGrowthSnapshots(ctx context.Context, _ *mcp.CallToolRequest, in listGrowthSnapshotsIn) (*mcp.CallToolResult, listGrowthSnapshotsOut, error) {
	snapshots, err := h.st.ListGrowthSnapshots(ctx, in.ChatID, in.Days, in.Limit)
	if err != nil {
		return nil, listGrowthSnapshotsOut{}, err
	}
	out := listGrowthSnapshotsOut{Snapshots: snapshots, Count: len(snapshots)}
	if len(snapshots) >= 2 {
		latest := snapshots[0]
		earliest := snapshots[len(snapshots)-1]
		out.Comparison = &growthComparison{
			From:             earliest.CapturedAt,
			To:               latest.CapturedAt,
			SubscribersDelta: latest.Subscribers - earliest.Subscribers,
			MedianViewsDelta: latest.MedianViews - earliest.MedianViews,
		}
	}
	return nil, out, nil
}

func medianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	values = append([]int(nil), values...)
	sort.Ints(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

// ---- invite links and channel folders ----

type listInviteLinksIn struct {
	ChatID  int64 `json:"chat_id" jsonschema:"numeric channel or group id"`
	Revoked bool  `json:"revoked,omitempty" jsonschema:"list revoked links instead of active links"`
	Limit   int   `json:"limit,omitempty" jsonschema:"max links to return (default/max 100)"`
}

type listInviteLinksOut struct {
	Links []tgclient.InviteLink `json:"links,omitempty"`
	Count int                   `json:"count"`
	Total int                   `json:"total"`
}

func (h *handlers) listInviteLinks(ctx context.Context, _ *mcp.CallToolRequest, in listInviteLinksIn) (*mcp.CallToolResult, listInviteLinksOut, error) {
	links, total, err := h.tc.ListInviteLinks(ctx, in.ChatID, in.Revoked, in.Limit)
	if err != nil {
		return nil, listInviteLinksOut{}, err
	}
	return nil, listInviteLinksOut{Links: links, Count: len(links), Total: total}, nil
}

type createInviteLinkIn struct {
	ChatID        int64  `json:"chat_id" jsonschema:"numeric channel or group id"`
	Title         string `json:"title,omitempty" jsonschema:"administrator-only attribution label, max 32 characters"`
	ExpiresAt     string `json:"expires_at,omitempty" jsonschema:"expiry: Nd/Nh/Nm from now, YYYY-MM-DD or RFC3339; omit for no expiry"`
	UsageLimit    int    `json:"usage_limit,omitempty" jsonschema:"maximum joins; 0 = unlimited"`
	RequestNeeded bool   `json:"request_needed,omitempty" jsonschema:"require administrator approval for each join"`
}

type createInviteLinkOut struct {
	Invite tgclient.InviteLink `json:"invite"`
}

func (h *handlers) createInviteLink(ctx context.Context, _ *mcp.CallToolRequest, in createInviteLinkIn) (*mcp.CallToolResult, createInviteLinkOut, error) {
	expiresAt, err := tgclient.ParseFutureInstant(in.ExpiresAt, h.tc.Location())
	if err != nil {
		return nil, createInviteLinkOut{}, err
	}
	invite, err := h.tc.CreateInviteLink(ctx, in.ChatID, in.Title, expiresAt, in.UsageLimit, in.RequestNeeded)
	if err != nil {
		return nil, createInviteLinkOut{}, err
	}
	return nil, createInviteLinkOut{Invite: invite}, nil
}

type listChannelFoldersOut struct {
	Folders []tgclient.ChannelFolder `json:"folders,omitempty"`
	Count   int                      `json:"count"`
}

func (h *handlers) listChannelFolders(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listChannelFoldersOut, error) {
	folders, err := h.tc.ListChannelFolders(ctx)
	if err != nil {
		return nil, listChannelFoldersOut{}, err
	}
	return nil, listChannelFoldersOut{Folders: folders, Count: len(folders)}, nil
}

type createSharedFolderIn struct {
	Title      string  `json:"title" jsonschema:"Telegram folder title, 1 to 12 characters"`
	ShareTitle string  `json:"share_title,omitempty" jsonschema:"label shown on the exported folder link; defaults to title"`
	ChatIDs    []int64 `json:"chat_ids" jsonschema:"2 to 100 channel/group ids from list_chats"`
}

type createSharedFolderOut struct {
	Folder tgclient.SharedFolder `json:"folder"`
}

func (h *handlers) createSharedFolder(ctx context.Context, _ *mcp.CallToolRequest, in createSharedFolderIn) (*mcp.CallToolResult, createSharedFolderOut, error) {
	folder, err := h.tc.CreateSharedFolder(ctx, in.Title, in.ShareTitle, in.ChatIDs)
	if err != nil {
		return nil, createSharedFolderOut{}, err
	}
	return nil, createSharedFolderOut{Folder: folder}, nil
}

type forwardPostIn struct {
	FromChatID int64 `json:"from_chat_id" jsonschema:"numeric source chat/channel id"`
	ToChatID   int64 `json:"to_chat_id" jsonschema:"numeric destination chat/group/channel id"`
	MsgID      int   `json:"msg_id" jsonschema:"source message id to forward"`
	Silent     bool  `json:"silent,omitempty" jsonschema:"forward without a notification"`
}

type forwardPostOut struct {
	MsgID int    `json:"msg_id"`
	Link  string `json:"link,omitempty"`
}

func (h *handlers) forwardPost(ctx context.Context, _ *mcp.CallToolRequest, in forwardPostIn) (*mcp.CallToolResult, forwardPostOut, error) {
	message, err := h.tc.ForwardPost(ctx, in.FromChatID, in.ToChatID, in.MsgID, in.Silent)
	if err != nil {
		return nil, forwardPostOut{}, err
	}
	return nil, forwardPostOut{MsgID: message.ID, Link: message.Link}, nil
}

func renderPreferences(prefs []store.Preference) string {
	if len(prefs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("⚙️ Active preferences — apply these when summarizing. " +
		"If the user gives new feedback, call save_preference to remember it.\n")
	for _, p := range prefs {
		scope := "global"
		if p.Scope == "chat" {
			scope = "this chat"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", scope, p.Rule)
	}
	b.WriteString("\n")
	return b.String()
}

// ---- sync_chat ----

type syncChatIn struct {
	ChatID  int64 `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	TopicID int   `json:"topic_id,omitempty" jsonschema:"restrict caching to this forum topic; 0 = whole chat"`
	Max     int   `json:"max,omitempty" jsonschema:"max messages to fetch this run (default 1000)"`
}

type syncChatOut = syncer.Result

func (h *handlers) syncChat(ctx context.Context, _ *mcp.CallToolRequest, in syncChatIn) (*mcp.CallToolResult, syncChatOut, error) {
	res, err := syncer.Sync(ctx, h.tc, h.st, in.ChatID, in.TopicID, in.Max)
	if err != nil {
		return nil, syncChatOut{}, err
	}
	return nil, res, nil
}

// ---- search_chat ----

type searchChatIn struct {
	Query  string `json:"query" jsonschema:"full-text query; terms match by prefix"`
	ChatID int64  `json:"chat_id,omitempty" jsonschema:"scope to this chat; omit/0 for global search"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max hits (default 50)"`
}

type searchHit struct {
	ChatID  int64  `json:"chat_id"`
	MsgID   int    `json:"msg_id"`
	DateISO string `json:"date_iso"`
	Sender  string `json:"sender"`
	Text    string `json:"text"`
	Link    string `json:"link,omitempty"`
}

type searchChatOut struct {
	Hits  []searchHit `json:"hits,omitempty"`
	Count int         `json:"count"`
}

func (h *handlers) searchChat(ctx context.Context, _ *mcp.CallToolRequest, in searchChatIn) (*mcp.CallToolResult, searchChatOut, error) {
	var chatID *int64
	if in.ChatID != 0 {
		chatID = &in.ChatID
	}
	hits, err := h.st.SearchMessages(ctx, chatID, in.Query, in.Limit)
	if err != nil {
		return nil, searchChatOut{}, err
	}

	loc := h.tc.Location()
	out := make([]searchHit, 0, len(hits))
	for _, hh := range hits {
		meta, _, _ := h.st.GetChat(ctx, hh.ChatID)
		out = append(out, searchHit{
			ChatID:  hh.ChatID,
			MsgID:   hh.MsgID,
			DateISO: time.Unix(hh.Date, 0).In(loc).Format(time.RFC3339),
			Sender:  hh.Sender,
			Text:    hh.Text,
			Link: tgclient.BuildLink(tgclient.LinkArgs{
				Kind:      tgclient.PeerKind(meta.Kind),
				ChannelID: hh.ChatID,
				Username:  meta.Username,
				MsgID:     hh.MsgID,
				TopicID:   hh.TopicID,
			}),
		})
	}
	return nil, searchChatOut{Hits: out, Count: len(out)}, nil
}

// ---- save_preference ----

type savePreferenceIn struct {
	Rule   string `json:"rule" jsonschema:"the instruction to remember, e.g. 'skip job spam'"`
	ChatID int64  `json:"chat_id,omitempty" jsonschema:"scope the rule to this chat; omit/0 for a global rule"`
}

type savePreferenceOut struct {
	Saved store.Preference `json:"saved"`
}

func (h *handlers) savePreference(ctx context.Context, _ *mcp.CallToolRequest, in savePreferenceIn) (*mcp.CallToolResult, savePreferenceOut, error) {
	var chatID *int64
	if in.ChatID != 0 {
		chatID = &in.ChatID
	}
	p, err := h.st.AddPreference(ctx, in.Rule, chatID)
	if err != nil {
		return nil, savePreferenceOut{}, err
	}
	return nil, savePreferenceOut{Saved: p}, nil
}

// ---- list_preferences ----

type listPreferencesIn struct {
	ChatID int64 `json:"chat_id,omitempty" jsonschema:"show global + this chat's rules; omit/0 for all rules"`
}

type listPreferencesOut struct {
	Preferences []store.Preference `json:"preferences,omitempty"`
	Count       int                `json:"count"`
}

func (h *handlers) listPreferences(ctx context.Context, _ *mcp.CallToolRequest, in listPreferencesIn) (*mcp.CallToolResult, listPreferencesOut, error) {
	var chatID *int64
	if in.ChatID != 0 {
		chatID = &in.ChatID
	}
	prefs, err := h.st.ListPreferences(ctx, chatID)
	if err != nil {
		return nil, listPreferencesOut{}, err
	}
	return nil, listPreferencesOut{Preferences: prefs, Count: len(prefs)}, nil
}

// ---- delete_preference ----

type deletePreferenceIn struct {
	ID int64 `json:"id" jsonschema:"preference id from list_preferences"`
}

type deletePreferenceOut struct {
	Deleted bool `json:"deleted"`
}

func (h *handlers) deletePreference(ctx context.Context, _ *mcp.CallToolRequest, in deletePreferenceIn) (*mcp.CallToolResult, deletePreferenceOut, error) {
	ok, err := h.st.DeletePreference(ctx, in.ID)
	if err != nil {
		return nil, deletePreferenceOut{}, err
	}
	return nil, deletePreferenceOut{Deleted: ok}, nil
}

// ---- download_media ----

type downloadMediaIn struct {
	ChatID int64  `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	MsgID  int    `json:"msg_id" jsonschema:"id of the message whose media to download"`
	OutDir string `json:"out_dir,omitempty" jsonschema:"directory to save into; defaults to a temp dir"`
}

type downloadMediaOut struct {
	Path  string             `json:"path"`
	Media tgclient.MediaInfo `json:"media"`
}

func (h *handlers) downloadMedia(ctx context.Context, _ *mcp.CallToolRequest, in downloadMediaIn) (*mcp.CallToolResult, downloadMediaOut, error) {
	path, media, err := h.tc.DownloadMedia(ctx, in.ChatID, in.MsgID, in.OutDir)
	if err != nil {
		return nil, downloadMediaOut{}, err
	}
	out := downloadMediaOut{Path: path, Media: media}
	msg := fmt.Sprintf("saved %s to %s", mediaSummary(media), path)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, out, nil
}

// ---- growth partner CRM (local only) ----

type savePartnerIn struct {
	ID           int64   `json:"id,omitempty" jsonschema:"existing partner id to update; omit to create or reuse by chat_id/username"`
	ChatID       *int64  `json:"chat_id,omitempty" jsonschema:"Telegram chat id, when known"`
	Username     *string `json:"username,omitempty" jsonschema:"Telegram username, with or without @"`
	Title        *string `json:"title,omitempty" jsonschema:"channel or partner display name"`
	Contact      *string `json:"contact,omitempty" jsonschema:"contact person or handle"`
	Status       *string `json:"status,omitempty" jsonschema:"candidate, contacted, negotiating, agreed, active, paused or declined"`
	Notes        *string `json:"notes,omitempty" jsonschema:"research and relationship notes"`
	Terms        *string `json:"terms,omitempty" jsonschema:"agreed or proposed repost terms"`
	AudienceSize *int    `json:"audience_size,omitempty" jsonschema:"subscriber or member count"`
	MedianViews  *int    `json:"median_views,omitempty" jsonschema:"representative median post views"`
	NextActionAt *string `json:"next_action_at,omitempty" jsonschema:"next follow-up time in RFC3339; pass an empty string to clear"`
}

type savePartnerOut struct {
	Partner store.Partner `json:"partner"`
}

func (h *handlers) savePartner(ctx context.Context, _ *mcp.CallToolRequest, in savePartnerIn) (*mcp.CallToolResult, savePartnerOut, error) {
	var (
		p   store.Partner
		err error
	)
	if in.ID != 0 {
		p, err = h.st.GetPartner(ctx, in.ID)
		if err != nil {
			return nil, savePartnerOut{}, err
		}
	} else {
		username := ""
		if in.Username != nil {
			username = *in.Username
		}
		if existing, found, findErr := h.st.FindPartner(ctx, in.ChatID, username); findErr != nil {
			return nil, savePartnerOut{}, findErr
		} else if found {
			p = existing
		}
	}
	if in.ChatID != nil {
		p.ChatID = in.ChatID
	}
	if in.Username != nil {
		p.Username = *in.Username
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	if in.Contact != nil {
		p.Contact = *in.Contact
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	if in.Notes != nil {
		p.Notes = *in.Notes
	}
	if in.Terms != nil {
		p.Terms = *in.Terms
	}
	if in.AudienceSize != nil {
		p.AudienceSize = *in.AudienceSize
	}
	if in.MedianViews != nil {
		p.MedianViews = *in.MedianViews
	}
	if in.NextActionAt != nil {
		p.NextActionAt, err = parseOptionalRFC3339(*in.NextActionAt)
		if err != nil {
			return nil, savePartnerOut{}, err
		}
	}
	p, err = h.st.SavePartner(ctx, p)
	if err != nil {
		return nil, savePartnerOut{}, err
	}
	return nil, savePartnerOut{Partner: p}, nil
}

type listPartnersIn struct {
	Status string `json:"status,omitempty" jsonschema:"optional partner status filter"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max partners to return (default 100, max 500)"`
}

type listPartnersOut struct {
	Partners []store.Partner `json:"partners,omitempty"`
	Count    int             `json:"count"`
}

func (h *handlers) listPartners(ctx context.Context, _ *mcp.CallToolRequest, in listPartnersIn) (*mcp.CallToolResult, listPartnersOut, error) {
	partners, err := h.st.ListPartners(ctx, in.Status, in.Limit)
	if err != nil {
		return nil, listPartnersOut{}, err
	}
	return nil, listPartnersOut{Partners: partners, Count: len(partners)}, nil
}

// ---- partner outreach outbox (local only, no delivery) ----

type createOutreachDraftIn struct {
	PartnerID      int64  `json:"partner_id,omitempty" jsonschema:"local partner id from list_partners"`
	TargetChatID   int64  `json:"target_chat_id,omitempty" jsonschema:"Telegram chat id to address later"`
	TargetUsername string `json:"target_username,omitempty" jsonschema:"Telegram username to address later"`
	Text           string `json:"text" jsonschema:"draft message text; stored locally only"`
}

type outreachDraftOut struct {
	Draft store.OutreachDraft `json:"draft"`
}

func (h *handlers) createOutreachDraft(ctx context.Context, _ *mcp.CallToolRequest, in createOutreachDraftIn) (*mcp.CallToolResult, outreachDraftOut, error) {
	var partnerID, targetChatID *int64
	if in.PartnerID != 0 {
		partnerID = &in.PartnerID
	}
	if in.TargetChatID != 0 {
		targetChatID = &in.TargetChatID
	}
	draft, err := h.st.CreateOutreachDraft(ctx, store.OutreachDraft{
		PartnerID:      partnerID,
		TargetChatID:   targetChatID,
		TargetUsername: in.TargetUsername,
		Text:           in.Text,
	})
	if err != nil {
		return nil, outreachDraftOut{}, err
	}
	return nil, outreachDraftOut{Draft: draft}, nil
}

type listOutreachDraftsIn struct {
	Status    string `json:"status,omitempty" jsonschema:"optional draft, ready or archived filter"`
	PartnerID int64  `json:"partner_id,omitempty" jsonschema:"optional local partner id filter"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max drafts to return (default 100, max 500)"`
}

type listOutreachDraftsOut struct {
	Drafts []store.OutreachDraft `json:"drafts,omitempty"`
	Count  int                   `json:"count"`
}

func (h *handlers) listOutreachDrafts(ctx context.Context, _ *mcp.CallToolRequest, in listOutreachDraftsIn) (*mcp.CallToolResult, listOutreachDraftsOut, error) {
	var partnerID *int64
	if in.PartnerID != 0 {
		partnerID = &in.PartnerID
	}
	drafts, err := h.st.ListOutreachDrafts(ctx, in.Status, partnerID, in.Limit)
	if err != nil {
		return nil, listOutreachDraftsOut{}, err
	}
	return nil, listOutreachDraftsOut{Drafts: drafts, Count: len(drafts)}, nil
}

type updateOutreachDraftIn struct {
	ID     int64  `json:"id" jsonschema:"local draft id from list_outreach_drafts"`
	Text   string `json:"text,omitempty" jsonschema:"replacement draft text; omit to keep current text"`
	Status string `json:"status,omitempty" jsonschema:"draft, ready or archived; omit to keep current status"`
}

func (h *handlers) updateOutreachDraft(ctx context.Context, _ *mcp.CallToolRequest, in updateOutreachDraftIn) (*mcp.CallToolResult, outreachDraftOut, error) {
	draft, err := h.st.UpdateOutreachDraft(ctx, in.ID, in.Text, in.Status)
	if err != nil {
		return nil, outreachDraftOut{}, err
	}
	return nil, outreachDraftOut{Draft: draft}, nil
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("next_action_at must be RFC3339: %w", err)
	}
	return &parsed, nil
}

func mediaSummary(m tgclient.MediaInfo) string {
	if m.FileName != "" {
		return fmt.Sprintf("%s %q", m.Type, m.FileName)
	}
	return m.Type
}

// ---- publish_post ----

type publishPostIn struct {
	ChatID      int64  `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	Text        string `json:"text,omitempty" jsonschema:"post body in markdown; required unless a photo is attached"`
	PhotoPath   string `json:"photo_path,omitempty" jsonschema:"local image file path to attach; mutually exclusive with photo_url"`
	PhotoURL    string `json:"photo_url,omitempty" jsonschema:"external image URL to attach; mutually exclusive with photo_path"`
	ScheduledAt string `json:"scheduled_at,omitempty" jsonschema:"when to publish: Nd/Nh/Nm from now, YYYY-MM-DD or RFC3339; omit to post now"`
	Silent      bool   `json:"silent,omitempty" jsonschema:"post without a notification to subscribers"`
}

type publishPostOut struct {
	MsgID     int    `json:"msg_id"`
	Link      string `json:"link,omitempty"`
	Scheduled bool   `json:"scheduled"`
}

func (h *handlers) publishPost(ctx context.Context, _ *mcp.CallToolRequest, in publishPostIn) (*mcp.CallToolResult, publishPostOut, error) {
	when, err := tgclient.ParseFutureInstant(in.ScheduledAt, h.tc.Location())
	if err != nil {
		return nil, publishPostOut{}, err
	}
	m, err := h.tc.PublishPost(ctx, in.ChatID, in.Text, in.PhotoPath, in.PhotoURL, when, in.Silent)
	if err != nil {
		return nil, publishPostOut{}, err
	}
	out := publishPostOut{MsgID: m.ID, Link: m.Link, Scheduled: !when.IsZero()}
	msg := fmt.Sprintf("published msg_id=%d %s", out.MsgID, out.Link)
	if out.Scheduled {
		msg = fmt.Sprintf("scheduled msg_id=%d for %s", out.MsgID, when.Format(time.RFC3339))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, out, nil
}

// ---- edit_post ----

type editPostIn struct {
	ChatID int64  `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	MsgID  int    `json:"msg_id" jsonschema:"id of the post to edit"`
	Text   string `json:"text" jsonschema:"new post body in markdown"`
}

type editPostOut struct {
	MsgID int    `json:"msg_id"`
	Link  string `json:"link,omitempty"`
}

func (h *handlers) editPost(ctx context.Context, _ *mcp.CallToolRequest, in editPostIn) (*mcp.CallToolResult, editPostOut, error) {
	m, err := h.tc.EditPost(ctx, in.ChatID, in.MsgID, in.Text)
	if err != nil {
		return nil, editPostOut{}, err
	}
	return nil, editPostOut{MsgID: m.ID, Link: m.Link}, nil
}

// ---- pin_post ----

type pinPostIn struct {
	ChatID int64 `json:"chat_id" jsonschema:"numeric chat id from list_chats"`
	MsgID  int   `json:"msg_id" jsonschema:"id of the post to pin/unpin"`
	Silent bool  `json:"silent,omitempty" jsonschema:"pin without notifying subscribers"`
	Unpin  bool  `json:"unpin,omitempty" jsonschema:"unpin instead of pin"`
}

type pinPostOut struct {
	Pinned bool `json:"pinned"`
}

func (h *handlers) pinPost(ctx context.Context, _ *mcp.CallToolRequest, in pinPostIn) (*mcp.CallToolResult, pinPostOut, error) {
	if err := h.tc.PinPost(ctx, in.ChatID, in.MsgID, in.Silent, in.Unpin); err != nil {
		return nil, pinPostOut{}, err
	}
	return nil, pinPostOut{Pinned: !in.Unpin}, nil
}

func renderMessages(chat tgclient.Chat, msgs []tgclient.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %d messages\n", chat.Title, len(msgs))
	for _, m := range msgs {
		date := ""
		if len(m.DateISO) >= 10 {
			date = m.DateISO[:10]
		}
		fmt.Fprintf(&b, "[%s %s] %s: %s", date, m.TimeLocal, m.Author.Name, m.Text)
		if m.Views != 0 || m.Forwards != 0 || m.Reactions != 0 {
			fmt.Fprintf(&b, "  [views=%d forwards=%d reactions=%d]", m.Views, m.Forwards, m.Reactions)
		}
		if m.Link != "" {
			fmt.Fprintf(&b, "  %s", m.Link)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
