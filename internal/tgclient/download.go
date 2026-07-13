package tgclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// maxDownloadBytes caps how large an attachment download_media will fetch.
const maxDownloadBytes int64 = 50 << 20 // 50 MiB

// DownloadMedia downloads the media attached to message msgID in chat chatID
// into outDir (a temp dir when empty) and returns the saved path plus the
// attachment's metadata. It is read-only and never logs access hashes or file
// references.
func (c *Client) DownloadMedia(ctx context.Context, chatID int64, msgID int, outDir string) (string, MediaInfo, error) {
	info, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return "", MediaInfo{}, err
	}

	msg, err := c.fetchMessage(ctx, info, msgID)
	if err != nil {
		return "", MediaInfo{}, err
	}
	if msg.Media == nil {
		return "", MediaInfo{}, fmt.Errorf("no media in message %d", msgID)
	}

	loc, media, ext, err := buildMediaLocation(msg.Media)
	if err != nil {
		return "", MediaInfo{}, err
	}
	if media.Size > maxDownloadBytes {
		return "", MediaInfo{}, fmt.Errorf("media is %d bytes, exceeds the %d byte download limit", media.Size, maxDownloadBytes)
	}

	savedPath, err := buildSavePath(outDir, chatID, msgID, media.FileName, ext)
	if err != nil {
		return "", MediaInfo{}, err
	}

	if err := c.downloadTo(ctx, loc, savedPath); err != nil {
		// File references are short-lived: on expiry, re-fetch the message to get
		// a fresh reference and retry the download exactly once.
		if !tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
			return "", MediaInfo{}, fmt.Errorf("download media: %w", err)
		}
		msg, err = c.fetchMessage(ctx, info, msgID)
		if err != nil {
			return "", MediaInfo{}, err
		}
		if msg.Media == nil {
			return "", MediaInfo{}, fmt.Errorf("no media in message %d", msgID)
		}
		loc, media, _, err = buildMediaLocation(msg.Media)
		if err != nil {
			return "", MediaInfo{}, err
		}
		if err := c.downloadTo(ctx, loc, savedPath); err != nil {
			return "", MediaInfo{}, fmt.Errorf("download media after refresh: %w", err)
		}
	}

	return savedPath, media, nil
}

// fetchMessage retrieves a single message by id, using the channel-specific RPC
// for channels/supergroups and the generic one otherwise.
func (c *Client) fetchMessage(ctx context.Context, info peerInfo, msgID int) (*tg.Message, error) {
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}

	var (
		resp tg.MessagesMessagesClass
		err  error
	)
	if info.kind == KindChannel {
		ch, ok := info.input.(*tg.InputPeerChannel)
		if !ok {
			return nil, fmt.Errorf("cannot address channel for chat")
		}
		resp, err = c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      ids,
		})
	} else {
		resp, err = c.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, fmt.Errorf("get message %d: %w", msgID, err)
	}

	msgs, _, _ := extractMessages(resp)
	for _, mc := range msgs {
		if m, ok := mc.(*tg.Message); ok && m.ID == msgID {
			return m, nil
		}
	}
	return nil, fmt.Errorf("message %d not found", msgID)
}

// downloadTo streams a single file location to path on disk.
func (c *Client) downloadTo(ctx context.Context, loc tg.InputFileLocationClass, path string) error {
	_, err := downloader.NewDownloader().Download(c.api, loc).ToPath(ctx, path)
	return err
}

// buildMediaLocation builds the download location for a message's media along
// with its metadata and a file extension suggestion.
func buildMediaLocation(media tg.MessageMediaClass) (tg.InputFileLocationClass, MediaInfo, string, error) {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := m.Photo.(*tg.Photo)
		if !ok {
			return nil, MediaInfo{}, "", fmt.Errorf("photo is unavailable")
		}
		thumb, size := largestPhotoSize(photo.Sizes)
		loc := &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumb,
		}
		return loc, MediaInfo{Type: "photo", Size: size}, ".jpg", nil

	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return nil, MediaInfo{}, "", fmt.Errorf("document is unavailable")
		}
		info := documentMediaInfo(doc)
		loc := &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}
		return loc, info, extFor(info.FileName, info.MimeType), nil

	default:
		return nil, MediaInfo{}, "", fmt.Errorf("message media of type %T cannot be downloaded", media)
	}
}

// buildSavePath resolves the on-disk path to write to, creating the base
// directory and refusing to escape it.
func buildSavePath(outDir string, chatID int64, msgID int, fileName, ext string) (string, error) {
	base := outDir
	if base == "" {
		base = os.TempDir()
	} else if hasDotDot(base) {
		// Reject any caller-supplied parent reference before Clean can resolve it.
		return "", fmt.Errorf("out_dir must not contain '..'")
	}
	base = filepath.Clean(base)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("create out dir: %w", err)
	}

	name := fileName
	if name == "" {
		name = fmt.Sprintf("tg_%d_%d%s", chatID, msgID, ext)
	}
	name = sanitizeFileName(name)

	path := filepath.Join(base, name)
	// Defense in depth: the sanitized name must resolve inside base.
	if rel, err := filepath.Rel(base, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to write outside %s", base)
	}
	return path, nil
}

// sanitizeFileName reduces an arbitrary name to a safe single path component.
func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, `\`, "_")
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// extFor picks a file extension from a name, falling back to common MIME types
// and finally ".bin".
func extFor(name, mimeType string) string {
	if e := filepath.Ext(name); e != "" {
		return e
	}
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	}
	return ".bin"
}

// hasDotDot reports whether any path segment is a parent-directory reference.
func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, string(os.PathSeparator)) {
		if seg == ".." {
			return true
		}
	}
	return false
}
