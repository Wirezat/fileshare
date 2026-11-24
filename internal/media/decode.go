package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"

	_ "github.com/gen2brain/avif"
	_ "github.com/gen2brain/heic"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const DecodeMaxPixels = 4096 * 4096

var ErrTooLarge = errors.New("image exceeds the decode limit")

var decodeSlots = make(chan struct{}, 2)

// FitsDecodeLimit reports whether a w×h image is small enough to decode: at most DecodeMaxPixels pixels.
func FitsDecodeLimit(w, h int) bool {
	return w > 0 && h > 0 && w <= DecodeMaxPixels/h
}

// EncodeThumbnail decodes the image in src and returns it as a JPEG whose long edge is at most maxEdge,
// turned upright by its Exif orientation. A preview the file embeds is decoded instead when it is at least maxEdge
// long, when it is larger than the image itself, or when that cannot be decoded or exceeds FitsDecodeLimit; without one that gives the decode
// error or ErrTooLarge. A HEIF or AVIF file decodes its thumbnail item under the same size rule. It returns
// ctx.Err() when ctx ends while waiting for one of the two decode slots.
func EncodeThumbnail(ctx context.Context, src io.ReadSeeker, maxEdge int) ([]byte, error) {
	if file, ok := heifThumbnailItem(src, maxEdge); ok {
		src = bytes.NewReader(file)
	}
	p, hasPreview := findPreview(src)
	if hasPreview && (max(p.w, p.h) >= maxEdge || p.w*p.h > pixels(src)) {
		return encodePreview(ctx, src, p, maxEdge)
	}
	img, err := scaledImage(ctx, src, maxEdge)
	if err != nil && hasPreview && ctx.Err() == nil {
		return encodePreview(ctx, src, p, maxEdge)
	}
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, Orient(img, ExifOrientation(src)), &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pixels returns the number of pixels the header of the image in src gives, 0 when it cannot be read.
func pixels(src io.ReadSeeker) int {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	cfg, _, err := image.DecodeConfig(src)
	if err != nil {
		return 0
	}
	return cfg.Width * cfg.Height
}

// scaledImage decodes the image in src scaled to a long edge of at most maxEdge,
// or returns ErrTooLarge without decoding when the image exceeds FitsDecodeLimit.
func scaledImage(ctx context.Context, src io.ReadSeeker, maxEdge int) (*image.RGBA, error) {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(src)
	if err != nil {
		return nil, err
	}
	if !FitsDecodeLimit(cfg.Width, cfg.Height) {
		return nil, ErrTooLarge
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	select {
	case decodeSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-decodeSlots }()

	img, _, err := image.Decode(src)
	if err != nil {
		return nil, err
	}
	return Thumbnail(img, maxEdge), nil
}
