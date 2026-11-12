package cover

import "io"

const flacPictureBlock = 6

// findFLAC returns the PICTURE block of the FLAC stream in r, reading at most 16 MiB of metadata.
func findFLAC(r io.Reader) ([]byte, bool) {
	magic, ok := readN(r, 4)
	if !ok || string(magic) != "fLaC" {
		return nil, false
	}
	var p picker
	budget := int64(maxMetadata)
	for {
		head, ok := readN(r, 4)
		if !ok {
			return nil, false
		}
		last, typ := head[0]&0x80 != 0, head[0]&0x7f
		n := int64(head[1])<<16 | int64(head[2])<<8 | int64(head[3])
		if n > budget {
			return nil, false
		}
		budget -= n
		if typ == flacPictureBlock {
			block, ok := readN(r, n)
			if !ok {
				return nil, false
			}
			if picType, data, ok := parsePicture(block); ok {
				p.add(picType, data)
			}
		} else if _, err := io.CopyN(io.Discard, r, n); err != nil {
			return nil, false
		}
		if last {
			return p.result()
		}
	}
}
