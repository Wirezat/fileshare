package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestFitsDecodeLimit(t *testing.T) {
	cases := []struct {
		w, h int
		want bool
	}{
		{4032, 3024, true},
		{4096, 4096, true},
		{1816, 4032, true},
		{4097, 4096, false},
		{8000, 6000, false},
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
	_, err := EncodeThumbnail(context.Background(), bytes.NewReader(pngHeaderOnly(8000, 6000)), 1200)
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

// jpegWithExifThumbnail returns a JPEG header claiming w×h pixels whose Exif IFD1 points to thumb, with no image data.
func jpegWithExifThumbnail(w, h int, thumb []byte) []byte {
	le := binary.LittleEndian
	tiff := []byte("II*\x00")
	tiff = le.AppendUint32(tiff, 8)
	tiff = le.AppendUint16(tiff, 0)
	tiff = le.AppendUint32(tiff, 14)
	tiff = le.AppendUint16(tiff, 2)
	for _, e := range [][2]uint32{{0x0201, 44}, {0x0202, uint32(len(thumb))}} {
		tiff = le.AppendUint16(tiff, uint16(e[0]))
		tiff = le.AppendUint16(tiff, 4)
		tiff = le.AppendUint32(tiff, 1)
		tiff = le.AppendUint32(tiff, e[1])
	}
	tiff = le.AppendUint32(tiff, 0)
	tiff = append(tiff, thumb...)

	out := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	out = append(out, "JFIF\x00"...)
	out = append(out, 1, 1, 0, 0, 1, 0, 1, 0, 0)
	out = append(out, 0xFF, 0xE1)
	out = binary.BigEndian.AppendUint16(out, uint16(2+6+len(tiff)))
	out = append(out, "Exif\x00\x00"...)
	out = append(out, tiff...)
	out = append(out, 0xFF, 0xC0, 0x00, 0x11, 8)
	out = binary.BigEndian.AppendUint16(out, uint16(h))
	out = binary.BigEndian.AppendUint16(out, uint16(w))
	out = append(out, 3, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0)
	return append(out, 0xFF, 0xD9)
}

func TestEncodeThumbnailUsesTheExifThumbnailOfOversizedImages(t *testing.T) {
	var thumb bytes.Buffer
	jpeg.Encode(&thumb, image.NewRGBA(image.Rect(0, 0, 160, 120)), nil)

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(jpegWithExifThumbnail(8000, 6000, thumb.Bytes())), 120)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || cfg.Width != 120 || cfg.Height != 90 {
		t.Errorf("got %s %dx%d (%v), want jpeg 120x90", format, cfg.Width, cfg.Height, err)
	}
}

// withExifOrientation returns the JPEG j with an Exif segment whose IFD0 holds orientation o.
func withExifOrientation(j []byte, o uint16) []byte {
	le := binary.LittleEndian
	tiff := []byte("II*\x00")
	tiff = le.AppendUint32(tiff, 8)
	tiff = le.AppendUint16(tiff, 1)
	tiff = le.AppendUint16(tiff, 0x0112)
	tiff = le.AppendUint16(tiff, 3)
	tiff = le.AppendUint32(tiff, 1)
	tiff = le.AppendUint16(tiff, o)
	tiff = le.AppendUint16(tiff, 0)
	tiff = le.AppendUint32(tiff, 0)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(2+6+len(tiff)))
	out = append(out, "Exif\x00\x00"...)
	out = append(out, tiff...)
	return append(out, j[2:]...)
}

func TestEncodeThumbnailFollowsExifOrientation(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			if x < 100 {
				src.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				src.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var j bytes.Buffer
	jpeg.Encode(&j, src, &jpeg.Options{Quality: 95})

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(withExifOrientation(j.Bytes(), 6)), 200)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 100 || b.Dy() != 200 {
		t.Fatalf("preview is %dx%d, want 100x200", b.Dx(), b.Dy())
	}
	top, _, _, _ := img.At(50, 50).RGBA()
	_, _, bottom, _ := img.At(50, 150).RGBA()
	if top < 0xC000 || bottom < 0xC000 {
		t.Errorf("orientation 6 should put the left (red) half on top and the right (blue) half below")
	}
}
