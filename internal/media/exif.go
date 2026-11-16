package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

const (
	exifHeaderMax       = 1 << 20
	exifTagThumbOffset  = 0x0201
	exifTagThumbLength  = 0x0202
	jpegMarkerStartScan = 0xDA
	jpegMarkerExif      = 0xE1
)

// ExifThumbnail returns the JPEG thumbnail that the Exif data of the JPEG in r points to from its IFD1,
// reading at most exifHeaderMax bytes from the start of r; ok is false when there is none.
func ExifThumbnail(r io.ReadSeeker) (thumb []byte, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	br := bufio.NewReader(io.LimitReader(r, exifHeaderMax))
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return nil, false
	}
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0] != 0xFF || hdr[1] == jpegMarkerStartScan {
			return nil, false
		}
		n := int(binary.BigEndian.Uint16(hdr[2:])) - 2
		if n < 0 {
			return nil, false
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(br, seg); err != nil {
			return nil, false
		}
		if hdr[1] == jpegMarkerExif && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffThumbnail(seg[6:])
		}
	}
}

// tiffThumbnail returns a copy of the JPEG that IFD1 of the TIFF structure t locates by offset and length;
// ok is false when t is malformed or has no such JPEG.
func tiffThumbnail(t []byte) (thumb []byte, ok bool) {
	if len(t) < 8 {
		return nil, false
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, false
	}
	ifd1, ok := nextIFD(t, bo, int(bo.Uint32(t[4:])))
	if !ok || ifd1 == 0 || ifd1+2 > len(t) {
		return nil, false
	}
	var off, length int
	count := int(bo.Uint16(t[ifd1:]))
	for i := range count {
		e := ifd1 + 2 + 12*i
		if e+12 > len(t) {
			return nil, false
		}
		switch bo.Uint16(t[e:]) {
		case exifTagThumbOffset:
			off = int(bo.Uint32(t[e+8:]))
		case exifTagThumbLength:
			length = int(bo.Uint32(t[e+8:]))
		}
	}
	if off <= 0 || length <= 0 || off > len(t) || length > len(t)-off || !bytes.HasPrefix(t[off:], []byte{0xFF, 0xD8}) {
		return nil, false
	}
	return bytes.Clone(t[off : off+length]), true
}

// nextIFD returns the offset of the IFD that follows the IFD at offset ifd in t, 0 when it is the last one;
// ok is false when the IFD does not fit into t.
func nextIFD(t []byte, bo binary.ByteOrder, ifd int) (next int, ok bool) {
	if ifd < 0 || ifd+2 > len(t) {
		return 0, false
	}
	end := ifd + 2 + 12*int(bo.Uint16(t[ifd:]))
	if end+4 > len(t) {
		return 0, false
	}
	return int(bo.Uint32(t[end:])), true
}
