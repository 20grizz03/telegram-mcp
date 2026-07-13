package tgclient

import "testing"

import "github.com/gotd/td/tg"

func TestMediaInfoFor(t *testing.T) {
	tests := []struct {
		name  string
		media tg.MessageMediaClass
		want  *MediaInfo
	}{
		{
			name:  "no media",
			media: nil,
			want:  nil,
		},
		{
			name: "photo picks largest size",
			media: &tg.MessageMediaPhoto{Photo: &tg.Photo{
				Sizes: []tg.PhotoSizeClass{
					&tg.PhotoStrippedSize{Type: "i"},
					&tg.PhotoSize{Type: "m", Size: 1000},
					&tg.PhotoSize{Type: "x", Size: 5000},
					&tg.PhotoSizeProgressive{Type: "y", Sizes: []int{100, 9000}},
				},
			}},
			want: &MediaInfo{Type: "photo", Size: 9000},
		},
		{
			name: "plain document",
			media: &tg.MessageMediaDocument{Document: &tg.Document{
				MimeType: "application/pdf",
				Size:     2048,
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeFilename{FileName: "report.pdf"},
				},
			}},
			want: &MediaInfo{Type: "document", FileName: "report.pdf", MimeType: "application/pdf", Size: 2048},
		},
		{
			name: "video document",
			media: &tg.MessageMediaDocument{Document: &tg.Document{
				MimeType: "video/mp4",
				Size:     4096,
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeFilename{FileName: "clip.mp4"},
					&tg.DocumentAttributeVideo{},
				},
			}},
			want: &MediaInfo{Type: "video", FileName: "clip.mp4", MimeType: "video/mp4", Size: 4096},
		},
		{
			name: "audio document",
			media: &tg.MessageMediaDocument{Document: &tg.Document{
				MimeType:   "audio/mpeg",
				Size:       512,
				Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{}},
			}},
			want: &MediaInfo{Type: "audio", MimeType: "audio/mpeg", Size: 512},
		},
		{
			name: "voice document",
			media: &tg.MessageMediaDocument{Document: &tg.Document{
				MimeType:   "audio/ogg",
				Size:       256,
				Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{Voice: true}},
			}},
			want: &MediaInfo{Type: "voice", MimeType: "audio/ogg", Size: 256},
		},
		{
			name: "sticker wins over video",
			media: &tg.MessageMediaDocument{Document: &tg.Document{
				MimeType: "video/webm",
				Size:     128,
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeVideo{},
					&tg.DocumentAttributeSticker{},
				},
			}},
			want: &MediaInfo{Type: "sticker", MimeType: "video/webm", Size: 128},
		},
		{
			name:  "geo is other",
			media: &tg.MessageMediaGeo{},
			want:  &MediaInfo{Type: "other"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mediaInfoFor(tt.media)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %+v, got nil", tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("want %+v, got %+v", *tt.want, *got)
			}
		})
	}
}

func TestMediaLabel(t *testing.T) {
	tests := []struct {
		name string
		info *MediaInfo
		want string
	}{
		{"nil", nil, ""},
		{"photo", &MediaInfo{Type: "photo"}, "[photo]"},
		{"document with name", &MediaInfo{Type: "document", FileName: "report.pdf"}, "[document: report.pdf]"},
		{"video with name", &MediaInfo{Type: "video", FileName: "clip.mp4"}, "[video: clip.mp4]"},
		{"voice", &MediaInfo{Type: "voice"}, "[voice]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mediaLabel(tt.info); got != tt.want {
				t.Fatalf("want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestLargestPhotoSize(t *testing.T) {
	thumb, size := largestPhotoSize([]tg.PhotoSizeClass{
		&tg.PhotoStrippedSize{Type: "i"},
		&tg.PhotoSize{Type: "m", Size: 1000},
		&tg.PhotoSize{Type: "x", Size: 5000},
		&tg.PhotoSizeProgressive{Type: "y", Sizes: []int{100, 9000}},
	})
	if thumb != "y" || size != 9000 {
		t.Fatalf("want (y, 9000), got (%q, %d)", thumb, size)
	}
}
