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

func TestScheduledWriteValidation(t *testing.T) {
	c := &Client{}
	if _, err := c.EditScheduledPost(t.Context(), 1, 0, "text"); err == nil {
		t.Fatal("EditScheduledPost with zero msg_id must fail")
	}
	if _, err := c.EditScheduledPost(t.Context(), 1, 1, ""); err == nil {
		t.Fatal("EditScheduledPost with empty text must fail")
	}
	if err := c.DeleteScheduledPost(t.Context(), 1, 0); err == nil {
		t.Fatal("DeleteScheduledPost with zero msg_id must fail")
	}
}

func TestProfileBioRequestCanClearBio(t *testing.T) {
	req := profileBioRequest("")
	if bio, ok := req.GetAbout(); !ok || bio != "" {
		t.Fatalf("GetAbout() = %q, %v; want empty value explicitly set", bio, ok)
	}
}

func TestValidateRichSource(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		html     string
		photos   []RichPhoto
		wantErr  bool
	}{
		{name: "markdown", markdown: "# Title"},
		{name: "html", html: "<h1>Title</h1>"},
		{name: "empty", wantErr: true},
		{name: "both", markdown: "# Title", html: "<h1>Title</h1>", wantErr: true},
		{name: "valid photo", markdown: "![](tg://photo?id=cover)", photos: []RichPhoto{{ID: "cover", Path: "/tmp/cover.png"}}},
		{name: "invalid photo id", markdown: "x", photos: []RichPhoto{{ID: "bad id", Path: "/tmp/a.png"}}, wantErr: true},
		{name: "missing photo path", markdown: "x", photos: []RichPhoto{{ID: "cover"}}, wantErr: true},
		{name: "duplicate photo id", markdown: "x", photos: []RichPhoto{{ID: "cover", Path: "/tmp/a.png"}, {ID: "cover", Path: "/tmp/b.png"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRichSource(tt.markdown, tt.html, tt.photos)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRichSource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInputPhotoFromMedia(t *testing.T) {
	want := &tg.Photo{ID: 7, AccessHash: 8, FileReference: []byte{9}}
	got, err := inputPhotoFromMedia(&tg.MessageMediaPhoto{Photo: want})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.AccessHash != want.AccessHash {
		t.Fatalf("inputPhotoFromMedia() = %+v, want id/access hash from %+v", got, want)
	}

	if _, err := inputPhotoFromMedia(&tg.MessageMediaGeo{}); err == nil {
		t.Fatal("inputPhotoFromMedia() expected error for non-photo media")
	}
}
