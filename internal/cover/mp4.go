package cover

import (
	"encoding/binary"
	"io"
)

const (
	maxTopLevelBoxes = 64
	boxHeader        = 8
	dataHeader       = 8
	metaVersionBytes = 4
	typeJPEG         = 13
	typePNG          = 14
)

// findMP4 returns the first picture of the covr atom in the moov box of the MP4 stream r.
// Top-level boxes before moov are skipped, a moov box over 16 MiB gives false.
func findMP4(r io.Reader) ([]byte, bool) {
	for range maxTopLevelBoxes {
		head, ok := readN(r, boxHeader)
		if !ok {
			return nil, false
		}
		size := uint64(binary.BigEndian.Uint32(head[:4]))
		if size < boxHeader {
			return nil, false
		}
		payload := size - boxHeader
		if string(head[4:8]) == "moov" {
			if payload > maxMetadata {
				return nil, false
			}
			body, ok := readN(r, int64(payload))
			if !ok {
				return nil, false
			}
			return coverInMoov(body)
		}
		if !skip(r, int64(payload)) {
			return nil, false
		}
	}
	return nil, false
}

// skip moves r forward by n bytes, by seeking when r can; false when r ends early.
func skip(r io.Reader, n int64) bool {
	if s, ok := r.(io.Seeker); ok {
		_, err := s.Seek(n, io.SeekCurrent)
		return err == nil
	}
	copied, err := io.CopyN(io.Discard, r, n)
	return err == nil && copied == n
}

// coverInMoov walks moov > udta > meta > ilst > covr > data and returns the first JPEG or PNG picture.
func coverInMoov(moov []byte) ([]byte, bool) {
	box := moov
	for _, name := range []string{"udta", "meta", "ilst", "covr"} {
		inner, ok := childBox(box, name)
		if !ok {
			return nil, false
		}
		if name == "meta" && len(inner) >= metaVersionBytes {
			inner = inner[metaVersionBytes:]
		}
		box = inner
	}
	data, ok := childBox(box, "data")
	if !ok || len(data) <= dataHeader || (data[3] != typeJPEG && data[3] != typePNG) {
		return nil, false
	}
	return data[dataHeader:], true
}

// childBox returns the payload of the first box named name among the boxes packed in b.
func childBox(b []byte, name string) ([]byte, bool) {
	c := cursor{b}
	for {
		head, ok := c.take(boxHeader)
		if !ok {
			return nil, false
		}
		size := uint64(binary.BigEndian.Uint32(head[:4]))
		if size < boxHeader {
			return nil, false
		}
		payload, ok := c.take(size - boxHeader)
		if !ok {
			return nil, false
		}
		if string(head[4:8]) == name {
			return payload, true
		}
	}
}
