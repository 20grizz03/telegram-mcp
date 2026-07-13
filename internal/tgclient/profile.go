package tgclient

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"
)

// GetChannelProfile returns public growth-relevant metadata for a channel or
// supergroup already present in the user's dialog list.
func (c *Client) GetChannelProfile(ctx context.Context, chatID int64) (ChannelProfile, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return ChannelProfile{}, err
	}
	if info.kind != KindChannel {
		return ChannelProfile{}, fmt.Errorf("chat %d is not a channel or supergroup", chatID)
	}

	peer, ok := info.input.(*tg.InputPeerChannel)
	if !ok {
		return ChannelProfile{}, fmt.Errorf("chat %d has unsupported peer type %T", chatID, info.input)
	}
	full, err := c.api.ChannelsGetFullChannel(ctx, &tg.InputChannel{
		ChannelID:  peer.ChannelID,
		AccessHash: peer.AccessHash,
	})
	if err != nil {
		return ChannelProfile{}, fmt.Errorf("get channel profile: %w", err)
	}

	channel, ok := full.FullChat.(*tg.ChannelFull)
	if !ok {
		return ChannelProfile{}, fmt.Errorf("unexpected full channel type %T", full.FullChat)
	}
	profile := ChannelProfile{
		ChatID:   chatID,
		Title:    info.title,
		Username: info.username,
		About:    channel.About,
	}
	profile.Subscribers, _ = channel.GetParticipantsCount()
	profile.LinkedChatID, _ = channel.GetLinkedChatID()
	return profile, nil
}
