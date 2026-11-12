package cover

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	tagUnsync      = 0x80
	tagExtended    = 0x40
	v24Unsync      = 0x02
	v24LengthField = 0x01
	v24Skipped     = 0x0c
	v23Skipped     = 0xc0
)

// findID3 returns the APIC or PIC picture of the ID3v2 tag at the start of r.
func findID3(r io.Reader) ([]byte, bool) {
	head, ok := readN(r, 10)
	if !ok || string(head[:3]) != "ID3" || head[3] < 2 || head[3] > 4 {
		return nil, false
	}
	version, flags := head[3], head[5]
	size, ok := syncsafeInt(head[6:10])
	if !ok || size > maxMetadata {
		return nil, false
	}
	tag, ok := readN(r, int64(size))
	if !ok {
		return nil, false
	}
	if flags&tagUnsync != 0 && version < 4 {
		tag = removeUnsync(tag)
	}
	c := cursor{tag}
	if flags&tagExtended != 0 && version >= 3 && !skipExtendedHeader(&c, version) {
		return nil, false
	}

	var p picker
	for {
		id, body, frameFlags, ok := nextFrame(&c, version)
		if !ok {
			break
		}
		if version == 4 {
			if frameFlags&v24Skipped != 0 {
				continue
			}
			if frameFlags&v24LengthField != 0 {
				if len(body) < 4 {
					continue
				}
				body = body[4:]
			}
			if frameFlags&v24Unsync != 0 {
				body = removeUnsync(body)
			}
		}
		if version == 3 && frameFlags&v23Skipped != 0 {
			continue
		}
		if id == "APIC" || id == "PIC" {
			if picType, data, ok := parseAPIC(body, version == 2); ok {
				p.add(uint32(picType), data)
			}
		}
	}
	return p.result()
}

// syncsafeInt decodes an ID3 syncsafe integer (7 bits per byte); false when a byte has its high bit set.
func syncsafeInt(b []byte) (int, bool) {
	n := 0
	for _, x := range b {
		if x&0x80 != 0 {
			return 0, false
		}
		n = n<<7 | int(x)
	}
	return n, true
}

// removeUnsync undoes ID3 unsynchronisation by dropping the 0x00 after every 0xFF.
func removeUnsync(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte{0xff, 0x00}, []byte{0xff})
}

// skipExtendedHeader advances c past the extended header of an ID3v2.3 or v2.4 tag; false when it does not fit.
func skipExtendedHeader(c *cursor, version byte) bool {
	raw, ok := c.take(4)
	if !ok {
		return false
	}
	if version == 3 {
		_, ok = c.take(uint64(binary.BigEndian.Uint32(raw)))
		return ok
	}
	n, ok := syncsafeInt(raw)
	if !ok || n < 4 {
		return false
	}
	_, ok = c.take(uint64(n - 4))
	return ok
}

// nextFrame reads one frame header and body; false at padding or when the tag ends.
func nextFrame(c *cursor, version byte) (id string, body []byte, formatFlags byte, ok bool) {
	idLen, sizeLen, flagLen := 4, 4, 2
	if version == 2 {
		idLen, sizeLen, flagLen = 3, 3, 0
	}
	head, ok := c.take(uint64(idLen + sizeLen + flagLen))
	if !ok || head[0] == 0 {
		return "", nil, 0, false
	}
	sizeBytes := head[idLen : idLen+sizeLen]
	var size int
	switch version {
	case 2:
		size = int(sizeBytes[0])<<16 | int(sizeBytes[1])<<8 | int(sizeBytes[2])
	case 3:
		size = int(binary.BigEndian.Uint32(sizeBytes))
	default:
		if size, ok = syncsafeInt(sizeBytes); !ok {
			return "", nil, 0, false
		}
	}
	if flagLen > 0 {
		formatFlags = head[idLen+sizeLen+1]
	}
	body, ok = c.take(uint64(size))
	return string(head[:idLen]), body, formatFlags, ok
}

// parseAPIC returns the picture type and image bytes of an APIC body, or of a PIC body when v22 is set.
func parseAPIC(b []byte, v22 bool) (byte, []byte, bool) {
	c := cursor{b}
	enc, ok := c.take(1)
	if !ok {
		return 0, nil, false
	}
	if v22 {
		if _, ok := c.take(3); !ok {
			return 0, nil, false
		}
	} else {
		end := bytes.IndexByte(c.b, 0)
		if end < 0 {
			return 0, nil, false
		}
		c.b = c.b[end+1:]
	}
	picType, ok := c.take(1)
	if !ok {
		return 0, nil, false
	}
	end, width := descriptionEnd(c.b, enc[0])
	if end < 0 {
		return 0, nil, false
	}
	data := c.b[end+width:]
	return picType[0], data, len(data) > 0
}

// descriptionEnd returns the offset and width of the terminator ending a text in encoding enc, or -1.
func descriptionEnd(b []byte, enc byte) (int, int) {
	if enc != 1 && enc != 2 {
		return bytes.IndexByte(b, 0), 1
	}
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			return i, 2
		}
	}
	return -1, 2
}
