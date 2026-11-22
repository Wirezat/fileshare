package web

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/Wirezat/fileshare/internal/cover"
	"github.com/Wirezat/fileshare/internal/filetype"
	"github.com/Wirezat/fileshare/internal/media"
)

const (
	firstLinesRead = 4096
	firstLinesMax  = 20
	previewMinSize = 64
)

// previewSize returns the requested long edge of a preview image for a file shown with view, clamped to 64…1200,
// or 64…2400 for images the browser cannot show itself; missing or invalid gives the upper bound.
func previewSize(r *http.Request, view string) int {
	limit := media.ThumbnailMaxEdge
	if view == filetype.ViewConverted {
		limit = media.ConvertedMaxEdge
	}
	n, err := strconv.Atoi(r.URL.Query().Get("size"))
	if err != nil {
		return limit
	}
	return min(max(n, previewMinSize), limit)
}

// servePreviewVariant answers ?preview: a JPEG for images and audio covers, the first lines for text,
// 204 when there is nothing to show and 404 for views without a preview.
func (s *Server) servePreviewVariant(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	view := fileView(ctx)
	switch view {
	case filetype.ViewImage, filetype.ViewConverted, filetype.ViewAudio, filetype.ViewText, filetype.ViewMarkdown, filetype.ViewHTML, filetype.ViewProbe:
	default:
		http.NotFound(w, r)
		return
	}
	if notModified(w, r, ctx.fileInfo) {
		return
	}
	f, err := os.Open(ctx.diskPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	switch view {
	case filetype.ViewImage, filetype.ViewConverted:
		if !filetype.Lookup(typedName(ctx)).Thumbnail {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJPEG(w, r, f, previewSize(r, view))
	case filetype.ViewAudio:
		pic, ok := cover.Find(f, typedName(ctx))
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJPEG(w, r, bytes.NewReader(pic), previewSize(r, view))
	default:
		writeFirstLines(w, f)
	}
}

// writeJPEG answers with src scaled to a long edge of size, or 204 when it cannot or may not be decoded.
func writeJPEG(w http.ResponseWriter, r *http.Request, src io.ReadSeeker, size int) {
	out, err := media.EncodeThumbnail(r.Context(), src, size)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(out)
}

// writeFirstLines answers with up to 20 lines from the start of f as plain text, or 204 when f looks binary.
func writeFirstLines(w http.ResponseWriter, f io.Reader) {
	head, err := io.ReadAll(io.LimitReader(f, firstLinesRead))
	if err != nil || !filetype.Sniff(head) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	end := 0
	for range firstLinesMax {
		i := bytes.IndexByte(head[end:], '\n')
		if i < 0 {
			end = len(head)
			break
		}
		end += i + 1
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(filetype.TrimIncompleteRune(bytes.TrimSuffix(head[:end], []byte("\n"))))
}
