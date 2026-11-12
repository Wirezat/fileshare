package cover

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"runtime"
	"testing"
)

func syncsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

func id3Tag(version, flags byte, body []byte) []byte {
	out := append([]byte("ID3"), version, 0, flags)
	out = append(out, syncsafe(len(body))...)
	return append(out, body...)
}

func apicBody(picType byte, data []byte) []byte {
	b := append([]byte{0}, "image/png\x00"...)
	b = append(b, picType)
	b = append(b, "desc\x00"...)
	return append(b, data...)
}

func apicV23(picType byte, data []byte) []byte {
	body := apicBody(picType, data)
	out := []byte("APIC")
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
	out = append(out, 0, 0)
	return append(out, body...)
}

func apicV24(picType byte, data []byte) []byte {
	body := apicBody(picType, data)
	out := append([]byte("APIC"), syncsafe(len(body))...)
	out = append(out, 0, 0)
	return append(out, body...)
}

func flacBlock(typ byte, last bool, body []byte) []byte {
	h := typ
	if last {
		h |= 0x80
	}
	out := []byte{h, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	return append(out, body...)
}

func flacPicture(picType uint32, data []byte, claimedLen uint32) []byte {
	b := binary.BigEndian.AppendUint32(nil, picType)
	b = binary.BigEndian.AppendUint32(b, uint32(len("image/png")))
	b = append(b, "image/png"...)
	b = binary.BigEndian.AppendUint32(b, 0)
	b = append(b, make([]byte, 16)...)
	b = binary.BigEndian.AppendUint32(b, claimedLen)
	return append(b, data...)
}

func flacFile(blocks ...[]byte) []byte {
	out := []byte("fLaC")
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

// oggStream lays packets out as Ogg pages of at most maxSegments lacing values each.
func oggStream(packets [][]byte, maxSegments int) []byte {
	type segment struct{ data []byte }
	var segs []segment
	for _, p := range packets {
		for len(p) >= 255 {
			segs = append(segs, segment{p[:255]})
			p = p[255:]
		}
		segs = append(segs, segment{p})
	}
	var out []byte
	for seq := 0; len(segs) > 0; seq++ {
		n := min(maxSegments, len(segs))
		page := append([]byte("OggS"), 0, 0)
		page = append(page, make([]byte, 8)...)
		page = binary.LittleEndian.AppendUint32(page, 7)
		page = binary.LittleEndian.AppendUint32(page, uint32(seq))
		page = append(page, 0, 0, 0, 0, byte(n))
		for _, s := range segs[:n] {
			page = append(page, byte(len(s.data)))
		}
		for _, s := range segs[:n] {
			page = append(page, s.data...)
		}
		out = append(out, page...)
		segs = segs[n:]
	}
	return out
}

func TestFindMP3PrefersFrontCover(t *testing.T) {
	tag := id3Tag(4, 0, append(apicV24(0, []byte("OTHER")), apicV24(3, []byte("FRONT"))...))

	got, ok := Find(bytes.NewReader(append(tag, "audio"...)), "song.mp3")
	if !ok || string(got) != "FRONT" {
		t.Fatalf("Find = %q, %v; want FRONT", got, ok)
	}
}

func TestFindMP3UndoesTagUnsync(t *testing.T) {
	frame := bytes.ReplaceAll(apicV23(3, []byte{0xff, 0xe0}), []byte{0xff, 0xe0}, []byte{0xff, 0x00, 0xe0})

	got, ok := Find(bytes.NewReader(id3Tag(3, 0x80, frame)), "song.mp3")
	if !ok || !bytes.Equal(got, []byte{0xff, 0xe0}) {
		t.Fatalf("Find = %x, %v; want ffe0", got, ok)
	}
}

func TestFindMP3RefusesTagOver16MiB(t *testing.T) {
	header := append([]byte("ID3"), 3, 0, 0)
	header = append(header, syncsafe(16<<20+1)...)

	if got, ok := Find(bytes.NewReader(header), "song.mp3"); ok {
		t.Fatalf("Find = %q, want no cover", got)
	}
}

func TestFindFLACPicture(t *testing.T) {
	file := flacFile(flacBlock(0, false, make([]byte, 34)), flacBlock(6, true, flacPicture(3, []byte("FRONT"), 5)))

	got, ok := Find(bytes.NewReader(file), "song.flac")
	if !ok || string(got) != "FRONT" {
		t.Fatalf("Find = %q, %v; want FRONT", got, ok)
	}
}

func TestFindFLACHugeLengthAllocatesNothingLarge(t *testing.T) {
	file := flacFile(flacBlock(0, false, make([]byte, 34)), flacBlock(6, true, flacPicture(3, []byte("x"), 0xfffffff0)))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	_, ok := Find(bytes.NewReader(file), "evil.flac")

	runtime.ReadMemStats(&after)
	if ok {
		t.Error("Find reported a cover for a picture longer than its block")
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Errorf("Find allocated %d bytes for a %d-byte file", grown, len(file))
	}
}

func TestFindOggPictureAcrossPages(t *testing.T) {
	data := append([]byte("FRONT"), bytes.Repeat([]byte("x"), 400)...)
	ident := append([]byte("\x01vorbis"), make([]byte, 23)...)
	field := "metadata_block_picture=" + base64.StdEncoding.EncodeToString(flacPicture(3, data, uint32(len(data))))
	comment := []byte("\x03vorbis")
	comment = binary.LittleEndian.AppendUint32(comment, 4)
	comment = append(comment, "test"...)
	comment = binary.LittleEndian.AppendUint32(comment, 1)
	comment = binary.LittleEndian.AppendUint32(comment, uint32(len(field)))
	comment = append(comment, field...)
	comment = append(comment, 1)

	got, ok := Find(bytes.NewReader(oggStream([][]byte{ident, comment}, 1)), "song.ogg")
	if !ok || !bytes.Equal(got, data) {
		t.Fatalf("Find = %d bytes, %v; want the %d-byte picture", len(got), ok, len(data))
	}
}

func TestFindWithoutCover(t *testing.T) {
	if _, ok := Find(bytes.NewReader([]byte("RIFF....WAVE")), "a.wav"); ok {
		t.Error("wav reported a cover")
	}
	if _, ok := Find(bytes.NewReader([]byte("\xff\xfb\x90\x00 frames")), "a.mp3"); ok {
		t.Error("mp3 without tag reported a cover")
	}
}
