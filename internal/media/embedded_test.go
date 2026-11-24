package media

import (
	"bytes"
	"testing"
)

func TestEmbeddedJPEGIsReturnedUnchanged(t *testing.T) {
	thumb := solidJPEG(400, 200, red)
	photo := withExifSegment(solidJPEG(1000, 500, blue), tiffWithThumbnail(thumb))

	for edge, want := range map[int]bool{100: false, 200: true, 400: true, 401: false} {
		got, ok := EmbeddedJPEG(bytes.NewReader(photo), edge)
		if ok != want || ok && !bytes.Equal(got, thumb) {
			t.Errorf("edge %d: %d bytes, %v; want %v", edge, len(got), ok, want)
		}
	}
}

func TestServableJPEGIsUprightSmallAndNotMuchLargerThanAsked(t *testing.T) {
	small := solidJPEG(400, 200, red)
	cases := []struct {
		name string
		data []byte
		edge int
		want bool
	}{
		{"fits", small, 300, true},
		{"three times the edge", small, 134, true},
		{"over three times the edge", small, 133, false},
		{"turned by its orientation", withExifOrientation(small, 6), 300, false},
		{"too many bytes", append(append([]byte(nil), small...), make([]byte, servableMaxBytes)...), 300, false},
		{"no jpeg", []byte("\x89PNG\r\n\x1a\n"), 300, false},
	}
	for _, tc := range cases {
		if got := ServableJPEG(tc.data, tc.edge); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
