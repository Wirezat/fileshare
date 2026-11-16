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

const DecodeMaxPixels = 4096 * 4096

var ErrTooLarge = errors.New("image exceeds the decode limit")

var decodeSlots = make(chan struct{}, 2)

// FitsDecodeLimit reports whether a w×h image is small enough to decode: at most DecodeMaxPixels pixels.
func FitsDecodeLimit(w, h int) bool {
	return w > 0 && h > 0 && w <= DecodeMaxPixels/h
}

// EncodeThumbnail decodes the image in src and returns it as a JPEG whose long edge is at most maxEdge.
// An image over FitsDecodeLimit is not decoded: its Exif thumbnail is used instead, and without one it returns
// ErrTooLarge. It returns ctx.Err() when ctx ends while waiting for one of the two decode slots.
func EncodeThumbnail(ctx context.Context, src io.ReadSeeker, maxEdge int) ([]byte, error) {
	out, err := encodeThumbnail(ctx, src, maxEdge)
	if !errors.Is(err, ErrTooLarge) {
		return out, err
	}
	thumb, ok := ExifThumbnail(src)
	if !ok {
		return nil, ErrTooLarge
	}
	return encodeThumbnail(ctx, bytes.NewReader(thumb), maxEdge)
}

// encodeThumbnail decodes the image in src and returns it as a JPEG whose long edge is at most maxEdge,
// or ErrTooLarge without decoding when the image exceeds FitsDecodeLimit.
func encodeThumbnail(ctx context.Context, src io.ReadSeeker, maxEdge int) ([]byte, error) {
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
