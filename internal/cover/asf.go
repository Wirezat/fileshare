package cover

import (
	"encoding/binary"
	"io"
)

const (
	asfHeader          = "\x30\x26\xb2\x75\x8e\x66\xcf\x11\xa6\xd9\x00\xaa\x00\x62\xce\x6c"
	asfExtendedContent = "\x40\xa4\xd0\xd2\x07\xe3\xd2\x11\x97\xf0\x00\xa0\xc9\x5e\xa8\x50"
	asfHeaderExtension = "\xb5\x03\xbf\x5f\x2e\xa9\xcf\x11\x8e\xe3\x00\xc0\x0c\x20\x53\x65"
	asfMetadataLibrary = "\x94\x1c\x23\x44\x98\x94\xd1\x49\xa1\x41\x1d\x13\x4e\x45\x70\x54"

	asfObjectHeader    = 24
	asfHeaderFields    = 6
	asfExtensionFields = 22
	asfPictureName     = "W\x00M\x00/\x00P\x00i\x00c\x00t\x00u\x00r\x00e\x00\x00\x00"
)

// findASF returns the WM/Picture of the header of the ASF stream r (wma, wmv); a header over 16 MiB gives false.
func findASF(r io.Reader) ([]byte, bool) {
	head, ok := readN(r, asfObjectHeader)
	if !ok || string(head[:16]) != asfHeader {
		return nil, false
	}
	size := binary.LittleEndian.Uint64(head[16:])
	if size < asfObjectHeader+asfHeaderFields || size > maxMetadata {
		return nil, false
	}
	body, ok := readN(r, int64(size-asfObjectHeader))
	if !ok {
		return nil, false
	}
	var p picker
	asfObjects(body[asfHeaderFields:], &p)
	return p.result()
}

// asfObjects adds the pictures of the ASF objects packed in b to p, including those of a header extension.
func asfObjects(b []byte, p *picker) {
	c := cursor{b}
	for {
		head, ok := c.take(asfObjectHeader)
		if !ok {
			return
		}
		size := binary.LittleEndian.Uint64(head[16:])
		if size < asfObjectHeader {
			return
		}
		payload, ok := c.take(size - asfObjectHeader)
		if !ok {
			return
		}
		switch string(head[:16]) {
		case asfExtendedContent:
			asfExtendedPictures(payload, p)
		case asfMetadataLibrary:
			asfLibraryPictures(payload, p)
		case asfHeaderExtension:
			if len(payload) >= asfExtensionFields {
				asfObjects(payload[asfExtensionFields:], p)
			}
		}
	}
}

// asfExtendedPictures adds the pictures of an extended content description payload to p.
func asfExtendedPictures(b []byte, p *picker) {
	c := cursor{b}
	count, ok := c.u16le()
	for range count {
		var nameLen, valueLen uint16
		var name, value []byte
		if nameLen, ok = c.u16le(); !ok {
			return
		}
		if name, ok = c.take(uint64(nameLen)); !ok {
			return
		}
		if _, ok = c.u16le(); !ok {
			return
		}
		if valueLen, ok = c.u16le(); !ok {
			return
		}
		if value, ok = c.take(uint64(valueLen)); !ok {
			return
		}
		if string(name) == asfPictureName {
			asfPicture(value, p)
		}
	}
}

// asfLibraryPictures adds the pictures of a metadata library payload to p.
func asfLibraryPictures(b []byte, p *picker) {
	c := cursor{b}
	count, _ := c.u16le()
	for range count {
		head, ok := c.take(12)
		if !ok {
			return
		}
		name, ok := c.take(uint64(binary.LittleEndian.Uint16(head[4:])))
		if !ok {
			return
		}
		value, ok := c.take(uint64(binary.LittleEndian.Uint32(head[8:])))
		if !ok {
			return
		}
		if string(name) == asfPictureName {
			asfPicture(value, p)
		}
	}
}

// asfPicture adds the image of a WM/Picture value (type, length, MIME type, description, data) to p.
func asfPicture(b []byte, p *picker) {
	c := cursor{b}
	picType, ok := c.take(1)
	if !ok {
		return
	}
	n, ok := c.u32(binary.LittleEndian)
	if !ok {
		return
	}
	for range 2 {
		for {
			unit, ok := c.u16le()
			if !ok {
				return
			}
			if unit == 0 {
				break
			}
		}
	}
	if data, ok := c.take(uint64(n)); ok && n > 0 {
		p.add(uint32(picType[0]), data)
	}
}

// u16le reads a little-endian 16-bit integer; false when fewer than 2 bytes remain.
func (c *cursor) u16le() (uint16, bool) {
	b, ok := c.take(2)
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b), true
}
