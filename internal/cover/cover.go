package cover

import (
	"encoding/binary"
	"io"
	"path/filepath"
	"strings"
)

const (
	maxMetadata = 16 << 20
	frontCover  = 3
)

// Find returns the picture embedded in the mp3, flac or ogg file read from r, chosen by name's extension.
// A front cover wins over other pictures; any other format, a missing picture or a malformed file gives false.
// It reads at most 16 MiB of metadata.
func Find(r io.Reader, name string) ([]byte, bool) {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "mp3":
		return findID3(r)
	case "flac":
		return findFLAC(r)
	case "ogg":
		return findOgg(r)
	}
	return nil, false
}

type picker struct {
	first, front []byte
}

// add records a picture of type picType; the first picture and the first front cover are kept.
func (p *picker) add(picType uint32, data []byte) {
	if p.first == nil {
		p.first = data
	}
	if picType == frontCover && p.front == nil {
		p.front = data
	}
}

// result returns the front cover, else the first picture, and false when there was none.
func (p *picker) result() ([]byte, bool) {
	if p.front != nil {
		return p.front, true
	}
	return p.first, p.first != nil
}

// readN reads exactly n bytes from r, growing the buffer only as data arrives; false when r ends early.
func readN(r io.Reader, n int64) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r, n))
	return b, err == nil && int64(len(b)) == n
}

type cursor struct {
	b []byte
}

// take returns the next n bytes and advances past them; false when fewer remain.
func (c *cursor) take(n uint64) ([]byte, bool) {
	if n > uint64(len(c.b)) {
		return nil, false
	}
	out := c.b[:n]
	c.b = c.b[n:]
	return out, true
}

// u32 reads a 32-bit integer in order; false when fewer than 4 bytes remain.
func (c *cursor) u32(order binary.ByteOrder) (uint32, bool) {
	b, ok := c.take(4)
	if !ok {
		return 0, false
	}
	return order.Uint32(b), true
}

// prefixed reads a 32-bit length in order and returns that many following bytes; false when they do not fit.
func (c *cursor) prefixed(order binary.ByteOrder) ([]byte, bool) {
	n, ok := c.u32(order)
	if !ok {
		return nil, false
	}
	return c.take(uint64(n))
}

// parsePicture reads a FLAC PICTURE block body and returns its picture type and image bytes.
func parsePicture(block []byte) (uint32, []byte, bool) {
	c := cursor{block}
	picType, ok := c.u32(binary.BigEndian)
	if !ok {
		return 0, nil, false
	}
	if _, ok := c.prefixed(binary.BigEndian); !ok {
		return 0, nil, false
	}
	if _, ok := c.prefixed(binary.BigEndian); !ok {
		return 0, nil, false
	}
	if _, ok := c.take(16); !ok {
		return 0, nil, false
	}
	data, ok := c.prefixed(binary.BigEndian)
	return picType, data, ok && len(data) > 0
}
