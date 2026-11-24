package media

import (
	"bytes"
	"image"
	"io"
)

const (
	servableMaxBytes = 200 << 10
	servableMaxScale = 3
	sharpMinEdge     = 300
)

// EmbeddedJPEG returns the preview JPEG that the image file in r carries, unchanged, when its long edge is at least
// sharpEnough(edge), the file does not turn it and ServableJPEG holds for it; ok is false otherwise.
func EmbeddedJPEG(r io.ReadSeeker, edge int) (data []byte, ok bool) {
	p, ok := findPreview(r)
	if !ok || max(p.w, p.h) < sharpEnough(edge) || p.orientation > 1 || p.n > servableMaxBytes {
		return nil, false
	}
	data, err := io.ReadAll(sectionOf(r, p))
	return data, err == nil && ServableJPEG(data, edge)
}

// sharpEnough returns the long edge an embedded preview needs to stand in for a preview of long edge edge: half of it
// but at least sharpMinEdge, all of it below sharpMinEdge and above ThumbnailMaxEdge.
func sharpEnough(edge int) int {
	if edge > ThumbnailMaxEdge {
		return edge
	}
	return min(edge, max((edge+1)/2, sharpMinEdge))
}

// ServableJPEG reports whether data can be shown as a preview of long edge edge without encoding it anew: an upright
// JPEG of at most servableMaxBytes whose long edge is at most servableMaxScale times edge.
func ServableJPEG(data []byte, edge int) bool {
	if len(data) > servableMaxBytes {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && format == "jpeg" && max(cfg.Width, cfg.Height) <= servableMaxScale*edge &&
		ExifOrientation(bytes.NewReader(data)) == 1
}
