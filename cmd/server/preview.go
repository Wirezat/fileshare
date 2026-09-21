package main

import (
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

var previewHtmlPath = "./web/html/preview.html"

// previewBotAgents are User-Agent substrings of known link-preview crawlers.
// Signal, Threema and iMessage present themselves as WhatsApp or
// facebookexternalhit and need no entry of their own.
var previewBotAgents = []string{
	"WhatsApp", "facebookexternalhit", "TelegramBot", "Discordbot",
	"Slackbot", "Twitterbot", "LinkedInBot", "Mastodon", "Iframely",
}

// isPreviewBot reports whether the request's User-Agent belongs to a known
// link-preview crawler.
func isPreviewBot(r *http.Request) bool {
	ua := r.Header.Get("User-Agent")
	for _, agent := range previewBotAgents {
		if strings.Contains(ua, agent) {
			return true
		}
	}
	return false
}

var mediaKinds = map[string]string{
	"jpg": "image", "jpeg": "image", "png": "image", "gif": "image", "webp": "image", "avif": "image",
	"mp4": "video", "mov": "video", "webm": "video", "mkv": "video", "avi": "video", "wmv": "video",
}

// mediaKind reports whether name is an image, a video, or neither.
func mediaKind(name string) string {
	return mediaKinds[officeExt(name)]
}

// previewWanted reports whether this request should get a link-preview page
// with Open Graph tags instead of the file itself.
func previewWanted(r *http.Request, ctx *requestContext) bool {
	return r.Method == http.MethodGet &&
		!ctx.fileInfo.IsDir() &&
		r.URL.Query().Get("dl") == "" &&
		isPreviewBot(r)
}

var byteUnits = []string{"B", "KB", "MB", "GB", "TB"}

// formatBytes renders n the way the share page's own byte formatter does.
func formatBytes(n int64) string {
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

// appendExpiry adds an "expires in N days" clause to desc, unless exp is 0.
func appendExpiry(desc string, exp int64) string {
	if exp == 0 {
		return desc
	}
	days := max(int(time.Until(time.Unix(exp, 0)).Hours()/24), 0)
	return fmt.Sprintf("%s · expires in %d days", desc, days)
}

// fileDescription builds the og:description for a single shared file.
func fileDescription(fd shared.FileData, name string, size int64) string {
	kind := strings.ToUpper(officeExt(name))
	if kind == "" {
		kind = "FILE"
	}
	return appendExpiry(fmt.Sprintf("%s · %s", kind, formatBytes(size)), fd.Expiration)
}

// countNoun renders n with noun in the correct English plural.
func countNoun(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// dirDescription builds the og:description for a folder share.
func dirDescription(files []shared.FileInfo, expiration int64) string {
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
	desc := fmt.Sprintf("%s, %s · %s", countNoun(fileCount, "file"), countNoun(dirCount, "folder"), formatBytes(totalSize))
	return appendExpiry(desc, expiration)
}

// previewData is the template context for the link-preview page.
type previewData struct {
	Title       string
	Description string
	URL         string
	RefreshURL  string
	ImageURL    string
	ImageType   string
	VideoURL    string
	VideoType   string
}

// absoluteURL resolves path against the request's scheme and host.
func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

var (
	previewTemplateOnce sync.Once
	previewTemplate     *template.Template
	previewTemplateErr  error
)

func loadPreviewTemplate() (*template.Template, error) {
	previewTemplateOnce.Do(func() {
		previewTemplate, previewTemplateErr = template.New("preview").ParseFiles(previewHtmlPath)
		if previewTemplateErr != nil {
			GoLog.Errorf("failed to parse preview template: %v", previewTemplateErr)
		}
	})
	return previewTemplate, previewTemplateErr
}

// servePreview renders an Open Graph preview page for a bot hitting a shared
// file directly, instead of handing it the file's raw bytes.
func servePreview(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	tmpl, err := loadPreviewTemplate()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	name := ctx.fileInfo.Name()
	dlPath := r.URL.Path + "?dl=1"
	data := previewData{
		Title:       name,
		Description: fileDescription(ctx.fileData, name, ctx.fileInfo.Size()),
		URL:         absoluteURL(r, r.URL.Path),
		RefreshURL:  dlPath,
	}

	switch mediaKind(name) {
	case "image":
		if canThumbnail(name) {
			data.ImageURL = absoluteURL(r, r.URL.Path+"?preview=1")
			data.ImageType = "image/jpeg"
		}
	case "video":
		data.VideoURL = absoluteURL(r, dlPath)
		data.VideoType = mimeType(name)
	}

	renderPreview(w, http.StatusOK, tmpl, data)
}

// serveExpiredPreview renders the same page for a bot hitting an expired share.
func serveExpiredPreview(w http.ResponseWriter, r *http.Request, subpath string) {
	tmpl, err := loadPreviewTemplate()
	if err != nil {
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}
	renderPreview(w, http.StatusGone, tmpl, previewData{
		Title:       "/" + subpath,
		Description: "This share has expired",
		URL:         absoluteURL(r, r.URL.Path),
	})
}

func renderPreview(w http.ResponseWriter, status int, tmpl *template.Template, data previewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "preview", data); err != nil {
		GoLog.Errorf("failed to render preview template: %v", err)
	}
}

func mimeType(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}
