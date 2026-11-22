package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func solidJPEG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	return b.Bytes()
}

func tiffWithThumbnail(thumb []byte) []byte {
	return exifItem(thumb)[4:]
}

func withExifSegment(j, tiff []byte) []byte {
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(2+6+len(tiff)))
	out = append(out, "Exif\x00\x00"...)
	out = append(out, tiff...)
	return append(out, j[2:]...)
}

var (
	red  = color.RGBA{R: 255, A: 255}
	blue = color.RGBA{B: 255, A: 255}
)

func centerIsRed(t *testing.T, out []byte) bool {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r, _, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	return r > 0xC000 && bl < 0x4000
}

func TestEncodeThumbnailTakesTheExifThumbnailWhenItIsLargeEnough(t *testing.T) {
	photo := withExifSegment(solidJPEG(1000, 500, blue), tiffWithThumbnail(solidJPEG(400, 200, red)))

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(photo), 300)
	if err != nil || !centerIsRed(t, out) {
		t.Errorf("300 px preview: err %v, want the red Exif thumbnail", err)
	}
	out, err = EncodeThumbnail(context.Background(), bytes.NewReader(photo), 800)
	if err != nil || centerIsRed(t, out) {
		t.Errorf("800 px preview: err %v, want the blue photo itself", err)
	}
}

func TestEncodeThumbnailTakesTheExifThumbnailOfWebP(t *testing.T) {
	chunk := func(fourcc string, data []byte) []byte {
		out := binary.LittleEndian.AppendUint32([]byte(fourcc), uint32(len(data)))
		out = append(out, data...)
		if len(data)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	body := append([]byte("WEBP"), chunk("VP8 ", make([]byte, 31))...)
	body = append(body, chunk("EXIF", tiffWithThumbnail(solidJPEG(400, 200, red)))...)
	webp := append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(webp), 300)
	if err != nil || !centerIsRed(t, out) {
		t.Errorf("err %v, want the red Exif thumbnail", err)
	}
}

type testEntry struct {
	tag, typ     uint16
	count, value uint32
}

func ifdBytes(entries ...testEntry) []byte {
	le := binary.LittleEndian
	out := le.AppendUint16(nil, uint16(len(entries)))
	for _, e := range entries {
		out = le.AppendUint16(out, e.tag)
		out = le.AppendUint16(out, e.typ)
		out = le.AppendUint32(out, e.count)
		out = le.AppendUint32(out, e.value)
	}
	return le.AppendUint32(out, 0)
}

func jpegSize(t *testing.T, out []byte) (int, int) {
	t.Helper()
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func tiffRaw(small, big []byte) []byte {
	sub := 8 + 54
	data := sub + 42
	raw := []byte("II*\x00\x08\x00\x00\x00")
	raw = append(raw, ifdBytes(
		testEntry{0x0112, 3, 1, 6},
		testEntry{0x0201, 4, 1, uint32(data)},
		testEntry{0x0202, 4, 1, uint32(len(small))},
		testEntry{0x014A, 4, 1, uint32(sub)})...)
	raw = append(raw, ifdBytes(
		testEntry{0x0103, 3, 1, 6},
		testEntry{0x0111, 4, 1, uint32(data + len(small))},
		testEntry{0x0117, 4, 1, uint32(len(big))})...)
	return append(append(raw, small...), big...)
}

func TestRawPreviewIsTheLargestEmbeddedJPEGTurnedByTheOrientation(t *testing.T) {
	raw := tiffRaw(solidJPEG(160, 120, blue), solidJPEG(800, 600, red))

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(raw), 400)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := jpegSize(t, out); w != 300 || h != 400 || !centerIsRed(t, out) {
		t.Errorf("got %dx%d, want the red 800x600 preview scaled to 400 and turned upright to 300x400", w, h)
	}
}

func TestRW2PreviewFromJpgFromRaw(t *testing.T) {
	preview := solidJPEG(400, 200, red)
	raw := append([]byte("IIU\x00\x08\x00\x00\x00"), ifdBytes(testEntry{0x002E, 7, uint32(len(preview)), 8 + 18})...)
	raw = append(raw, preview...)

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(raw), 200)
	if err != nil || !centerIsRed(t, out) {
		t.Errorf("err %v, want the red embedded JPEG", err)
	}
}

func TestCR3PreviewTurnedByItsMetadata(t *testing.T) {
	prvw := make([]byte, 16)
	big := solidJPEG(800, 600, red)
	binary.BigEndian.PutUint32(prvw[12:], uint32(len(big)))
	cmt1 := append([]byte("II*\x00\x08\x00\x00\x00"), ifdBytes(testEntry{0x0112, 3, 1, 8})...)
	canon := []byte("\x85\xc0\xb6\x87\x82\x0f\x11\xe0\x81\x11\xf4\xce\x46\x2b\x6a\x48")
	previewUUID := []byte("\xea\xf4\x2b\x5e\x1c\x98\x4b\x88\xb9\xfb\xb7\xdc\x40\x6e\x4d\x16")
	file := bytes.Join([][]byte{
		bmffBox("ftyp", []byte("crx \x00\x00\x00\x01")),
		bmffBox("moov", bmffBox("uuid", canon, bmffBox("CMT1", cmt1))),
		bmffBox("uuid", previewUUID, make([]byte, 8), bmffBox("PRVW", prvw, big)),
	}, nil)

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(file), 400)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := jpegSize(t, out); w != 300 || h != 400 {
		t.Errorf("got %dx%d, want the 800x600 preview turned upright to 300x400", w, h)
	}
}

func TestRAFPreviewFromItsHeader(t *testing.T) {
	preview := solidJPEG(400, 200, red)
	raf := append([]byte("FUJIFILMCCD-RAW "), make([]byte, 84-16)...)
	raf = binary.BigEndian.AppendUint32(raf, 100)
	raf = binary.BigEndian.AppendUint32(raf, uint32(len(preview)))
	raf = append(raf, make([]byte, 100-len(raf))...)
	raf = append(raf, preview...)

	out, err := EncodeThumbnail(context.Background(), bytes.NewReader(raf), 200)
	if err != nil || !centerIsRed(t, out) {
		t.Errorf("err %v, want the red embedded JPEG", err)
	}
}
