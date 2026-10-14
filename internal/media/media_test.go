package media

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestIsPreviewBot(t *testing.T) {
	cases := map[string]bool{
		"WhatsApp/2.24.1.1":             true,
		"facebookexternalhit/1.1":       true,
		"TelegramBot (like TwitterBot)": true,
		"Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)": true,
		"Slackbot-LinkExpanding 1.0":                                        true,
		"Twitterbot/1.0":                                                    true,
		"LinkedInBot/1.0":                                                   true,
		"Mozilla/5.0 (compatible; Mastodon/4.0.0)":                          true,
		"Iframely/1.3.1":                                                    true,
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0":            false,
		"": false,
	}
	for ua, want := range cases {
		if got := IsPreviewBot(ua); got != want {
			t.Errorf("IsPreviewBot(%q) = %v, want %v", ua, got, want)
		}
	}
}

func TestKind(t *testing.T) {
	cases := map[string]string{
		"photo.jpg": "image", "photo.PNG": "image", "anim.gif": "image", "pic.webp": "image",
		"clip.mp4": "video", "clip.MOV": "video", "clip.webm": "video",
		"song.mp3": "", "doc.pdf": "", "notiz.txt": "", "no-ext": "",
	}
	for name, want := range cases {
		if got := Kind(name); got != want {
			t.Errorf("Kind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:             "0 B",
		1023:          "1023 B",
		1024:          "1.0 KB",
		1536:          "1.5 KB",
		1 << 20:       "1.0 MB",
		3 * (1 << 30): "3.0 GB",
		5 * (1 << 40): "5.0 TB",
	}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFileDescription(t *testing.T) {
	if desc := FileDescription("report.pdf", 2048, 0); desc != "PDF · 2.0 KB" {
		t.Errorf("FileDescription = %q", desc)
	}

	exp := time.Now().Add(48 * time.Hour).Unix()
	desc := FileDescription("report.pdf", 2048, exp)
	if !strings.HasSuffix(desc, "· expires in 1 days") && !strings.HasSuffix(desc, "· expires in 2 days") {
		t.Errorf("FileDescription with expiry = %q", desc)
	}

	if desc := FileDescription("no-extension", 100, 0); !strings.HasPrefix(desc, "FILE ·") {
		t.Errorf("FileDescription for extensionless file = %q", desc)
	}
}

func TestDirDescription(t *testing.T) {
	if empty := DirDescription(nil, 0); empty != "Empty folder" {
		t.Errorf("DirDescription(empty) = %q", empty)
	}

	files := []FileInfo{
		{Name: "a.txt", Size: 1024},
		{Name: "b.txt", Size: 1024},
		{Name: "sub", IsDir: true},
	}
	if desc := DirDescription(files, 0); desc != "2 files, 1 folder · 2.0 KB" {
		t.Errorf("DirDescription = %q", desc)
	}

	if single := DirDescription([]FileInfo{{Name: "a.txt", Size: 1}}, 0); single != "1 file, 0 folders · 1 B" {
		t.Errorf("DirDescription(one file) = %q", single)
	}
}

func TestIsText(t *testing.T) {
	for name, want := range map[string]bool{
		"a.txt": true, "a.md": true, "a.go": true, "a.json": true, "a.YML": true,
		"a.zip": false, "a.pdf": false, "a.docx": false, "noext": false, "a.png": false,
	} {
		if got := IsText(name); got != want {
			t.Errorf("IsText(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCanThumbnail(t *testing.T) {
	for name, want := range map[string]bool{
		"a.jpg": true, "a.JPEG": true, "a.png": true, "a.gif": true, "a.webp": true,
		"a.avif": false, "a.mp4": false, "a.pdf": false, "noext": false,
	} {
		if got := CanThumbnail(name); got != want {
			t.Errorf("CanThumbnail(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestMimeType(t *testing.T) {
	if got := MimeType("clip.mp4"); got != "video/mp4" {
		t.Errorf("MimeType(mp4) = %q", got)
	}
	if got := MimeType("blob.unknownext"); got != "application/octet-stream" {
		t.Errorf("MimeType(unknown) = %q", got)
	}
}

func TestThumbnailScalesTheLongEdgeAndFlattensOntoWhite(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2*ThumbnailMaxEdge, ThumbnailMaxEdge))
	got := Thumbnail(src, ThumbnailMaxEdge)
	if b := got.Bounds(); b.Dx() != ThumbnailMaxEdge || b.Dy() != ThumbnailMaxEdge/2 {
		t.Errorf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), ThumbnailMaxEdge, ThumbnailMaxEdge/2)
	}
	if c := color.RGBAModel.Convert(got.At(0, 0)).(color.RGBA); c != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("transparent pixel became %v, want white", c)
	}
}

func TestThumbnailKeepsSmallImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	src.Set(5, 5, color.NRGBA{200, 40, 40, 255})
	got := Thumbnail(src, ThumbnailMaxEdge)
	if b := got.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Errorf("size = %dx%d, want 40x30", b.Dx(), b.Dy())
	}
	if c := color.RGBAModel.Convert(got.At(5, 5)).(color.RGBA); c != (color.RGBA{200, 40, 40, 255}) {
		t.Errorf("pixel = %v", c)
	}
}
