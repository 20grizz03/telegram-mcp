package tgclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// ChannelSearchResult is one channel returned by Telegram global channel
// search, optionally accompanied by the message that matched the query.
type ChannelSearchResult struct {
	Chat   Chat     `json:"chat"`
	Joined bool     `json:"joined"`
	Match  *Message `json:"match,omitempty"`
}

// SearchPostsQuota describes the current Telegram allowance for global
// full-text post search. PaidRequired is informational: this client never sets
// allow_paid_stars and therefore can never spend Stars.
type SearchPostsQuota struct {
	QueryIsFree  bool   `json:"query_is_free"`
	TotalDaily   int    `json:"total_daily"`
	Remains      int    `json:"remains"`
	WaitTill     string `json:"wait_till,omitempty"`
	StarsAmount  int64  `json:"stars_amount"`
	PaidRequired bool   `json:"paid_required"`
}

// PublicPostSearchResult contains posts returned by a global public-channel
// search plus the quota state checked immediately before the request.
type PublicPostSearchResult struct {
	Posts       []SearchPost     `json:"posts,omitempty"`
	Quota       SearchPostsQuota `json:"quota"`
	BlockedPaid bool             `json:"blocked_paid"`
}

// SearchPost pairs a globally found post with its source channel.
type SearchPost struct {
	Chat    Chat    `json:"chat"`
	Message Message `json:"message"`
}

// SearchChannels searches Telegram's global Channels tab. Unlike ListChats,
// results may include public channels the current account has not joined.
func (c *Client) SearchChannels(ctx context.Context, query string, limit int) ([]ChannelSearchResult, error) {
	query, limit, err := validateSearch(query, limit)
	if err != nil {
		return nil, err
	}

	resp, err := c.api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{
		BroadcastsOnly: true,
		Q:              query,
		Filter:         &tg.InputMessagesFilterEmpty{},
		OffsetPeer:     &tg.InputPeerEmpty{},
		Limit:          limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search channels: %w", err)
	}

	return c.mapChannelSearchResults(resp), nil
}

// SearchPublicPosts searches full text across public channel posts. Telegram
// may charge Stars after a daily allowance is exhausted, so the allowance is
// checked first and paid searches are refused. AllowPaidStars is deliberately
// never populated on the API request as a second safety boundary.
func (c *Client) SearchPublicPosts(ctx context.Context, query string, limit int) (PublicPostSearchResult, error) {
	query, limit, err := validateSearch(query, limit)
	if err != nil {
		return PublicPostSearchResult{}, err
	}

	flood, err := c.api.ChannelsCheckSearchPostsFlood(ctx, &tg.ChannelsCheckSearchPostsFloodRequest{
		Query: query,
	})
	if err != nil {
		return PublicPostSearchResult{}, fmt.Errorf("check public post search quota: %w", err)
	}

	quota := c.searchPostsQuota(flood)
	if quota.PaidRequired {
		return PublicPostSearchResult{Quota: quota, BlockedPaid: true}, nil
	}

	resp, err := c.api.ChannelsSearchPosts(ctx, &tg.ChannelsSearchPostsRequest{
		Query:      query,
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      limit,
		// Never set AllowPaidStars. Even if quota changes between the check and
		// this request, Telegram cannot charge the account.
	})
	if err != nil {
		return PublicPostSearchResult{}, fmt.Errorf("search public posts: %w", err)
	}

	return PublicPostSearchResult{
		Posts: c.mapPublicPostSearchResults(resp),
		Quota: quota,
	}, nil
}

func validateSearch(query string, limit int) (string, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, fmt.Errorf("search query must not be empty")
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	return query, limit, nil
}

func (c *Client) searchPostsQuota(flood *tg.SearchPostsFlood) SearchPostsQuota {
	quota := SearchPostsQuota{
		QueryIsFree: flood.QueryIsFree,
		TotalDaily:  flood.TotalDaily,
		Remains:     flood.Remains,
		StarsAmount: flood.StarsAmount,
	}
	quota.PaidRequired = !quota.QueryIsFree && quota.Remains <= 0
	if waitTill, ok := flood.GetWaitTill(); ok {
		quota.WaitTill = time.Unix(int64(waitTill), 0).In(c.loc).Format(time.RFC3339)
	}
	return quota
}

func (c *Client) mapChannelSearchResults(resp tg.MessagesMessagesClass) []ChannelSearchResult {
	msgs, chats, users := extractMessages(resp)
	idx := indexEntities(chats, users)
	c.cacheSearchPeers(idx)

	joined := make(map[int64]bool, len(idx.channels))
	for id, ch := range idx.channels {
		joined[id] = !ch.Left
	}

	seen := make(map[int64]bool)
	out := make([]ChannelSearchResult, 0, len(idx.channels))
	for _, mc := range msgs {
		m, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		info, id, ok := idx.peerInfoFor(m.PeerID)
		if !ok || info.kind != KindChannel || seen[id] {
			continue
		}
		ch := idx.channels[id]
		if ch == nil || !ch.Broadcast {
			continue
		}
		match := c.mapMessage(m, idx, info, 0)
		out = append(out, ChannelSearchResult{
			Chat:   chatFromPeer(id, info),
			Joined: joined[id],
			Match:  &match,
		})
		seen[id] = true
	}

	// Telegram normally includes one matching message per channel. Keep peers
	// that arrived without a regular message so the result remains useful if
	// that response shape changes or contains service messages.
	for _, cc := range chats {
		ch, ok := cc.(*tg.Channel)
		if !ok || !ch.Broadcast || seen[ch.ID] {
			continue
		}
		info, id, ok := idx.peerInfoFor(&tg.PeerChannel{ChannelID: ch.ID})
		if !ok {
			continue
		}
		out = append(out, ChannelSearchResult{
			Chat:   chatFromPeer(id, info),
			Joined: joined[id],
		})
		seen[id] = true
	}
	return out
}

func (c *Client) mapPublicPostSearchResults(resp tg.MessagesMessagesClass) []SearchPost {
	msgs, chats, users := extractMessages(resp)
	idx := indexEntities(chats, users)
	c.cacheSearchPeers(idx)

	out := make([]SearchPost, 0, len(msgs))
	for _, mc := range msgs {
		m, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		info, id, ok := idx.peerInfoFor(m.PeerID)
		if !ok || info.kind != KindChannel {
			continue
		}
		ch := idx.channels[id]
		if ch == nil || !ch.Broadcast {
			continue
		}
		out = append(out, SearchPost{
			Chat:    chatFromPeer(id, info),
			Message: c.mapMessage(m, idx, info, 0),
		})
	}
	return out
}

func chatFromPeer(id int64, info peerInfo) Chat {
	return Chat{
		ID:       id,
		Title:    info.title,
		Kind:     info.kind,
		Username: info.username,
		IsForum:  info.isForum,
	}
}

// cacheSearchPeers makes discovered channels immediately usable by read_chat,
// sync_chat and get_channel_profile without joining them first.
func (c *Client) cacheSearchPeers(idx entityIndex) {
	peers := make(map[int64]peerInfo, len(idx.channels))
	for id := range idx.channels {
		info, exposedID, ok := idx.peerInfoFor(&tg.PeerChannel{ChannelID: id})
		if ok {
			peers[exposedID] = info
		}
	}

	c.mergePeers(peers)
}
