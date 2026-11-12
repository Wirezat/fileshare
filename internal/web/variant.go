package web

import (
	"fmt"
	"net/http"
	"os"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/filetype"
)

// officeOn reports whether the request's share has the office suite switched on and configured.
func officeOn(ctx *requestContext) bool {
	return ctx.fileData.Office != config.OfficeOff && ctx.config.OfficeReady()
}

// fileView returns the filetype view of the requested file in its share.
func fileView(ctx *requestContext) string {
	return filetype.ViewFor(ctx.fileInfo.Name(), officeOn(ctx))
}

// wantsPreview reports whether the request carries a non-empty preview parameter.
func wantsPreview(r *http.Request) bool {
	return r.URL.Query().Get("preview") != ""
}

// wantsView reports whether the request carries a non-empty view parameter.
func wantsView(r *http.Request) bool {
	return r.URL.Query().Get("view") != ""
}

// wantsVariant reports whether the request asks for a preview or view of the file instead of the file itself.
func wantsVariant(r *http.Request) bool {
	return wantsPreview(r) || wantsView(r)
}

// notModified sets the ETag and Cache-Control of a preview or view of info's file and answers 304 when the
// client already holds that revision; true means the response is done.
func notModified(w http.ResponseWriter, r *http.Request, info os.FileInfo) bool {
	etag := fmt.Sprintf(`"%x-%x"`, info.Size(), info.ModTime().UnixNano())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// serveViewVariant answers ?view=text, source, frame and office; any other value gets 404.
func (s *Server) serveViewVariant(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	switch r.URL.Query().Get("view") {
	case "text":
		serveTextView(w, r, ctx)
	case "source":
		serveSourceView(w, r, ctx)
	case "frame":
		serveFrame(w, r, ctx)
	case "office":
		if fileView(ctx) != filetype.ViewOffice {
			http.NotFound(w, r)
			return
		}
		s.serveOfficeViewer(w, r, ctx, true)
	default:
		http.NotFound(w, r)
	}
}
