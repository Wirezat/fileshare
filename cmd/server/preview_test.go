package main

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/draw"

	"github.com/Wirezat/fileshare/pkg/shared"
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
		req, _ := http.NewRequest(http.MethodGet, "/docs/x", nil)
		req.Header.Set("User-Agent", ua)
		if got := isPreviewBot(req); got != want {
			t.Errorf("isPreviewBot(%q) = %v, want %v", ua, got, want)
		}
	}
}

func TestMediaKind(t *testing.T) {
	cases := map[string]string{
		"photo.jpg": "image", "photo.PNG": "image", "anim.gif": "image", "pic.webp": "image",
		"clip.mp4": "video", "clip.MOV": "video", "clip.webm": "video",
		"song.mp3": "", "doc.pdf": "", "notiz.txt": "", "no-ext": "",
	}
	for name, want := range cases {
		if got := mediaKind(name); got != want {
			t.Errorf("mediaKind(%q) = %q, want %q", name, got, want)
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
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFileDescription(t *testing.T) {
	desc := fileDescription(shared.FileData{}, "report.pdf", 2048)
	if desc != "PDF · 2.0 KB" {
		t.Errorf("fileDescription = %q", desc)
	}

	exp := time.Now().Add(48 * time.Hour).Unix()
	desc = fileDescription(shared.FileData{Expiration: exp}, "report.pdf", 2048)
	if !strings.HasSuffix(desc, "· expires in 1 days") && !strings.HasSuffix(desc, "· expires in 2 days") {
		t.Errorf("fileDescription with expiry = %q", desc)
	}

	desc = fileDescription(shared.FileData{}, "no-extension", 100)
	if !strings.HasPrefix(desc, "FILE ·") {
		t.Errorf("fileDescription for extensionless file = %q", desc)
	}
}

func TestDirDescription(t *testing.T) {
	empty := dirDescription(nil, 0)
	if empty != "Empty folder" {
		t.Errorf("dirDescription(empty) = %q", empty)
	}

	files := []shared.FileInfo{
		{Name: "a.txt", Size: 1024},
		{Name: "b.txt", Size: 1024},
		{Name: "sub", IsDir: true},
	}
	desc := dirDescription(files, 0)
	if desc != "2 files, 1 folder · 2.0 KB" {
		t.Errorf("dirDescription = %q", desc)
	}

	single := dirDescription([]shared.FileInfo{{Name: "a.txt", Size: 1}}, 0)
	if single != "1 file, 0 folders · 1 B" {
		t.Errorf("dirDescription(one file) = %q", single)
	}
}

func TestPreviewPageForPlainFile(t *testing.T) {
	withOfficeShare(t, shared.FileData{}, "", "")

	rec := get(t, "/docs/notiz.txt", "User-Agent", "TelegramBot (like TwitterBot)")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want html", ct)
	}
	for _, want := range []string{
		`og:title" content="notiz.txt"`,
		`og:description" content="TXT · `,
		`og:url" content="http://example.com/docs/notiz.txt"`,
		`url=/docs/notiz.txt?dl=1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preview page lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "og:image") || strings.Contains(body, "og:video") {
		t.Error("plain text file got an og:image or og:video tag")
	}
	if strings.Contains(body, "plain") {
		t.Error("preview page leaked the file's contents")
	}
}

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{200, 40, 40, 255}}, image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewPageForImage(t *testing.T) {
	dir := withOfficeShare(t, shared.FileData{}, "", "")
	writeTestPNG(t, filepath.Join(dir, "photo.jpg"), 40, 30)

	rec := get(t, "/docs/photo.jpg", "User-Agent", "facebookexternalhit/1.1")
	body := rec.Body.String()
	for _, want := range []string{
		`og:image" content="http://example.com/docs/photo.jpg?preview=1"`,
		`og:image:type" content="image/jpeg"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("image preview lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "og:video") {
		t.Error("image file got an og:video tag")
	}
}

func TestPreviewImageThumbnail(t *testing.T) {
	dir := withOfficeShare(t, shared.FileData{}, "", "")
	writeTestPNG(t, filepath.Join(dir, "big.png"), 3000, 200)
	writeTestPNG(t, filepath.Join(dir, "small.png"), 40, 30)

	rec := get(t, "/docs/big.png?preview=1", "User-Agent", "facebookexternalhit/1.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	cfg, format, err := image.DecodeConfig(rec.Body)
	if err != nil {
		t.Fatalf("thumbnail did not decode as an image: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if cfg.Width > thumbnailMaxEdge || cfg.Height > thumbnailMaxEdge {
		t.Errorf("thumbnail is %dx%d, want capped at %d", cfg.Width, cfg.Height, thumbnailMaxEdge)
	}
	if cfg.Width != thumbnailMaxEdge {
		t.Errorf("thumbnail width = %d, want the long edge scaled to %d", cfg.Width, thumbnailMaxEdge)
	}

	rec = get(t, "/docs/small.png?preview=1", "User-Agent", "facebookexternalhit/1.1")
	cfg, _, err = image.DecodeConfig(rec.Body)
	if err != nil {
		t.Fatalf("small thumbnail did not decode: %v", err)
	}
	if cfg.Width != 40 || cfg.Height != 30 {
		t.Errorf("small image got resized to %dx%d, want unchanged 40x30", cfg.Width, cfg.Height)
	}
}

func TestPreviewSkipsOgImageForUnthumbnailableFormats(t *testing.T) {
	dir := withOfficeShare(t, shared.FileData{}, "", "")
	os.WriteFile(filepath.Join(dir, "photo.avif"), []byte("not a real avif but irrelevant here"), 0o600)

	rec := get(t, "/docs/photo.avif", "User-Agent", "facebookexternalhit/1.1")
	body := rec.Body.String()
	if strings.Contains(body, "og:image") {
		t.Errorf("avif file got an og:image tag we can't actually serve: %s", body)
	}
	if !strings.Contains(body, `og:description" content="AVIF ·`) {
		t.Errorf("avif file lacks a text description: %s", body)
	}
}

func TestPreviewPageForVideo(t *testing.T) {
	dir := withOfficeShare(t, shared.FileData{}, "", "")
	os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake-mp4-bytes"), 0o600)

	rec := get(t, "/docs/clip.mp4", "User-Agent", "Discordbot/2.0")
	body := rec.Body.String()
	for _, want := range []string{
		`og:video" content="http://example.com/docs/clip.mp4?dl=1"`,
		`og:video:secure_url" content="http://example.com/docs/clip.mp4?dl=1"`,
		`og:video:type"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("video preview lacks %s: %s", want, body)
		}
	}
}

func TestPreviewDoesNotApplyToDl1OrRealBrowsers(t *testing.T) {
	withOfficeShare(t, shared.FileData{}, "", "")

	rec := get(t, "/docs/notiz.txt?dl=1", "User-Agent", "TelegramBot (like TwitterBot)")
	if rec.Body.String() != "plain" {
		t.Errorf("?dl=1 with a bot UA got %q, want the raw file", rec.Body.String())
	}

	rec = get(t, "/docs/notiz.txt", "User-Agent", "Mozilla/5.0 Chrome/120.0")
	if rec.Body.String() != "plain" {
		t.Errorf("a real browser got %q, want the raw file", rec.Body.String())
	}
}

func TestPreviewDoesNotApplyToFolders(t *testing.T) {
	withOfficeShare(t, shared.FileData{}, "", "")

	rec := get(t, "/docs", "User-Agent", "TelegramBot (like TwitterBot)")
	body := rec.Body.String()
	if !strings.Contains(body, `class="file-grid"`) {
		t.Errorf("a folder request did not render the directory listing: %s", body)
	}
	if strings.Contains(body, `http-equiv="refresh"`) {
		t.Error("a folder request rendered the file-preview page")
	}
	if !strings.Contains(body, `og:description" content="2 files, 0 folders`) {
		t.Errorf("folder page lacks an og:description built from its contents: %s", body)
	}
}

func TestGatePageDescriptionDoesNotLeakTheShare(t *testing.T) {
	hash, _ := shared.HashPassword("geheim")
	withOfficeShare(t, shared.FileData{Password: hash}, "", "")

	rec := get(t, "/docs/notiz.txt")
	body := rec.Body.String()
	if !strings.Contains(body, `og:description" content="Password protected share"`) {
		t.Errorf("gate page lacks the fixed og:description: %s", body)
	}
	if strings.Contains(body, "notiz.txt") {
		t.Error("gate page leaked the requested file name")
	}
}

func TestExpiredSharePreview(t *testing.T) {
	withOfficeShare(t, shared.FileData{Expired: true}, "", "")

	rec := get(t, "/docs/notiz.txt", "User-Agent", "TelegramBot (like TwitterBot)")
	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want html for a bot", ct)
	}
	if !strings.Contains(rec.Body.String(), "This share has expired") {
		t.Errorf("expired preview lacks the description: %s", rec.Body.String())
	}

	rec = get(t, "/docs/notiz.txt", "User-Agent", "Mozilla/5.0 Chrome/120.0")
	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		t.Error("a real browser got the html preview page instead of the plain error")
	}
}
