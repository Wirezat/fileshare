package media

import (
	"encoding/binary"
	"io"
)

const (
	heifMetaMax     = 4 << 20
	heifExifMax     = 1 << 20
	heifFileMax     = 64 << 20
	heifBoxHeader   = 8
	heifFullHeader  = 4
	heifExifTypeLen = 4
)

// HEIFExifThumbnail returns the JPEG thumbnail that the Exif item of the HEIC, HEIF or AVIF file in r points to
// from its IFD1; ok is false when the file has no such item or thumbnail. It reads a meta box of at most 4 MiB
// and an Exif item of at most 1 MiB.
func HEIFExifThumbnail(r io.ReadSeeker) (thumb []byte, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	meta, ok := heifMeta(r)
	if !ok {
		return nil, false
	}
	offset, length, ok := exifItemLocation(meta)
	if !ok || length <= heifExifTypeLen || length > heifExifMax {
		return nil, false
	}
	if _, err := r.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, false
	}
	item, err := io.ReadAll(io.LimitReader(r, int64(length)))
	if err != nil || uint64(len(item)) != length {
		return nil, false
	}
	tiffStart := heifExifTypeLen + uint64(binary.BigEndian.Uint32(item))
	if tiffStart >= uint64(len(item)) {
		return nil, false
	}
	return tiffThumbnail(item[tiffStart:])
}

// heifMeta returns the payload of the top-level meta box of r, at most heifMetaMax bytes, skipping the boxes before it.
func heifMeta(r io.ReadSeeker) ([]byte, bool) {
	n, ok := bmffChild(r, "meta", 0)
	if !ok || n < heifFullHeader || n > heifMetaMax {
		return nil, false
	}
	meta := make([]byte, n)
	if _, err := io.ReadFull(r, meta); err != nil {
		return nil, false
	}
	return meta, true
}

// isoChildren returns the boxes packed in b by type; a box that does not fit ends the list.
func isoChildren(b []byte) map[string][]byte {
	out := map[string][]byte{}
	for len(b) >= heifBoxHeader {
		size := uint64(binary.BigEndian.Uint32(b[:4]))
		if size < heifBoxHeader || size > uint64(len(b)) {
			break
		}
		if _, seen := out[string(b[4:8])]; !seen {
			out[string(b[4:8])] = b[heifBoxHeader:size]
		}
		b = b[size:]
	}
	return out
}

// exifItemLocation returns the file offset and length of the Exif item described by the meta payload.
func exifItemLocation(meta []byte) (offset, length uint64, ok bool) {
	if len(meta) < heifFullHeader {
		return 0, 0, false
	}
	boxes := isoChildren(meta[heifFullHeader:])
	id, ok := exifItemID(boxes["iinf"])
	if !ok {
		return 0, 0, false
	}
	return itemExtent(boxes["iloc"], id)
}

// exifItemID returns the ID of the item of type Exif in the iinf payload.
func exifItemID(iinf []byte) (uint32, bool) {
	if len(iinf) < heifFullHeader+2 {
		return 0, false
	}
	skip := heifFullHeader + 2
	if iinf[0] != 0 {
		skip = heifFullHeader + 4
	}
	if len(iinf) < skip {
		return 0, false
	}
	b := iinf[skip:]
	for len(b) >= heifBoxHeader {
		size := uint64(binary.BigEndian.Uint32(b[:4]))
		if size < heifBoxHeader || size > uint64(len(b)) {
			return 0, false
		}
		if string(b[4:8]) == "infe" {
			if id, typ, ok := infeItem(b[heifBoxHeader:size]); ok && typ == "Exif" {
				return id, true
			}
		}
		b = b[size:]
	}
	return 0, false
}

// infeItem returns the item ID and four-character type of an infe payload of version 2 or 3.
func infeItem(p []byte) (id uint32, typ string, ok bool) {
	if len(p) < heifFullHeader {
		return 0, "", false
	}
	switch p[0] {
	case 2:
		if len(p) < heifFullHeader+8 {
			return 0, "", false
		}
		return uint32(binary.BigEndian.Uint16(p[4:])), string(p[8:12]), true
	case 3:
		if len(p) < heifFullHeader+10 {
			return 0, "", false
		}
		return binary.BigEndian.Uint32(p[4:]), string(p[10:14]), true
	}
	return 0, "", false
}

// itemExtent returns the file offset and length of the first extent of item id in the iloc payload; only items
// stored at a file offset (construction method 0) count.
func itemExtent(iloc []byte, id uint32) (offset, length uint64, ok bool) {
	c := cursorBytes{b: iloc}
	version, ok := c.uint(1)
	if !ok || version > 2 || !c.skip(3) {
		return 0, 0, false
	}
	sizes, ok := c.uint(1)
	baseIdx, ok2 := c.uint(1)
	if !ok || !ok2 {
		return 0, 0, false
	}
	offSize, lenSize, baseSize, idxSize := int(sizes>>4), int(sizes&15), int(baseIdx>>4), int(baseIdx&15)
	if version == 0 {
		idxSize = 0
	}
	count, ok := c.uint(map[uint64]int{0: 2, 1: 2, 2: 4}[version])
	if !ok {
		return 0, 0, false
	}
	for range count {
		itemID, ok := c.uint(map[uint64]int{0: 2, 1: 2, 2: 4}[version])
		method := uint64(0)
		if version > 0 {
			var m uint64
			m, ok2 = c.uint(2)
			method, ok = m&15, ok && ok2
		}
		_, ok3 := c.uint(2)
		base, ok4 := c.uint(baseSize)
		extents, ok5 := c.uint(2)
		if !ok || !ok3 || !ok4 || !ok5 {
			return 0, 0, false
		}
		for e := range extents {
			if idxSize > 0 && !c.skip(idxSize) {
				return 0, 0, false
			}
			off, ok1 := c.uint(offSize)
			ln, ok2 := c.uint(lenSize)
			if !ok1 || !ok2 {
				return 0, 0, false
			}
			if uint32(itemID) == id && method == 0 && e == 0 {
				return base + off, ln, true
			}
		}
	}
	return 0, 0, false
}

type cursorBytes struct{ b []byte }

// uint reads an n-byte big-endian integer, n from 0 to 8; false when fewer bytes remain.
func (c *cursorBytes) uint(n int) (uint64, bool) {
	if n < 0 || n > 8 || n > len(c.b) {
		return 0, false
	}
	var v uint64
	for _, x := range c.b[:n] {
		v = v<<8 | uint64(x)
	}
	c.b = c.b[n:]
	return v, true
}

// skip moves past n bytes; false when fewer remain.
func (c *cursorBytes) skip(n int) bool {
	if n < 0 || n > len(c.b) {
		return false
	}
	c.b = c.b[n:]
	return true
}

// heifThumbnailItem returns the HEIF or AVIF file in r with its thumbnail item made the primary image, when the
// thumbnail's long edge reaches maxEdge or the primary image exceeds FitsDecodeLimit; ok is false otherwise.
// It reads files of at most heifFileMax bytes.
func heifThumbnailItem(r io.ReadSeeker, maxEdge int) (file []byte, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	if typ, _, ok := bmffNext(r); !ok || typ != "ftyp" {
		return nil, false
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	meta, ok := heifMeta(r)
	if !ok {
		return nil, false
	}
	metaEnd, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, false
	}
	pitmAt, primary, idSize, ok := primaryItem(meta)
	if !ok {
		return nil, false
	}
	children := isoChildren(meta[heifFullHeader:])
	thumb, ok := thumbnailOf(children["iref"], primary)
	if !ok {
		return nil, false
	}
	sizes := itemSizes(children["iprp"])
	tw, th := sizes[thumb][0], sizes[thumb][1]
	pw, ph := sizes[primary][0], sizes[primary][1]
	if max(tw, th) < maxEdge && FitsDecodeLimit(pw, ph) {
		return nil, false
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	file, err = io.ReadAll(io.LimitReader(r, heifFileMax+1))
	if err != nil || len(file) > heifFileMax {
		return nil, false
	}
	at := int(metaEnd) - len(meta) + pitmAt
	if idSize == 2 {
		binary.BigEndian.PutUint16(file[at:], uint16(thumb))
	} else {
		binary.BigEndian.PutUint32(file[at:], thumb)
	}
	return file, true
}

// primaryItem returns the offset in the meta payload of the item ID in its pitm box, that ID and its size in bytes.
func primaryItem(meta []byte) (at int, id uint32, idSize int, ok bool) {
	off := heifFullHeader
	for off+heifBoxHeader <= len(meta) {
		size := int(binary.BigEndian.Uint32(meta[off:]))
		if size < heifBoxHeader || off+size > len(meta) {
			return 0, 0, 0, false
		}
		if string(meta[off+4:off+8]) == "pitm" && size >= heifBoxHeader+heifFullHeader+2 {
			at = off + heifBoxHeader + heifFullHeader
			if meta[off+heifBoxHeader] == 0 {
				return at, uint32(binary.BigEndian.Uint16(meta[at:])), 2, true
			}
			if size >= heifBoxHeader+heifFullHeader+4 {
				return at, binary.BigEndian.Uint32(meta[at:]), 4, true
			}
			return 0, 0, 0, false
		}
		off += size
	}
	return 0, 0, 0, false
}

// thumbnailOf returns the ID of the item that the iref payload marks as thumbnail of item primary.
func thumbnailOf(iref []byte, primary uint32) (uint32, bool) {
	if len(iref) < heifFullHeader {
		return 0, false
	}
	idSize := 2
	if iref[0] != 0 {
		idSize = 4
	}
	for typ, ref := range isoBoxes(iref[heifFullHeader:]) {
		if typ != "thmb" {
			continue
		}
		c := cursorBytes{b: ref}
		from, ok1 := c.uint(idSize)
		count, ok2 := c.uint(2)
		if !ok1 || !ok2 {
			continue
		}
		for range count {
			if to, ok := c.uint(idSize); ok && uint32(to) == primary {
				return uint32(from), true
			}
		}
	}
	return 0, false
}

// itemSizes returns the width and height of each item that the ipma box in the iprp payload links to an ispe
// property of its ipco box.
func itemSizes(iprp []byte) map[uint32][2]int {
	children := isoChildren(iprp)
	var props [][2]int
	for typ, p := range isoBoxes(children["ipco"]) {
		size := [2]int{}
		if typ == "ispe" && len(p) >= 12 {
			size = [2]int{int(binary.BigEndian.Uint32(p[4:])), int(binary.BigEndian.Uint32(p[8:]))}
		}
		props = append(props, size)
	}
	sizes := map[uint32][2]int{}
	ipma := children["ipma"]
	if len(ipma) < heifFullHeader {
		return sizes
	}
	idSize, wide := 2, ipma[3]&1 == 1
	if ipma[0] != 0 {
		idSize = 4
	}
	c := cursorBytes{b: ipma[heifFullHeader:]}
	entries, ok := c.uint(4)
	for i := uint64(0); ok && i < entries; i++ {
		id, ok1 := c.uint(idSize)
		count, ok2 := c.uint(1)
		if !ok1 || !ok2 {
			break
		}
		for range count {
			var index uint64
			if wide {
				index, ok = c.uint(2)
				index &= 0x7FFF
			} else {
				index, ok = c.uint(1)
				index &= 0x7F
			}
			if ok && index >= 1 && int(index) <= len(props) && props[index-1] != [2]int{} {
				sizes[uint32(id)] = props[index-1]
			}
		}
	}
	return sizes
}

// isoBoxes yields the type and payload of each box packed in b; a box that does not fit ends the sequence.
func isoBoxes(b []byte) func(yield func(string, []byte) bool) {
	return func(yield func(string, []byte) bool) {
		boxes(b, yield)
	}
}
