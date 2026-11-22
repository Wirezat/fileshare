package media

import (
	"encoding/binary"
	"io"
)

const (
	psdHeader         = 26
	psdThumbnailID    = 0x040C
	psdThumbHeader    = 28
	psdMaxResources   = 1024
	psdResourceHeader = 6
)

// psdPreviews returns the JPEG of the thumbnail resource in the image resources of the Photoshop file in r.
func psdPreviews(r io.ReadSeeker) []preview {
	if _, err := r.Seek(psdHeader, io.SeekStart); err != nil {
		return nil
	}
	colorMode, ok := readU32(r)
	if !ok {
		return nil
	}
	if _, err := r.Seek(int64(colorMode), io.SeekCurrent); err != nil {
		return nil
	}
	length, ok := readU32(r)
	if !ok {
		return nil
	}
	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil
	}
	end := pos + int64(length)
	for range psdMaxResources {
		if pos+psdResourceHeader+2 > end {
			return nil
		}
		var head [psdResourceHeader + 1]byte
		if _, err := io.ReadFull(r, head[:]); err != nil || string(head[:4]) != "8BIM" {
			return nil
		}
		id := binary.BigEndian.Uint16(head[4:])
		name := int64(head[6]) + 1
		name += name % 2
		if _, err := r.Seek(name-1, io.SeekCurrent); err != nil {
			return nil
		}
		size, ok := readU32(r)
		if !ok {
			return nil
		}
		data := pos + psdResourceHeader + name + 4
		if id == psdThumbnailID && size > psdThumbHeader {
			return []preview{{at: data + psdThumbHeader, n: int64(size) - psdThumbHeader, orientation: 1}}
		}
		pos = data + int64(size) + int64(size%2)
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return nil
		}
	}
	return nil
}

// readU32 reads a big-endian uint32 from r.
func readU32(r io.Reader) (uint32, bool) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[:]), true
}
