package tgclient

import (
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
)

// entityIndex resolves peers and author names from the users/chats blocks that
// accompany every Telegram response.
type entityIndex struct {
	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
}

func indexEntities(chats []tg.ChatClass, users []tg.UserClass) entityIndex {
	idx := entityIndex{
		users:    make(map[int64]*tg.User),
		chats:    make(map[int64]*tg.Chat),
		channels: make(map[int64]*tg.Channel),
	}
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			idx.users[user.ID] = user
		}
	}
	for _, c := range chats {
		switch v := c.(type) {
		case *tg.Chat:
			idx.chats[v.ID] = v
		case *tg.Channel:
			idx.channels[v.ID] = v
		}
	}
	return idx
}

// peerInfoFor resolves a Peer to an addressable peerInfo and its exposed id.
func (idx entityIndex) peerInfoFor(peer tg.PeerClass) (peerInfo, int64, bool) {
	switch p := peer.(type) {
	case *tg.PeerUser:
		u, ok := idx.users[p.UserID]
		if !ok {
			return peerInfo{}, 0, false
		}
		username, _ := u.GetUsername()
		return peerInfo{
			input:    &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash},
			kind:     KindUser,
			username: username,
			title:    userName(u),
		}, u.ID, true

	case *tg.PeerChat:
		ch, ok := idx.chats[p.ChatID]
		if !ok {
			return peerInfo{}, 0, false
		}
		return peerInfo{
			input: &tg.InputPeerChat{ChatID: ch.ID},
			kind:  KindChat,
			title: ch.Title,
		}, ch.ID, true

	case *tg.PeerChannel:
		ch, ok := idx.channels[p.ChannelID]
		if !ok {
			return peerInfo{}, 0, false
		}
		username, _ := ch.GetUsername()
		return peerInfo{
			input:     &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash},
			kind:      KindChannel,
			username:  username,
			channelID: ch.ID,
			isForum:   ch.Forum,
			title:     ch.Title,
		}, ch.ID, true
	}
	return peerInfo{}, 0, false
}

// authorFor returns the display id+name of a message sender.
func (idx entityIndex) authorFor(from tg.PeerClass, fallbackTitle string) Author {
	switch p := from.(type) {
	case *tg.PeerUser:
		if u, ok := idx.users[p.UserID]; ok {
			return Author{ID: u.ID, Name: userName(u)}
		}
		return Author{ID: p.UserID, Name: fmt.Sprintf("user %d", p.UserID)}
	case *tg.PeerChannel:
		if ch, ok := idx.channels[p.ChannelID]; ok {
			return Author{ID: ch.ID, Name: ch.Title}
		}
		return Author{ID: p.ChannelID, Name: fallbackTitle}
	case *tg.PeerChat:
		if ch, ok := idx.chats[p.ChatID]; ok {
			return Author{ID: ch.ID, Name: ch.Title}
		}
	}
	// Channel posts often have no sender: attribute to the channel itself.
	return Author{Name: fallbackTitle}
}

func userName(u *tg.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	if un, ok := u.GetUsername(); ok && un != "" {
		return "@" + un
	}
	return fmt.Sprintf("user %d", u.ID)
}
