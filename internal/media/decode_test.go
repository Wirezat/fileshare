package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestFitsDecodeLimit(t *testing.T) {
	cases := []struct {
		w, h int
		want bool
	}{
		{3840, 2160, true},
		{2160, 3840, true},
		{3841, 100, false},
		{100, 3841, false},
		{2200, 2200, false},
		{4000, 3000, false},
	}
	for _, tc := range cases {
		if got := FitsDecodeLimit(tc.w, tc.h); got != tc.want {
			t.Errorf("FitsDecodeLimit(%d, %d) = %v, want %v", tc.w, tc.h, got, tc.want)
		}
	}
}

// pngHeaderOnly returns a PNG signature and IHDR chunk claiming w×h pixels, with no image data.
func pngHeaderOnly(w, h int) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, uint32(w))
	binary.Write(&ihdr, binary.BigEndian, uint32(h))
	ihdr.Write([]byte{8, 2, 0, 0, 0})
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4))
	out.Write(ihdr.Bytes())
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

func TestEncodeThumbnailRefusesOversizedWithoutDecoding(t *testing.T) {
	_, err := EncodeThumbnail(context.Background(), bytes.NewReader(pngHeaderOnly(4000, 3000)), 1200)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestEncodeThumbnailScalesLongEdge(t *testing.T) {
	var src bytes.Buffer
	png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 300, 100)))

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(src.Bytes()), 120)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || cfg.Width != 120 || cfg.Height != 40 {
		t.Errorf("got %s %dx%d (%v), want jpeg 120x40", format, cfg.Width, cfg.Height, err)
	}
}

func TestEncodeThumbnailStopsWaitingWithTheContext(t *testing.T) {
	for range cap(decodeSlots) {
		decodeSlots <- struct{}{}
	}
	defer func() {
		for range cap(decodeSlots) {
			<-decodeSlots
		}
	}()
	var src bytes.Buffer
	png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := EncodeThumbnail(ctx, bytes.NewReader(src.Bytes()), 120); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
