package tgclient

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// mediaInfoFor inspects a message's media and returns its metadata, or nil when
// the message carries no media. It never returns the file bytes.
func mediaInfoFor(media tg.MessageMediaClass) *MediaInfo {
	switch m := media.(type) {
	case nil:
		return nil
	case *tg.MessageMediaPhoto:
		info := MediaInfo{Type: "photo"}
		if photo, ok := m.Photo.(*tg.Photo); ok {
			_, info.Size = largestPhotoSize(photo.Sizes)
		}
		return &info
	case *tg.MessageMediaDocument:
		if doc, ok := m.Document.(*tg.Document); ok {
			info := documentMediaInfo(doc)
			return &info
		}
		return &MediaInfo{Type: "document"}
	default:
		return &MediaInfo{Type: "other"}
	}
}

// documentMediaInfo derives metadata from a document, classifying it as
// video/audio/voice/sticker when the corresponding attribute is present.
func documentMediaInfo(doc *tg.Document) MediaInfo {
	info := MediaInfo{Type: "document", MimeType: doc.MimeType, Size: doc.Size}

	var isVideo, isAudio, isVoice, isSticker bool
	for _, attr := range doc.Attributes {
		switch a := attr.(type) {
		case *tg.DocumentAttributeFilename:
			info.FileName = a.FileName
		case *tg.DocumentAttributeVideo:
			isVideo = true
		case *tg.DocumentAttributeAudio:
			isAudio = true
			isVoice = a.Voice
		case *tg.DocumentAttributeSticker:
			isSticker = true
		}
	}

	switch {
	case isSticker:
		info.Type = "sticker"
	case isVoice:
		info.Type = "voice"
	case isAudio:
		info.Type = "audio"
	case isVideo:
		info.Type = "video"
	}
	return info
}

// mediaLabel renders a short placeholder ("[photo]", "[document: report.pdf]")
// used as the text of a message that has only an attachment and no caption.
func mediaLabel(info *MediaInfo) string {
	if info == nil {
		return ""
	}
	if info.FileName != "" {
		return fmt.Sprintf("[%s: %s]", info.Type, info.FileName)
	}
	return fmt.Sprintf("[%s]", info.Type)
}

// largestPhotoSize returns the type tag and byte size of the biggest downloadable
// variant of a photo, skipping the tiny stripped/empty placeholders.
func largestPhotoSize(sizes []tg.PhotoSizeClass) (string, int64) {
	var (
		bestType string
		bestSize int64
	)
	consider := func(typ string, size int64) {
		if size >= bestSize {
			bestSize, bestType = size, typ
		}
	}
	for _, s := range sizes {
		switch ps := s.(type) {
		case *tg.PhotoSize:
			consider(ps.Type, int64(ps.Size))
		case *tg.PhotoCachedSize:
			consider(ps.Type, int64(len(ps.Bytes)))
		case *tg.PhotoSizeProgressive:
			var max int
			for _, n := range ps.Sizes {
				if n > max {
					max = n
				}
			}
			consider(ps.Type, int64(max))
		}
	}
	return bestType, bestSize
}
