package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

const (
	exifHeaderMax       = 1 << 20
	exifTagOrientation  = 0x0112
	exifTagThumbOffset  = 0x0201
	exifTagThumbLength  = 0x0202
	jpegMarkerStartScan = 0xDA
	jpegMarkerExif      = 0xE1
)

// ExifOrientation returns the Exif orientation (1 to 8) of the JPEG in r, 1 when it has none or an invalid one.
func ExifOrientation(r io.ReadSeeker) int {
	t, _, ok := exifTIFF(r)
	if !ok {
		return 1
	}
	return tiffOrientation(t)
}

// tiffOrientation returns the orientation tag (1 to 8) of IFD0 of the TIFF structure t, 1 when it has none
// or an invalid one.
func tiffOrientation(t []byte) int {
	bo, ok := tiffByteOrder(t)
	if !ok {
		return 1
	}
	ifd0 := int(bo.Uint32(t[4:]))
	if ifd0 < 0 || ifd0+2 > len(t) {
		return 1
	}
	for i := range int(bo.Uint16(t[ifd0:])) {
		e := ifd0 + 2 + 12*i
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == exifTagOrientation {
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}

// exifTIFF returns the TIFF structure of the Exif segment of the JPEG in r and its offset in the file, reading at
// most exifHeaderMax bytes from the start of r; ok is false when r is no JPEG or has no Exif segment before its
// image data.
func exifTIFF(r io.ReadSeeker) (t []byte, at int64, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, 0, false
	}
	br := bufio.NewReader(io.LimitReader(r, exifHeaderMax))
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return nil, 0, false
	}
	pos := int64(2)
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0] != 0xFF || hdr[1] == jpegMarkerStartScan {
			return nil, 0, false
		}
		n := int(binary.BigEndian.Uint16(hdr[2:])) - 2
		if n < 0 {
			return nil, 0, false
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(br, seg); err != nil {
			return nil, 0, false
		}
		if hdr[1] == jpegMarkerExif && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) && len(seg) >= 14 {
			return seg[6:], pos + 4 + 6, true
		}
		pos += 4 + int64(n)
	}
}

// tiffThumbnail returns the offset in t and the length of the JPEG that IFD1 of the TIFF structure t locates;
// ok is false when t is malformed or has no such JPEG.
func tiffThumbnail(t []byte) (off, length int, ok bool) {
	bo, ok := tiffByteOrder(t)
	if !ok {
		return 0, 0, false
	}
	ifd1, ok := nextIFD(t, bo, int(bo.Uint32(t[4:])))
	if !ok || ifd1 == 0 || ifd1+2 > len(t) {
		return 0, 0, false
	}
	count := int(bo.Uint16(t[ifd1:]))
	for i := range count {
		e := ifd1 + 2 + 12*i
		if e+12 > len(t) {
			return 0, 0, false
		}
		switch bo.Uint16(t[e:]) {
		case exifTagThumbOffset:
			off = int(bo.Uint32(t[e+8:]))
		case exifTagThumbLength:
			length = int(bo.Uint32(t[e+8:]))
		}
	}
	if off <= 0 || length <= 0 || off > len(t) || length > len(t)-off || !bytes.HasPrefix(t[off:], []byte{0xFF, 0xD8}) {
		return 0, 0, false
	}
	return off, length, true
}

// tiffByteOrder returns the byte order the TIFF structure t declares; ok is false when t is too short or declares none.
func tiffByteOrder(t []byte) (bo binary.ByteOrder, ok bool) {
	if len(t) < 8 {
		return nil, false
	}
	switch string(t[:2]) {
	case "II":
		return binary.LittleEndian, true
	case "MM":
		return binary.BigEndian, true
	}
	return nil, false
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
