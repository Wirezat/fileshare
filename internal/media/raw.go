package media

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	tagCompression     = 0x0103
	tagStripOffsets    = 0x0111
	tagStripByteCounts = 0x0117
	tagSubIFDs         = 0x014A
	tagJpgFromRaw      = 0x002E
	rawMaxIFDs         = 16
	rawMaxEntries      = 1024
	rawMaxSubIFDs      = 8
	cr3UUIDMax         = 1 << 20
	cr3PRVWHeader      = 16
	rafPreviewAt       = 84
)

var (
	cr3MetadataUUID = []byte("\x85\xc0\xb6\x87\x82\x0f\x11\xe0\x81\x11\xf4\xce\x46\x2b\x6a\x48")
	cr3PreviewUUID  = []byte("\xea\xf4\x2b\x5e\x1c\x98\x4b\x88\xb9\xfb\xb7\xdc\x40\x6e\x4d\x16")
)

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	value    [4]byte
}

// tiffRawPreviews returns the JPEGs that the IFDs and SubIFDs of the TIFF-based camera raw file in r locate by
// JPEG offset and length, by a single strip or, in RW2 files, by the JpgFromRaw tag; head holds the first bytes
// of r. Their orientation is the one of IFD0 when it has one.
func tiffRawPreviews(r io.ReadSeeker, head []byte) []preview {
	var bo binary.ByteOrder = binary.LittleEndian
	if head[0] == 'M' {
		bo = binary.BigEndian
	}
	queue := []int64{int64(bo.Uint32(head[4:8]))}
	seen := map[int64]bool{}
	orientation := 0
	var found []preview
	for len(queue) > 0 && len(seen) < rawMaxIFDs {
		off := queue[0]
		queue = queue[1:]
		if off <= 0 || seen[off] {
			continue
		}
		first := len(seen) == 0
		seen[off] = true
		entries, next, ok := readIFD(r, bo, off)
		if !ok {
			continue
		}
		if next != 0 {
			queue = append(queue, next)
		}
		tags := map[uint16]ifdEntry{}
		for _, e := range entries {
			tags[e.tag] = e
		}
		if o, ok := tags[exifTagOrientation]; ok && first {
			orientation = int(entryValue(bo, o))
		}
		if at, ok := tags[exifTagThumbOffset]; ok {
			if n, ok := tags[exifTagThumbLength]; ok {
				found = append(found, preview{at: int64(entryValue(bo, at)), n: int64(entryValue(bo, n))})
			}
		}
		if at, ok := tags[tagStripOffsets]; ok && at.count == 1 {
			if n, ok := tags[tagStripByteCounts]; ok && n.count == 1 {
				found = append(found, preview{at: int64(entryValue(bo, at)), n: int64(entryValue(bo, n))})
			}
		}
		if e, ok := tags[tagJpgFromRaw]; ok && e.count > 4 {
			found = append(found, preview{at: int64(bo.Uint32(e.value[:])), n: int64(e.count)})
		}
		if e, ok := tags[tagSubIFDs]; ok {
			queue = append(queue, subIFDs(r, bo, e)...)
		}
	}
	if orientation < 1 || orientation > 8 {
		orientation = 0
	}
	for i := range found {
		found[i].orientation = orientation
	}
	return found
}

// readIFD reads the entries of the IFD at offset off of r and the offset of the next IFD, 0 for none.
func readIFD(r io.ReadSeeker, bo binary.ByteOrder, off int64) (entries []ifdEntry, next int64, ok bool) {
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		return nil, 0, false
	}
	var count [2]byte
	if _, err := io.ReadFull(r, count[:]); err != nil {
		return nil, 0, false
	}
	n := int(bo.Uint16(count[:]))
	if n > rawMaxEntries {
		return nil, 0, false
	}
	b := make([]byte, 12*n+4)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, 0, false
	}
	for i := range n {
		e := b[12*i:]
		entry := ifdEntry{tag: bo.Uint16(e), typ: bo.Uint16(e[2:]), count: bo.Uint32(e[4:])}
		copy(entry.value[:], e[8:12])
		entries = append(entries, entry)
	}
	return entries, int64(bo.Uint32(b[12*n:])), true
}

// entryValue returns the first value of an IFD entry of type SHORT or LONG.
func entryValue(bo binary.ByteOrder, e ifdEntry) uint32 {
	if e.typ == 3 {
		return uint32(bo.Uint16(e.value[:]))
	}
	return bo.Uint32(e.value[:])
}

// subIFDs returns the offsets that the SubIFDs entry e lists, at most rawMaxSubIFDs.
func subIFDs(r io.ReadSeeker, bo binary.ByteOrder, e ifdEntry) []int64 {
	if e.count == 1 {
		return []int64{int64(entryValue(bo, e))}
	}
	n := min(int(e.count), rawMaxSubIFDs)
	if _, err := r.Seek(int64(bo.Uint32(e.value[:])), io.SeekStart); err != nil {
		return nil
	}
	b := make([]byte, 4*n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil
	}
	offs := make([]int64, n)
	for i := range offs {
		offs[i] = int64(bo.Uint32(b[4*i:]))
	}
	return offs
}

// cr3Previews returns the PRVW preview of the Canon CR3 file in r, with the orientation of the CMT1 metadata in moov.
func cr3Previews(r io.ReadSeeker) []preview {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	orientation := 0
	var found []preview
	for range bmffMaxBoxes {
		typ, n, ok := bmffNext(r)
		if !ok || n < 0 {
			break
		}
		start, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			break
		}
		switch typ {
		case "moov":
			if o, ok := cr3Orientation(r, n); ok {
				orientation = o
			}
		case "uuid":
			if p, ok := cr3PRVW(r, n); ok {
				found = append(found, p)
			}
		}
		if _, err := r.Seek(start+n, io.SeekStart); err != nil {
			break
		}
	}
	for i := range found {
		found[i].orientation = orientation
	}
	return found
}

// cr3Orientation returns the orientation of the CMT1 box in the Canon uuid box of the moov payload of length n at r.
func cr3Orientation(r io.ReadSeeker, n int64) (int, bool) {
	end, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, false
	}
	end += n
	for range bmffMaxBoxes {
		typ, size, ok := bmffNext(r)
		if !ok || size < 0 {
			return 0, false
		}
		start, _ := r.Seek(0, io.SeekCurrent)
		if start+size > end {
			return 0, false
		}
		if typ == "uuid" && size >= 16 && size <= cr3UUIDMax {
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return 0, false
			}
			if bytes.Equal(b[:16], cr3MetadataUUID) {
				o := 0
				boxes(b[16:], func(typ string, p []byte) bool {
					if typ == "CMT1" {
						o = tiffOrientation(p)
					}
					return o == 0
				})
				return o, o != 0
			}
		}
		if _, err := r.Seek(start+size, io.SeekStart); err != nil {
			return 0, false
		}
	}
	return 0, false
}

// cr3PRVW returns the JPEG of the PRVW box in the uuid payload of length n at r when it is the Canon preview uuid.
func cr3PRVW(r io.ReadSeeker, n int64) (preview, bool) {
	var head [16 + 8 + 8 + cr3PRVWHeader]byte
	if n < int64(len(head)) {
		return preview{}, false
	}
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return preview{}, false
	}
	if _, err := io.ReadFull(r, head[:]); err != nil || !bytes.Equal(head[:16], cr3PreviewUUID) || string(head[28:32]) != "PRVW" {
		return preview{}, false
	}
	length := int64(binary.BigEndian.Uint32(head[len(head)-4:]))
	return preview{at: start + int64(len(head)), n: length}, true
}

// rafPreviews returns the JPEG that the header of the Fujifilm RAF file in r locates.
func rafPreviews(r io.ReadSeeker) []preview {
	if _, err := r.Seek(rafPreviewAt, io.SeekStart); err != nil {
		return nil
	}
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return nil
	}
	return []preview{{at: int64(binary.BigEndian.Uint32(b[:4])), n: int64(binary.BigEndian.Uint32(b[4:]))}}
}
