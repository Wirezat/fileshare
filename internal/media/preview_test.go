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
