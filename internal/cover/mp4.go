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

// findMP4 returns the first picture of the covr atom in moov > udta of the MP4 or QuickTime stream r.
// Boxes before moov and before udta are skipped, a udta box over 16 MiB gives false.
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
			return findUdta(r, payload)
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

// findUdta reads the boxes of the moov payload of length n at r and returns the cover in its udta box.
func findUdta(r io.Reader, n uint64) ([]byte, bool) {
	for range maxTopLevelBoxes {
		if n < boxHeader {
			return nil, false
		}
		head, ok := readN(r, boxHeader)
		if !ok {
			return nil, false
		}
		size := uint64(binary.BigEndian.Uint32(head[:4]))
		if size < boxHeader || size > n {
			return nil, false
		}
		n -= size
		payload := size - boxHeader
		if string(head[4:8]) != "udta" {
			if !skip(r, int64(payload)) {
				return nil, false
			}
			continue
		}
		if payload > maxMetadata {
			return nil, false
		}
		body, ok := readN(r, int64(payload))
		if !ok {
			return nil, false
		}
		return coverInUdta(body)
	}
	return nil, false
}

// coverInUdta walks udta > meta > ilst > covr > data and returns the first JPEG or PNG picture.
func coverInUdta(udta []byte) ([]byte, bool) {
	box := udta
	for _, name := range []string{"meta", "ilst", "covr"} {
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
