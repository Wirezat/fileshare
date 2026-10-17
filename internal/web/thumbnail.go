package web

import (
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/media"
	_ "golang.org/x/image/webp"
)

// wantsPreviewImage reports whether this request is for a thumbnailed image.
func wantsPreviewImage(r *http.Request, ctx *requestContext) bool {
	return r.URL.Query().Get("preview") == "1" && media.CanThumbnail(ctx.fileInfo.Name())
}

// servePreviewImage answers with the shared image as a JPEG thumbnail.
func servePreviewImage(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	f, err := os.Open(ctx.diskPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		GoLog.Errorf("thumbnail decode %s: %v", ctx.diskPath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	thumb := media.Thumbnail(src, media.ThumbnailMaxEdge)
	if err := jpeg.Encode(w, thumb, &jpeg.Options{Quality: media.ThumbnailQuality}); err != nil {
		GoLog.Errorf("thumbnail encode %s: %v", ctx.diskPath, err)
	}
}
