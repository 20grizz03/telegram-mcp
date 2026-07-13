package tgclient

import (
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

func TestPublishedMessageIDScheduled(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateMessageID{ID: 37, RandomID: 123},
		&tg.UpdateNewScheduledMessage{Message: &tg.Message{ID: 37}},
	}}

	got, err := publishedMessageID(updates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 37 {
		t.Fatalf("publishedMessageID = %d, want 37", got)
	}
}

func TestPublishedMessageIDImmediate(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 12}},
	}}

	got, err := publishedMessageID(updates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 12 {
		t.Fatalf("publishedMessageID = %d, want 12", got)
	}
}

func TestPublishedMessageIDReturnsSendError(t *testing.T) {
	want := errors.New("send failed")
	if _, err := publishedMessageID(nil, want); !errors.Is(err, want) {
		t.Fatalf("publishedMessageID error = %v, want %v", err, want)
	}
}
