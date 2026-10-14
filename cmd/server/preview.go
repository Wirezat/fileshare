package main

import (
	"html/template"
	"net/http"
	"sync"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/media"
)

var previewHtmlPath = "./web/html/preview.html"

// previewWanted reports whether this request should get a link-preview page
// with Open Graph tags instead of the file itself.
func previewWanted(r *http.Request, ctx *requestContext) bool {
	return r.Method == http.MethodGet &&
		!ctx.fileInfo.IsDir() &&
		r.URL.Query().Get("dl") == "" &&
		media.IsPreviewBot(r.UserAgent())
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
		Description: media.FileDescription(name, ctx.fileInfo.Size(), ctx.fileData.Expiration),
		URL:         absoluteURL(r, r.URL.Path),
		RefreshURL:  dlPath,
	}

	switch media.Kind(name) {
	case "image":
		if media.CanThumbnail(name) {
			data.ImageURL = absoluteURL(r, r.URL.Path+"?preview=1")
			data.ImageType = "image/jpeg"
		}
	case "video":
		data.VideoURL = absoluteURL(r, dlPath)
		data.VideoType = media.MimeType(name)
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
