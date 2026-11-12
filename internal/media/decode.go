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

	_ "golang.org/x/image/webp"
)

const (
	DecodeMaxLong  = 3840
	DecodeMaxShort = 2160
)

var ErrTooLarge = errors.New("image exceeds the decode limit")

var decodeSlots = make(chan struct{}, 2)

// FitsDecodeLimit reports whether a w×h image is small enough to decode: long edge at most 3840, short edge at most 2160.
func FitsDecodeLimit(w, h int) bool {
	return max(w, h) <= DecodeMaxLong && min(w, h) <= DecodeMaxShort
}

// EncodeThumbnail decodes the image in src and returns it as a JPEG whose long edge is at most maxEdge.
// It returns ErrTooLarge without decoding when the image exceeds FitsDecodeLimit, and ctx.Err() when ctx ends
// while waiting for one of the two decode slots.
func EncodeThumbnail(ctx context.Context, src io.ReadSeeker, maxEdge int) ([]byte, error) {
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
	var out bytes.Buffer
	if err := jpeg.Encode(&out, Thumbnail(img, maxEdge), &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
