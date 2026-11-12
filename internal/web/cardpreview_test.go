package web

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func previewEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	return withOfficeShare(t, config.FileData{}, "", "")
}

func jpegSize(t *testing.T, body []byte) (int, int) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || format != "jpeg" {
		t.Fatalf("body is not a jpeg: %s %v", format, err)
	}
	return cfg.Width, cfg.Height
}

func pngHeaderOnly(w, h int) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, uint32(w))
	binary.Write(&ihdr, binary.BigEndian, uint32(h))
	ihdr.Write([]byte{8, 2, 0, 0, 0})
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4))
	out.Write(ihdr.Bytes())
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

// mp3WithCover returns an ID3v2.3 tag holding pic as front cover, followed by fake audio.
func mp3WithCover(pic []byte) []byte {
	body := append([]byte{0}, "image/png\x00"...)
	body = append(body, 3, 0)
	body = append(body, pic...)
	frame := binary.BigEndian.AppendUint32([]byte("APIC"), uint32(len(body)))
	frame = append(frame, 0, 0)
	frame = append(frame, body...)
	n := len(frame)
	tag := append([]byte("ID3"), 3, 0, 0, byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f))
	return append(append(tag, frame...), "audio"...)
}

func TestCardPreviewScalesToSize(t *testing.T) {
	e, dir := previewEnv(t)
	writeTestPNG(t, filepath.Join(dir, "wide.png"), 400, 200)

	rec := get(t, e, "/docs/wide.png?preview=1&size=100")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if w, h := jpegSize(t, rec.Body.Bytes()); w != 100 || h != 50 {
		t.Errorf("preview is %dx%d, want 100x50", w, h)
	}
}

func TestCardPreviewSizeIsClamped(t *testing.T) {
	e, dir := previewEnv(t)
	writeTestPNG(t, filepath.Join(dir, "square.png"), 300, 300)
	writeTestPNG(t, filepath.Join(dir, "large.png"), 2000, 1000)

	if w, h := jpegSize(t, get(t, e, "/docs/square.png?preview=1&size=10").Body.Bytes()); w != 64 || h != 64 {
		t.Errorf("size=10 gave %dx%d, want 64x64", w, h)
	}
	if w, h := jpegSize(t, get(t, e, "/docs/large.png?preview=1&size=5000").Body.Bytes()); w != 1200 || h != 600 {
		t.Errorf("size=5000 gave %dx%d, want 1200x600", w, h)
	}
}

func TestCardPreviewOver4KIsNoContent(t *testing.T) {
	e, dir := previewEnv(t)
	os.WriteFile(filepath.Join(dir, "phone.png"), pngHeaderOnly(4000, 3000), 0o600)

	rec := get(t, e, "/docs/phone.png?preview=1&size=300")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d, %d bytes; want 204 without body", rec.Code, rec.Body.Len())
	}
}

func TestCardPreviewAvifIsNoContent(t *testing.T) {
	e, dir := previewEnv(t)
	os.WriteFile(filepath.Join(dir, "photo.avif"), []byte("avif bytes"), 0o600)

	if rec := get(t, e, "/docs/photo.avif?preview=1"); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
}

func TestCardPreviewAudioCover(t *testing.T) {
	e, dir := previewEnv(t)
	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 50, 50)))
	os.WriteFile(filepath.Join(dir, "song.mp3"), mp3WithCover(pic.Bytes()), 0o600)
	os.WriteFile(filepath.Join(dir, "plain.mp3"), []byte("\xff\xfb\x90\x00 frames"), 0o600)

	rec := get(t, e, "/docs/song.mp3?preview=1&size=300")
	if rec.Code != http.StatusOK {
		t.Fatalf("cover: status %d", rec.Code)
	}
	if w, h := jpegSize(t, rec.Body.Bytes()); w != 50 || h != 50 {
		t.Errorf("cover is %dx%d, want 50x50", w, h)
	}
	if rec := get(t, e, "/docs/plain.mp3?preview=1"); rec.Code != http.StatusNoContent {
		t.Errorf("mp3 without cover: status %d, want 204", rec.Code)
	}
}

func TestCardPreviewFirstLines(t *testing.T) {
	e, dir := previewEnv(t)
	var lines, want []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, fmt.Sprintf("line%02d", i))
	}
	want = lines[:20]
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	rec := get(t, e, "/docs/notes.txt?preview=1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Body.String(); got != strings.Join(want, "\n") {
		t.Errorf("body = %q, want the first 20 lines", got)
	}
}

func TestCardPreviewBinaryIsNoContent(t *testing.T) {
	e, dir := previewEnv(t)
	os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x01\x02"), 0o600)

	if rec := get(t, e, "/docs/blob.bin?preview=1"); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
}

func TestCardPreviewOtherViewsAreNotFound(t *testing.T) {
	e, dir := previewEnv(t)
	for _, name := range []string{"a.pdf", "a.zip", "a.mp4"} {
		os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o600)
		if rec := get(t, e, "/docs/"+name+"?preview=1"); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
	}
}

func TestPreviewRevalidates(t *testing.T) {
	e, _ := previewEnv(t)

	first := get(t, e, "/docs/notiz.txt?preview=1")
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("ETag %q, Cache-Control %q", etag, first.Header().Get("Cache-Control"))
	}
	if rec := get(t, e, "/docs/notiz.txt?preview=1", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("revalidation: status %d, want 304", rec.Code)
	}
}
