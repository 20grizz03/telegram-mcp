package tgclient

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"
)

func TestBuildMediaLocationPhoto(t *testing.T) {
	media := &tg.MessageMediaPhoto{Photo: &tg.Photo{
		ID:            111,
		AccessHash:    222,
		FileReference: []byte{1, 2, 3},
		Sizes: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "m", Size: 1000},
			&tg.PhotoSize{Type: "x", Size: 5000},
		},
	}}

	loc, info, ext, err := buildMediaLocation(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pl, ok := loc.(*tg.InputPhotoFileLocation)
	if !ok {
		t.Fatalf("want *tg.InputPhotoFileLocation, got %T", loc)
	}
	if pl.ID != 111 || pl.AccessHash != 222 || string(pl.FileReference) != "\x01\x02\x03" || pl.ThumbSize != "x" {
		t.Fatalf("unexpected location: %+v", pl)
	}
	if ext != ".jpg" || info.Type != "photo" || info.Size != 5000 {
		t.Fatalf("unexpected info: ext=%q %+v", ext, info)
	}
}

func TestBuildMediaLocationDocument(t *testing.T) {
	media := &tg.MessageMediaDocument{Document: &tg.Document{
		ID:            333,
		AccessHash:    444,
		FileReference: []byte{9},
		MimeType:      "application/pdf",
		Size:          2048,
		Attributes:    []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: "report.pdf"}},
	}}

	loc, info, ext, err := buildMediaLocation(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dl, ok := loc.(*tg.InputDocumentFileLocation)
	if !ok {
		t.Fatalf("want *tg.InputDocumentFileLocation, got %T", loc)
	}
	if dl.ID != 333 || dl.AccessHash != 444 || string(dl.FileReference) != "\x09" {
		t.Fatalf("unexpected location: %+v", dl)
	}
	if ext != ".pdf" || info.Type != "document" || info.FileName != "report.pdf" || info.Size != 2048 {
		t.Fatalf("unexpected info: ext=%q %+v", ext, info)
	}
}

func TestBuildMediaLocationUnsupported(t *testing.T) {
	if _, _, _, err := buildMediaLocation(&tg.MessageMediaGeo{}); err == nil {
		t.Fatalf("expected error for unsupported media")
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{"a/b/c.txt", "c.txt"},
		{`a\b\c.txt`, "a_b_c.txt"},
		{"..", "file"},
		{"", "file"},
		{"  ", "file"},
	}
	for _, tt := range tests {
		if got := sanitizeFileName(tt.in); got != tt.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtFor(t *testing.T) {
	tests := []struct {
		name string
		mime string
		want string
	}{
		{"report.pdf", "", ".pdf"},
		{"photo", "image/jpeg", ".jpg"},
		{"", "application/pdf", ".pdf"},
		{"", "video/mp4", ".mp4"},
		{"", "totally/unknown-xyz", ".bin"},
		{"", "", ".bin"},
	}
	for _, tt := range tests {
		if got := extFor(tt.name, tt.mime); got != tt.want {
			t.Errorf("extFor(%q, %q) = %q, want %q", tt.name, tt.mime, got, tt.want)
		}
	}
}

func TestBuildSavePath(t *testing.T) {
	dir := t.TempDir()

	// Explicit filename is used and sanitized into the base dir.
	got, err := buildSavePath(dir, 42, 7, "report.pdf", ".pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(dir, "report.pdf"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Empty filename falls back to a tg_<chat>_<msg>.<ext> name.
	got, err = buildSavePath(dir, 42, 7, "", ".jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(dir, "tg_42_7.jpg"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A traversal filename is reduced to its base inside the dir.
	got, err = buildSavePath(dir, 1, 2, "../../etc/passwd", ".bin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(dir, "passwd"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// out_dir is created if missing.
	nested := filepath.Join(dir, "sub", "deep")
	if _, err := buildSavePath(nested, 1, 2, "x.txt", ".txt"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Fatalf("expected %q to be created as a dir, err=%v", nested, err)
	}

	// out_dir containing a .. segment is rejected (note: built without
	// filepath.Join, which would collapse the ".." before we see it).
	escape := dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "escape"
	if _, err := buildSavePath(escape, 1, 2, "x.txt", ".txt"); err == nil {
		t.Fatalf("expected error for out_dir with '..'")
	}
}
