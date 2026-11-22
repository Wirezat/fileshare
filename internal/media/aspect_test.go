package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

func TestAspectRatioOfJPEGFollowsExifOrientation(t *testing.T) {
	var j bytes.Buffer
	jpeg.Encode(&j, image.NewRGBA(image.Rect(0, 0, 200, 100)), nil)

	got, ok := AspectRatio(bytes.NewReader(withExifOrientation(j.Bytes(), 6)), "photo.jpg")
	if !ok || got != 0.5 {
		t.Errorf("AspectRatio = %v, %v; want 0.5 for a 200x100 photo turned upright", got, ok)
	}
}

func TestAspectRatioOfHeicAndAvifFromTheirHeader(t *testing.T) {
	for _, name := range []string{"testdata/photo.avif", "testdata/photo.heic"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := AspectRatio(bytes.NewReader(src), name); !ok || got != 1 {
			t.Errorf("%s: AspectRatio = %v, %v; want 1 for the 512x512 photo", name, got, ok)
		}
	}
}

func TestAspectRatioOfSVG(t *testing.T) {
	cases := []struct {
		svg  string
		want float64
	}{
		{`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 150"></svg>`, 2},
		{`<svg width="40px" height="80px" viewBox="0 0 300 150"></svg>`, 0.5},
	}
	for _, tc := range cases {
		if got, ok := AspectRatio(bytes.NewReader([]byte(tc.svg)), "a.svg"); !ok || got != tc.want {
			t.Errorf("AspectRatio(%s) = %v, %v; want %v", tc.svg, got, ok, tc.want)
		}
	}
}

func bmffBox(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(typ), body...)...)
}

func tkhd(w, h uint32, rotated bool) []byte {
	body := make([]byte, 84)
	matrix := body[40:76]
	if rotated {
		binary.BigEndian.PutUint32(matrix[4:], 0x00010000)
		binary.BigEndian.PutUint32(matrix[12:], 0xFFFF0000)
	} else {
		binary.BigEndian.PutUint32(matrix[0:], 0x00010000)
		binary.BigEndian.PutUint32(matrix[16:], 0x00010000)
	}
	binary.BigEndian.PutUint32(body[76:], w<<16)
	binary.BigEndian.PutUint32(body[80:], h<<16)
	return bmffBox("tkhd", body)
}

func TestAspectRatioOfMP4FromItsVideoTrack(t *testing.T) {
	moov := bmffBox("moov",
		bmffBox("mvhd", make([]byte, 100)),
		bmffBox("trak", tkhd(0, 0, false), bmffBox("mdia", make([]byte, 64))),
		bmffBox("trak", tkhd(1920, 1080, true), bmffBox("mdia", make([]byte, 64))))
	file := bytes.Join([][]byte{bmffBox("ftyp", []byte("isom\x00\x00\x00\x00")), bmffBox("mdat", make([]byte, 1<<20)), moov}, nil)

	got, ok := AspectRatio(bytes.NewReader(file), "clip.mp4")
	if !ok || got != 0.5625 {
		t.Errorf("AspectRatio = %v, %v; want 0.5625 for a 1920x1080 track rotated by 90 degrees", got, ok)
	}
}

func ebmlBytes(id []byte, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := append(append([]byte(nil), id...), 0x01)
	size := binary.BigEndian.AppendUint64(nil, uint64(len(body)))
	return append(append(out, size[1:]...), body...)
}

func TestAspectRatioOfMatroskaFromItsVideoTrack(t *testing.T) {
	video := ebmlBytes([]byte{0xE0}, ebmlBytes([]byte{0xB0}, []byte{0x02, 0x80}), ebmlBytes([]byte{0xBA}, []byte{0x01, 0x68}))
	file := bytes.Join([][]byte{
		ebmlBytes([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebmlBytes([]byte{0x42, 0x82}, []byte("webm"))),
		ebmlBytes([]byte{0x18, 0x53, 0x80, 0x67},
			ebmlBytes([]byte{0x11, 0x4D, 0x9B, 0x74}, make([]byte, 40)),
			ebmlBytes([]byte{0x16, 0x54, 0xAE, 0x6B},
				ebmlBytes([]byte{0xAE}, ebmlBytes([]byte{0xD7}, []byte{1})),
				ebmlBytes([]byte{0xAE}, ebmlBytes([]byte{0xD7}, []byte{2}), video))),
	}, nil)

	got, ok := AspectRatio(bytes.NewReader(file), "clip.webm")
	if !ok || got != 640.0/360 {
		t.Errorf("AspectRatio = %v, %v; want %v for a 640x360 video track", got, ok, 640.0/360)
	}
}
