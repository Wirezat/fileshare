package media

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"image"
	"io"
	"strconv"
	"strings"
)

const (
	bmffMaxBoxes   = 64
	tkhdMax        = 128
	svgHeadMax     = 64 << 10
	ebmlMaxTop     = 64
	ebmlTracksMax  = 1 << 20
	ebmlUnknown    = ^uint64(0)
	idEBML         = 0x1A45DFA3
	idSegment      = 0x18538067
	idTracks       = 0x1654AE6B
	idTrackEntry   = 0xAE
	idVideo        = 0xE0
	idPixelWidth   = 0xB0
	idPixelHeight  = 0xBA
	idDisplayWidth = 0x54B0
	idDisplayHigh  = 0x54BA
)

// AspectRatio returns width divided by height of the image or video in r as it is shown, read from its header
// and chosen by name's extension: jpg, png, gif, webp, heic, heif, avif, svg, mp4, m4v, mov, mkv or webm.
// A JPEG's Exif orientation and a HEIF rotation or video track rotation by 90 degrees are applied.
// ok is false for any other format or a header without a usable size.
func AspectRatio(r io.ReadSeeker, name string) (ratio float64, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, false
	}
	var w, h float64
	switch ext(name) {
	case "jpg", "jpeg", "png", "gif", "webp":
		cfg, _, err := image.DecodeConfig(r)
		if err != nil {
			return 0, false
		}
		w, h = float64(cfg.Width), float64(cfg.Height)
		if o := ExifOrientation(r); o >= 5 {
			w, h = h, w
		}
	case "heic", "heif", "avif":
		w, h, ok = heifSize(r)
	case "svg":
		w, h, ok = svgSize(r)
	case "mp4", "m4v", "mov":
		w, h, ok = mp4Size(r)
	case "mkv", "webm":
		w, h, ok = matroskaSize(r)
	}
	if w <= 0 || h <= 0 {
		return 0, false
	}
	return w / h, true
}

// bmffNext reads the header of the next ISO base media box from r.
// It returns the box type and the payload length, -1 when the box runs to the end of the file.
func bmffNext(r io.Reader) (typ string, payload int64, ok bool) {
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return "", 0, false
	}
	size := uint64(binary.BigEndian.Uint32(head[:4]))
	header := uint64(8)
	switch size {
	case 0:
		return string(head[4:]), -1, true
	case 1:
		var large [8]byte
		if _, err := io.ReadFull(r, large[:]); err != nil {
			return "", 0, false
		}
		size, header = binary.BigEndian.Uint64(large[:]), 16
	}
	if size < header || size-header > 1<<62 {
		return "", 0, false
	}
	return string(head[4:]), int64(size - header), true
}

// bmffChild reads the boxes of r up to limit bytes and returns the payload length of the first one named typ,
// leaving r at that payload.
func bmffChild(r io.ReadSeeker, typ string, limit int64) (payload int64, ok bool) {
	for range bmffMaxBoxes {
		name, n, ok := bmffNext(r)
		if !ok || (n < 0 && name != typ) {
			return 0, false
		}
		if name == typ {
			if n < 0 {
				n = limit
			}
			return n, true
		}
		if _, err := r.Seek(n, io.SeekCurrent); err != nil {
			return 0, false
		}
	}
	return 0, false
}

// boxes calls fn for the type and payload of each box packed in b until fn returns false.
func boxes(b []byte, fn func(typ string, payload []byte) bool) {
	for len(b) >= 8 {
		size := uint64(binary.BigEndian.Uint32(b))
		if size < 8 || size > uint64(len(b)) {
			return
		}
		if !fn(string(b[4:8]), b[8:size]) {
			return
		}
		b = b[size:]
	}
}

// heifSize returns the largest image size of the ispe properties in the meta box of the HEIF or AVIF file r,
// swapped when an irot property turns it by 90 or 270 degrees.
func heifSize(r io.ReadSeeker) (w, h float64, ok bool) {
	meta, ok := heifMeta(r)
	if !ok {
		return 0, 0, false
	}
	rotated := false
	boxes(meta[4:], func(typ string, iprp []byte) bool {
		if typ != "iprp" {
			return true
		}
		boxes(iprp, func(typ string, ipco []byte) bool {
			if typ != "ipco" {
				return true
			}
			boxes(ipco, func(typ string, p []byte) bool {
				switch {
				case typ == "ispe" && len(p) >= 12:
					pw, ph := float64(binary.BigEndian.Uint32(p[4:])), float64(binary.BigEndian.Uint32(p[8:]))
					if pw*ph > w*h {
						w, h = pw, ph
					}
				case typ == "irot" && len(p) >= 1:
					rotated = p[0]&1 == 1
				}
				return true
			})
			return false
		})
		return false
	})
	if rotated {
		w, h = h, w
	}
	return w, h, w > 0 && h > 0
}

// mp4Size returns the width and height of the first track header in the moov box of the MP4 or QuickTime file r
// with a non-zero size, swapped when its matrix turns it by 90 or 270 degrees.
func mp4Size(r io.ReadSeeker) (w, h float64, ok bool) {
	moov, ok := bmffChild(r, "moov", 0)
	if !ok {
		return 0, 0, false
	}
	for moov > 0 {
		start, _ := r.Seek(0, io.SeekCurrent)
		trak, ok := bmffChild(r, "trak", moov)
		if !ok {
			return 0, 0, false
		}
		trakStart, _ := r.Seek(0, io.SeekCurrent)
		if n, ok := bmffChild(r, "tkhd", trak); ok && n <= tkhdMax {
			body := make([]byte, n)
			if _, err := io.ReadFull(r, body); err == nil {
				if w, h, ok := tkhdSize(body); ok {
					return w, h, true
				}
			}
		}
		if _, err := r.Seek(trakStart+trak, io.SeekStart); err != nil {
			return 0, 0, false
		}
		moov -= trakStart + trak - start
	}
	return 0, 0, false
}

// tkhdSize returns the shown width and height of the track header payload b; ok is false for a zero size.
func tkhdSize(b []byte) (w, h float64, ok bool) {
	matrix := 40
	if len(b) > 0 && b[0] == 1 {
		matrix = 52
	}
	if len(b) < matrix+44 {
		return 0, 0, false
	}
	w = float64(binary.BigEndian.Uint32(b[matrix+36:])) / 65536
	h = float64(binary.BigEndian.Uint32(b[matrix+40:])) / 65536
	if binary.BigEndian.Uint32(b[matrix:]) == 0 && binary.BigEndian.Uint32(b[matrix+4:]) != 0 {
		w, h = h, w
	}
	return w, h, w > 0 && h > 0
}

// svgSize returns the width and height attributes of the svg root element in r, or the size of its viewBox
// when they are missing or relative.
func svgSize(r io.Reader) (w, h float64, ok bool) {
	d := xml.NewDecoder(io.LimitReader(r, svgHeadMax))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err != nil {
			return 0, 0, false
		}
		el, isStart := tok.(xml.StartElement)
		if !isStart {
			continue
		}
		if el.Name.Local != "svg" {
			return 0, 0, false
		}
		attr := map[string]string{}
		for _, a := range el.Attr {
			attr[a.Name.Local] = a.Value
		}
		if w, h := svgLength(attr["width"]), svgLength(attr["height"]); w > 0 && h > 0 {
			return w, h, true
		}
		box := strings.FieldsFunc(attr["viewBox"], func(c rune) bool { return c == ' ' || c == ',' })
		if len(box) != 4 {
			return 0, 0, false
		}
		w, _ = strconv.ParseFloat(box[2], 64)
		h, _ = strconv.ParseFloat(box[3], 64)
		return w, h, w > 0 && h > 0
	}
}

// svgLength returns the number of an absolute SVG length such as "40", "40px" or "4cm", 0 for a percentage or no number.
func svgLength(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimRight(s, "abcdefghijklmnopqrstuvwxyz"), 64)
	return v
}

// ebmlVint reads an EBML variable-length integer from r; keepMarker keeps the length marker bit as element IDs do.
// It returns the value, ebmlUnknown for a size with all value bits set.
func ebmlVint(r io.Reader, keepMarker bool) (v uint64, ok bool) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:1]); err != nil || b[0] == 0 {
		return 0, false
	}
	n := 1
	for b[0]&(0x80>>(n-1)) == 0 {
		n++
	}
	if _, err := io.ReadFull(r, b[1:n]); err != nil {
		return 0, false
	}
	allOnes := uint64(1)<<(7*n) - 1
	if !keepMarker {
		b[0] &^= 0x80 >> (n - 1)
	}
	for i := range n {
		v = v<<8 | uint64(b[i])
	}
	if !keepMarker && v == allOnes {
		return ebmlUnknown, true
	}
	return v, true
}

// ebmlElement reads the ID and payload size of the next EBML element from r.
func ebmlElement(r io.Reader) (id, size uint64, ok bool) {
	if id, ok = ebmlVint(r, true); !ok {
		return 0, 0, false
	}
	size, ok = ebmlVint(r, false)
	return id, size, ok
}

// ebmlChildren calls fn for the ID and payload of each element packed in b until fn returns false.
func ebmlChildren(b []byte, fn func(id uint64, payload []byte) bool) {
	r := bytes.NewReader(b)
	for {
		id, size, ok := ebmlElement(r)
		if !ok || size > uint64(r.Len()) {
			return
		}
		payload := b[len(b)-r.Len():][:size]
		r.Seek(int64(size), io.SeekCurrent)
		if !fn(id, payload) {
			return
		}
	}
}

// ebmlUint returns the big-endian unsigned integer stored in b.
func ebmlUint(b []byte) float64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return float64(v)
}

// matroskaSize returns the display size, else the pixel size, of the first video track in the Tracks element
// of the Matroska or WebM file r. Level-1 elements before Tracks are skipped.
func matroskaSize(r io.ReadSeeker) (w, h float64, ok bool) {
	id, size, ok := ebmlElement(r)
	if !ok || id != idEBML || size == ebmlUnknown {
		return 0, 0, false
	}
	if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
		return 0, 0, false
	}
	if id, _, ok = ebmlElement(r); !ok || id != idSegment {
		return 0, 0, false
	}
	for range ebmlMaxTop {
		id, size, ok := ebmlElement(r)
		if !ok || size == ebmlUnknown || size > 1<<62 {
			return 0, 0, false
		}
		if id != idTracks {
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return 0, 0, false
			}
			continue
		}
		if size > ebmlTracksMax {
			return 0, 0, false
		}
		tracks := make([]byte, size)
		if _, err := io.ReadFull(r, tracks); err != nil {
			return 0, 0, false
		}
		ebmlChildren(tracks, func(id uint64, entry []byte) bool {
			if id == idTrackEntry {
				ebmlChildren(entry, func(id uint64, video []byte) bool {
					if id == idVideo {
						w, h = videoSize(video)
					}
					return id != idVideo
				})
			}
			return w <= 0 || h <= 0
		})
		return w, h, w > 0 && h > 0
	}
	return 0, 0, false
}

// videoSize returns the display size of the Matroska Video element payload b, else its pixel size.
func videoSize(b []byte) (w, h float64) {
	var pw, ph, dw, dh float64
	ebmlChildren(b, func(id uint64, v []byte) bool {
		switch id {
		case idPixelWidth:
			pw = ebmlUint(v)
		case idPixelHeight:
			ph = ebmlUint(v)
		case idDisplayWidth:
			dw = ebmlUint(v)
		case idDisplayHigh:
			dh = ebmlUint(v)
		}
		return true
	})
	if dw > 0 && dh > 0 {
		return dw, dh
	}
	return pw, ph
}
