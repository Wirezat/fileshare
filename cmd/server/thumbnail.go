package main

import (
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"

	"github.com/Wirezat/GoLog"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	thumbnailMaxEdge = 1200
	thumbnailQuality = 82
)

var thumbnailFormats = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true,
}

// canThumbnail reports whether name is a format servePreviewImage can decode.
func canThumbnail(name string) bool {
	return thumbnailFormats[officeExt(name)]
}

// wantsPreviewImage reports whether this request is for a thumbnailed image.
func wantsPreviewImage(r *http.Request, ctx *requestContext) bool {
	return r.URL.Query().Get("preview") == "1" && canThumbnail(ctx.fileInfo.Name())
}

// servePreviewImage decodes the shared image and answers with a JPEG capped
// at thumbnailMaxEdge on its longest side, flattened onto a white background.
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
	if err := jpeg.Encode(w, thumbnail(src, thumbnailMaxEdge), &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		GoLog.Errorf("thumbnail encode %s: %v", ctx.diskPath, err)
	}
}

// thumbnail scales src to fit within maxEdge on its longest side, preserving
// aspect ratio, and flattens it onto a white background.
func thumbnail(src image.Image, maxEdge int) *image.RGBA {
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
