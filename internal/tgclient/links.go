package tgclient

import (
	"fmt"
	"strconv"
)

// PeerKind classifies a dialog/peer.
type PeerKind string

const (
	KindUser    PeerKind = "user"    // private chat with a user
	KindChat    PeerKind = "chat"    // legacy basic group
	KindChannel PeerKind = "channel" // supergroup or broadcast channel
)

// LinkArgs is everything BuildLink needs to construct a t.me deep link.
type LinkArgs struct {
	Kind      PeerKind
	ChannelID int64  // raw channel id (positive, without the -100 prefix)
	Username  string // public @username, empty for private peers
	MsgID     int    // target message id
	TopicID   int    // forum topic id, 0 if not in a topic
}

// BuildLink returns a clickable t.me link that opens the exact message, or an
// empty string when no public/deep link is possible (users and basic groups
// have no addressable message links).
//
// Formats:
//
//	public channel:          https://t.me/<username>/<msg>
//	public channel + topic:  https://t.me/<username>/<topic>/<msg>
//	private channel:         https://t.me/c/<channelID>/<msg>
//	private channel + topic: https://t.me/c/<channelID>/<topic>/<msg>
func BuildLink(a LinkArgs) string {
	if a.Kind != KindChannel || a.MsgID <= 0 {
		return ""
	}
	msg := strconv.Itoa(a.MsgID)

	if a.Username != "" {
		if a.TopicID > 0 {
			return fmt.Sprintf("https://t.me/%s/%d/%s", a.Username, a.TopicID, msg)
		}
		return fmt.Sprintf("https://t.me/%s/%s", a.Username, msg)
	}

	if a.ChannelID == 0 {
		return ""
	}
	if a.TopicID > 0 {
		return fmt.Sprintf("https://t.me/c/%d/%d/%s", a.ChannelID, a.TopicID, msg)
	}
	return fmt.Sprintf("https://t.me/c/%d/%s", a.ChannelID, msg)
}
