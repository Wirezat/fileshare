package media

import (
	"fmt"
	"image"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

const (
	ThumbnailMaxEdge = 1200
	ThumbnailQuality = 82
)

type FileInfo struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

func ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// Thumbnail scales src to fit within maxEdge on its longest side and flattens it onto white.
func Thumbnail(src image.Image, maxEdge int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if w > maxEdge || h > maxEdge {
		scale := float64(maxEdge) / float64(max(w, h))
		nw = max(int(float64(w)*scale), 1)
		nh = max(int(float64(h)*scale), 1)
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	}
	return dst
}

// MimeType returns the MIME type for name's extension, or application/octet-stream.
func MimeType(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

var previewBotAgents = []string{
	"WhatsApp", "facebookexternalhit", "TelegramBot", "Discordbot",
	"Slackbot", "Twitterbot", "LinkedInBot", "Mastodon", "Iframely",
}

// IsPreviewBot reports whether userAgent belongs to a known link-preview crawler.
func IsPreviewBot(userAgent string) bool {
	for _, agent := range previewBotAgents {
		if strings.Contains(userAgent, agent) {
			return true
		}
	}
	return false
}

var byteUnits = []string{"B", "KB", "MB", "GB", "TB"}

// FormatBytes renders n the way the share page's own byte formatter does.
func FormatBytes(n int64) string {
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(byteUnits)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, byteUnits[i])
	}
	return fmt.Sprintf("%.1f %s", f, byteUnits[i])
}

func appendExpiry(desc string, expiration int64) string {
	if expiration == 0 {
		return desc
	}
	days := max(int(time.Until(time.Unix(expiration, 0)).Hours()/24), 0)
	return fmt.Sprintf("%s · expires in %d days", desc, days)
}

// FileDescription builds the link-preview description of a shared file; expiration 0 means none.
func FileDescription(name string, size, expiration int64) string {
	kind := strings.ToUpper(ext(name))
	if kind == "" {
		kind = "FILE"
	}
	return appendExpiry(fmt.Sprintf("%s · %s", kind, FormatBytes(size)), expiration)
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// DirDescription builds the link-preview description of a folder share; expiration 0 means none.
func DirDescription(files []FileInfo, expiration int64) string {
	var fileCount, dirCount int
	var totalSize int64
	for _, f := range files {
		if f.IsDir {
			dirCount++
		} else {
			fileCount++
			totalSize += f.Size
		}
	}
	if fileCount == 0 && dirCount == 0 {
		return appendExpiry("Empty folder", expiration)
	}
	desc := fmt.Sprintf("%s, %s · %s", countNoun(fileCount, "file"), countNoun(dirCount, "folder"), FormatBytes(totalSize))
	return appendExpiry(desc, expiration)
}
