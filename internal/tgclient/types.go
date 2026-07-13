package tgclient

import "time"

// Chat is a dialog as surfaced to the MCP client.
type Chat struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Kind     PeerKind `json:"type"`
	Username string   `json:"username,omitempty"`
	IsForum  bool     `json:"is_forum"`
	Unread   int      `json:"unread"`
}

// Topic is a forum topic inside a supergroup.
type Topic struct {
	ID           int    `json:"topic_id"`
	Title        string `json:"title"`
	TopMessageID int    `json:"top_message_id"`
}

// Author is the sender of a message.
type Author struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MediaInfo describes an attachment surfaced alongside a message. It carries no
// file bytes — use download_media to fetch the actual file.
type MediaInfo struct {
	Type     string `json:"type"`                // photo | document | video | audio | voice | sticker | other
	FileName string `json:"file_name,omitempty"` // for documents
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Message is a single chat message, already enriched with a clickable link and
// a human-readable local time.
type Message struct {
	ID        int        `json:"msg_id"`
	DateISO   string     `json:"date_iso"`   // RFC3339 in the user's location
	TimeLocal string     `json:"time_local"` // HH:MM for quick scanning
	Author    Author     `json:"author"`
	Text      string     `json:"text"`
	ReplyTo   int        `json:"reply_to,omitempty"`
	Link      string     `json:"link,omitempty"`
	Media     *MediaInfo `json:"media,omitempty"`
	Views     int        `json:"views,omitempty"`
	Forwards  int        `json:"forwards,omitempty"`
	Reactions int        `json:"reactions,omitempty"`
	DateUnix  int64      `json:"-"` // raw timestamp, used for caching/filtering
}

// ChannelProfile contains growth-relevant public metadata returned by
// channels.getFullChannel. Metrics may be absent for chats where Telegram does
// not expose them to the current account.
type ChannelProfile struct {
	ChatID       int64  `json:"chat_id"`
	Title        string `json:"title"`
	Username     string `json:"username,omitempty"`
	About        string `json:"about,omitempty"`
	Subscribers  int    `json:"subscribers,omitempty"`
	LinkedChatID int64  `json:"linked_chat_id,omitempty"`
}

// InviteLink is an administrator-created channel/group invite with attribution
// counters returned by Telegram.
type InviteLink struct {
	Link          string     `json:"link"`
	Title         string     `json:"title,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	UsageLimit    int        `json:"usage_limit,omitempty"`
	Joined        int        `json:"joined"`
	Requested     int        `json:"requested"`
	RequestNeeded bool       `json:"request_needed"`
	Permanent     bool       `json:"permanent"`
	Revoked       bool       `json:"revoked"`
}

// ChannelFolder is a Telegram dialog folder suitable for channel curation.
type ChannelFolder struct {
	ID      int     `json:"id"`
	Title   string  `json:"title"`
	Shared  bool    `json:"shared"`
	ChatIDs []int64 `json:"chat_ids,omitempty"`
}

// SharedFolder is returned after creating a folder and exporting its public
// Telegram folder link.
type SharedFolder struct {
	ChannelFolder
	ShareTitle string `json:"share_title"`
	URL        string `json:"url"`
}
