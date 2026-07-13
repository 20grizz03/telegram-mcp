package tgclient

import (
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestMapMessageIncludesGrowthMetrics(t *testing.T) {
	c := &Client{loc: time.UTC}
	wire := &tg.Message{ID: 17, Date: 1_700_000_000, Message: "release notes"}
	wire.SetViews(1200)
	wire.SetForwards(34)
	wire.SetReactions(tg.MessageReactions{Results: []tg.ReactionCount{
		{Count: 5},
		{Count: 8},
	}})

	got := c.mapMessage(wire, entityIndex{}, peerInfo{
		kind:      KindChannel,
		title:     "Example Channel",
		username:  "example_channel",
		channelID: 123,
	}, 0)
	if got.Views != 1200 || got.Forwards != 34 || got.Reactions != 13 {
		t.Fatalf("metrics = views:%d forwards:%d reactions:%d", got.Views, got.Forwards, got.Reactions)
	}
}
