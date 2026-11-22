package cover

import (
	"bytes"
	"io"
	"strings"
)

const (
	idEBML         = 0x1A45DFA3
	idSegment      = 0x18538067
	idSeekHead     = 0x114D9B74
	idSeek         = 0x4DBB
	idSeekID       = 0x53AB
	idSeekPosition = 0x53AC
	idCluster      = 0x1F43B675
	idAttachments  = 0x1941A469
	idAttachedFile = 0x61A7
	idFileName     = 0x466E
	idFileMime     = 0x4660
	idFileData     = 0x465C
	maxMatroskaTop = 64
	unknownSize    = ^uint64(0)
)

// findMatroska returns the cover attachment of the Matroska or WebM stream r: a JPEG or PNG attachment named cover*,
// else the first JPEG or PNG attachment. It finds Attachments among the level-1 elements before the first Cluster or
// through the SeekHead; an Attachments element over 16 MiB gives false.
func findMatroska(r io.Reader) ([]byte, bool) {
	rs, seekable := r.(io.ReadSeeker)
	id, size, ok := readElement(r)
	if !seekable || !ok || id != idEBML || !skip(r, int64(size)) {
		return nil, false
	}
	if id, _, ok = readElement(r); !ok || id != idSegment {
		return nil, false
	}
	segment, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, false
	}
	for range maxMatroskaTop {
		id, size, ok := readElement(r)
		if !ok || size == unknownSize || id == idCluster {
			return nil, false
		}
		switch id {
		case idAttachments:
			return readAttachments(r, size)
		case idSeekHead:
			if size > maxMetadata {
				return nil, false
			}
			body, ok := readN(r, int64(size))
			if !ok {
				return nil, false
			}
			pos, ok := seekPosition(body, idAttachments)
			if !ok {
				continue
			}
			if _, err := rs.Seek(segment+int64(pos), io.SeekStart); err != nil {
				return nil, false
			}
			if id, size, ok := readElement(r); ok && id == idAttachments {
				return readAttachments(r, size)
			}
			return nil, false
		default:
			if !skip(r, int64(size)) {
				return nil, false
			}
		}
	}
	return nil, false
}

// readAttachments reads the Attachments payload of size at r and returns its cover; over 16 MiB gives false.
func readAttachments(r io.Reader, size uint64) ([]byte, bool) {
	if size > maxMetadata {
		return nil, false
	}
	body, ok := readN(r, int64(size))
	if !ok {
		return nil, false
	}
	return coverInAttachments(body)
}

// seekPosition returns the segment-relative position that the SeekHead payload b gives for element target.
func seekPosition(b []byte, target uint32) (uint64, bool) {
	r := bytes.NewReader(b)
	for {
		id, size, ok := readElement(r)
		if !ok || size == unknownSize || size > uint64(r.Len()) {
			return 0, false
		}
		entry, _ := readN(r, int64(size))
		if id != idSeek {
			continue
		}
		var seekID uint32
		var pos uint64
		found := false
		er := bytes.NewReader(entry)
		for {
			eid, esize, ok := readElement(er)
			if !ok || esize == unknownSize || esize > uint64(er.Len()) {
				break
			}
			value, _ := readN(er, int64(esize))
			switch eid {
			case idSeekID:
				for _, c := range value {
					seekID = seekID<<8 | uint32(c)
				}
			case idSeekPosition:
				for _, c := range value {
					pos = pos<<8 | uint64(c)
				}
				found = true
			}
		}
		if seekID == target && found {
			return pos, true
		}
	}
}

// readElement reads an EBML element header from r: its ID with the length marker kept and its payload size,
// unknownSize for the reserved all-ones size; false when r ends early or the header is malformed.
func readElement(r io.Reader) (id uint32, size uint64, ok bool) {
	first, ok := readN(r, 1)
	if !ok || first[0] == 0 {
		return 0, 0, false
	}
	idLen := leadingZeros(first[0]) + 1
	if idLen > 4 {
		return 0, 0, false
	}
	rest, ok := readN(r, int64(idLen-1))
	if !ok {
		return 0, 0, false
	}
	for _, b := range append(first, rest...) {
		id = id<<8 | uint32(b)
	}
	sz, ok := readN(r, 1)
	if !ok || sz[0] == 0 {
		return 0, 0, false
	}
	sizeLen := leadingZeros(sz[0]) + 1
	more, ok := readN(r, int64(sizeLen-1))
	if !ok {
		return 0, 0, false
	}
	size = uint64(sz[0]) & (0xFF >> sizeLen)
	allOnes := size == uint64(0xFF>>sizeLen)
	for _, b := range more {
		size = size<<8 | uint64(b)
		allOnes = allOnes && b == 0xFF
	}
	if allOnes {
		return id, unknownSize, true
	}
	return id, size, true
}

// leadingZeros returns the number of zero bits before the first one bit of b, 0 to 7.
func leadingZeros(b byte) int {
	n := 0
	for mask := byte(0x80); b&mask == 0 && n < 7; mask >>= 1 {
		n++
	}
	return n
}

// coverInAttachments returns the cover among the AttachedFile elements packed in the Attachments payload.
func coverInAttachments(b []byte) ([]byte, bool) {
	var first []byte
	r := bytes.NewReader(b)
	for {
		id, size, ok := readElement(r)
		if !ok || size == unknownSize || size > uint64(r.Len()) {
			return first, first != nil
		}
		file, _ := readN(r, int64(size))
		if id != idAttachedFile {
			continue
		}
		name, mime, data := attachedFile(file)
		if data == nil || (mime != "image/jpeg" && mime != "image/png") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), "cover") {
			return data, true
		}
		if first == nil {
			first = data
		}
	}
}

// attachedFile returns the name, MIME type and data of an AttachedFile payload; missing parts are empty.
func attachedFile(b []byte) (name, mime string, data []byte) {
	r := bytes.NewReader(b)
	for {
		id, size, ok := readElement(r)
		if !ok || size == unknownSize || size > uint64(r.Len()) {
			return
		}
		value, _ := readN(r, int64(size))
		switch id {
		case idFileName:
			name = string(value)
		case idFileMime:
			mime = string(value)
		case idFileData:
			data = value
		}
	}
}
