package tgclient

import (
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestExportedInvite(t *testing.T) {
	expires := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	raw := &tg.ChatInviteExported{
		Link:          "https://t.me/+example",
		Date:          int(time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC).Unix()),
		RequestNeeded: true,
	}
	raw.SetTitle("partner-a")
	raw.SetExpireDate(int(expires.Unix()))
	raw.SetUsageLimit(100)
	raw.SetUsage(12)
	raw.SetRequested(3)

	got, ok := exportedInvite(raw)
	if !ok {
		t.Fatal("exportedInvite rejected ChatInviteExported")
	}
	if got.Title != "partner-a" || got.Joined != 12 || got.Requested != 3 || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("invite = %+v", got)
	}
	if _, ok := exportedInvite(&tg.ChatInvitePublicJoinRequests{}); ok {
		t.Fatal("public join requests must not be treated as an attributed invite")
	}
}

func TestChannelFolderHelpers(t *testing.T) {
	filters := []tg.DialogFilterClass{
		&tg.DialogFilter{ID: 2},
		&tg.DialogFilterChatlist{ID: 4},
	}
	id, err := nextFolderID(filters)
	if err != nil {
		t.Fatal(err)
	}
	if id != 3 {
		t.Fatalf("next folder id = %d, want 3", id)
	}

	peers := []tg.InputPeerClass{
		&tg.InputPeerChannel{ChannelID: 11},
		&tg.InputPeerChat{ChatID: 22},
		&tg.InputPeerUser{UserID: 33},
	}
	ids := inputPeerIDs(peers)
	if len(ids) != 3 || ids[0] != 11 || ids[1] != 22 || ids[2] != 33 {
		t.Fatalf("peer ids = %v", ids)
	}
}
