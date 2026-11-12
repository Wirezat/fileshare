package cover

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"strings"
)

const pictureField = "METADATA_BLOCK_PICTURE"

// findOgg returns the METADATA_BLOCK_PICTURE of the Vorbis comment header in the Ogg stream r.
func findOgg(r io.Reader) ([]byte, bool) {
	packets, ok := oggPackets(r, 2)
	if !ok || !bytes.HasPrefix(packets[1], []byte("\x03vorbis")) {
		return nil, false
	}
	c := cursor{packets[1][7:]}
	if _, ok := c.prefixed(binary.LittleEndian); !ok {
		return nil, false
	}
	count, ok := c.u32(binary.LittleEndian)
	if !ok {
		return nil, false
	}
	var p picker
	for range count {
		field, ok := c.prefixed(binary.LittleEndian)
		if !ok {
			break
		}
		key, value, found := strings.Cut(string(field), "=")
		if !found || !strings.EqualFold(key, pictureField) {
			continue
		}
		block, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		if picType, data, ok := parsePicture(block); ok {
			p.add(picType, data)
		}
	}
	return p.result()
}

// oggPackets assembles the first n packets of the first logical stream in r, reading at most 16 MiB.
func oggPackets(r io.Reader, n int) ([][]byte, bool) {
	var packets [][]byte
	var cur []byte
	var serial uint32
	budget := int64(maxMetadata)
	for page := 0; len(packets) < n; page++ {
		head, ok := readN(r, 27)
		if !ok || string(head[:4]) != "OggS" {
			return nil, false
		}
		lacing, ok := readN(r, int64(head[26]))
		if !ok {
			return nil, false
		}
		total := int64(0)
		for _, v := range lacing {
			total += int64(v)
		}
		budget -= 27 + int64(len(lacing)) + total
		if budget < 0 {
			return nil, false
		}
		data, ok := readN(r, total)
		if !ok {
			return nil, false
		}
		s := binary.LittleEndian.Uint32(head[14:18])
		if page == 0 {
			serial = s
		}
		if s != serial {
			continue
		}
		for _, v := range lacing {
			cur = append(cur, data[:v]...)
			data = data[v:]
			if v < 255 {
				packets = append(packets, cur)
				cur = nil
				if len(packets) == n {
					break
				}
			}
		}
	}
	return packets, true
}
