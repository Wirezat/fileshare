package media

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func exifItem(thumb []byte) []byte {
	le := binary.LittleEndian
	tiff := []byte("II*\x00")
	tiff = le.AppendUint32(tiff, 8)
	tiff = le.AppendUint16(tiff, 0)
	tiff = le.AppendUint32(tiff, 14)
	tiff = le.AppendUint16(tiff, 2)
	for _, e := range [][2]uint32{{exifTagThumbOffset, 14 + 2 + 24 + 4}, {exifTagThumbLength, uint32(len(thumb))}} {
		tiff = le.AppendUint16(tiff, uint16(e[0]))
		tiff = le.AppendUint16(tiff, 4)
		tiff = le.AppendUint32(tiff, 1)
		tiff = le.AppendUint32(tiff, e[1])
	}
	tiff = le.AppendUint32(tiff, 0)
	tiff = append(tiff, thumb...)
	return append(binary.BigEndian.AppendUint32(nil, 0), tiff...)
}

// heifWithExif returns a HEIF file whose Exif item holds item, behind an unrelated item and media data.
func heifWithExif(item []byte) []byte {
	be := binary.BigEndian
	infe := func(id uint16, typ string) []byte {
		b := []byte{2, 0, 0, 0}
		b = be.AppendUint16(b, id)
		b = append(b, 0, 0)
		return bmffBox("infe", append(b, typ...), []byte{0})
	}
	iinf := bmffBox("iinf", []byte{0, 0, 0, 0, 0, 2}, infe(1, "hvc1"), infe(2, "Exif"))
	iloc := func(offset uint32) []byte {
		b := []byte{1, 0, 0, 0, 0x44, 0x00, 0, 2}
		for _, it := range [][3]uint32{{1, 0, 10}, {2, offset, uint32(len(item))}} {
			b = be.AppendUint16(b, uint16(it[0]))
			b = append(b, 0, 0, 0, 0, 0, 1)
			b = be.AppendUint32(b, it[1])
			b = be.AppendUint32(b, it[2])
		}
		return bmffBox("iloc", b)
	}
	build := func(offset uint32) []byte {
		meta := bmffBox("meta", []byte{0, 0, 0, 0}, iinf, iloc(offset))
		return bytes.Join([][]byte{bmffBox("ftyp", []byte("heic\x00\x00\x00\x00")), meta, bmffBox("mdat", make([]byte, 10), item)}, nil)
	}
	probe := build(0)
	return build(uint32(len(probe) - len(item)))
}

func TestHEIFExifThumbnail(t *testing.T) {
	thumb := append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte("x"), 100)...)

	got, ok := HEIFExifThumbnail(bytes.NewReader(heifWithExif(exifItem(thumb))))
	if !ok || !bytes.Equal(got, thumb) {
		t.Fatalf("HEIFExifThumbnail = %d bytes, %v; want the %d-byte thumbnail", len(got), ok, len(thumb))
	}
}

func TestHEIFExifThumbnailWithoutExifItem(t *testing.T) {
	if _, ok := HEIFExifThumbnail(bytes.NewReader(bmffBox("ftyp", []byte("avif\x00\x00\x00\x00")))); ok {
		t.Error("a file without meta box reported a thumbnail")
	}
}
