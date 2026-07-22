package tgclient

import (
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestValidateSearch(t *testing.T) {
	query, limit, err := validateSearch("  AI art  ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if query != "AI art" || limit != defaultSearchLimit {
		t.Fatalf("validateSearch = %q, %d", query, limit)
	}

	_, limit, err = validateSearch("AI", maxSearchLimit+100)
	if err != nil {
		t.Fatal(err)
	}
	if limit != maxSearchLimit {
		t.Fatalf("limit = %d, want %d", limit, maxSearchLimit)
	}

	if _, _, err := validateSearch("  ", 1); err == nil {
		t.Fatal("empty query must fail")
	}
}

func TestSearchPostsQuotaBlocksPaidSearch(t *testing.T) {
	c := &Client{loc: time.UTC}
	flood := &tg.SearchPostsFlood{
		TotalDaily:  5,
		Remains:     0,
		WaitTill:    1_800_000_000,
		StarsAmount: 10,
	}
	flood.SetWaitTill(flood.WaitTill)

	got := c.searchPostsQuota(flood)
	if !got.PaidRequired {
		t.Fatal("paid search must be blocked when no free slots remain")
	}
	if got.StarsAmount != 10 || got.WaitTill == "" {
		t.Fatalf("quota = %+v", got)
	}

	flood.Remains = 1
	if c.searchPostsQuota(flood).PaidRequired {
		t.Fatal("remaining free slot must allow search")
	}

	flood.Remains = 0
	flood.SetQueryIsFree(true)
	if c.searchPostsQuota(flood).PaidRequired {
		t.Fatal("free repeated query must allow search")
	}
}

func TestMapChannelSearchResultsCachesDiscoveredPeer(t *testing.T) {
	c := &Client{loc: time.UTC, peers: make(map[int64]peerInfo)}
	channel := testSearchChannel(777, "AI Digest", "ai_digest", true)
	msg := &tg.Message{
		ID:      42,
		PeerID:  &tg.PeerChannel{ChannelID: channel.ID},
		Date:    1_700_000_000,
		Message: "AI art workflow",
	}
	resp := &tg.MessagesMessagesSlice{
		Messages: []tg.MessageClass{msg},
		Chats:    []tg.ChatClass{channel},
	}

	got := c.mapChannelSearchResults(resp)
	if len(got) != 1 {
		t.Fatalf("results = %+v, want 1", got)
	}
	if got[0].Chat.ID != channel.ID || got[0].Chat.Username != "ai_digest" || got[0].Joined {
		t.Fatalf("channel = %+v", got[0])
	}
	if got[0].Match == nil || got[0].Match.Link != "https://t.me/ai_digest/42" {
		t.Fatalf("match = %+v", got[0].Match)
	}

	c.mu.Lock()
	_, cached := c.peers[channel.ID]
	c.mu.Unlock()
	if !cached {
		t.Fatal("discovered channel was not cached for read_chat")
	}
}

func TestMapPublicPostSearchResults(t *testing.T) {
	c := &Client{loc: time.UTC, peers: make(map[int64]peerInfo)}
	channel := testSearchChannel(888, "Creative AI", "creative_ai", false)
	resp := &tg.MessagesMessagesSlice{
		Messages: []tg.MessageClass{
			&tg.Message{
				ID:      7,
				PeerID:  &tg.PeerChannel{ChannelID: channel.ID},
				Date:    1_700_000_100,
				Message: "Short video generators",
			},
		},
		Chats: []tg.ChatClass{channel},
	}

	got := c.mapPublicPostSearchResults(resp)
	if len(got) != 1 {
		t.Fatalf("posts = %+v, want 1", got)
	}
	if got[0].Chat.Title != "Creative AI" || got[0].Message.Link != "https://t.me/creative_ai/7" {
		t.Fatalf("post = %+v", got[0])
	}
}

func TestMergePeersDoesNotReplaceFullPeerWithMinPeer(t *testing.T) {
	c := &Client{peers: map[int64]peerInfo{
		777: {title: "Full", username: "full", min: false},
	}}
	c.mergePeers(map[int64]peerInfo{
		777: {title: "Min", username: "min", min: true},
	})

	c.mu.Lock()
	got := c.peers[777]
	c.mu.Unlock()
	if got.title != "Full" || got.min {
		t.Fatalf("peer = %+v, want existing full peer", got)
	}

	c.mergePeers(map[int64]peerInfo{
		777: {title: "Updated Full", username: "updated", min: false},
	})
	c.mu.Lock()
	got = c.peers[777]
	c.mu.Unlock()
	if got.title != "Updated Full" || got.min {
		t.Fatalf("peer = %+v, want newer full peer", got)
	}
}

func testSearchChannel(id int64, title, username string, left bool) *tg.Channel {
	ch := &tg.Channel{
		ID:        id,
		Title:     title,
		Broadcast: true,
		Left:      left,
		Min:       left,
	}
	ch.SetAccessHash(id * 10)
	ch.SetUsername(username)
	return ch
}
