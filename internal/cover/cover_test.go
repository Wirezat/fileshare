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

func TestFindOpusPicture(t *testing.T) {
	data := append([]byte("FRONT"), bytes.Repeat([]byte("x"), 400)...)
	field := "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(flacPicture(3, data, uint32(len(data))))
	comment := []byte("OpusTags")
	comment = binary.LittleEndian.AppendUint32(comment, 4)
	comment = append(comment, "test"...)
	comment = binary.LittleEndian.AppendUint32(comment, 1)
	comment = binary.LittleEndian.AppendUint32(comment, uint32(len(field)))
	comment = append(comment, field...)

	got, ok := Find(bytes.NewReader(oggStream([][]byte{append([]byte("OpusHead"), make([]byte, 11)...), comment}, 1)), "song.opus")
	if !ok || !bytes.Equal(got, data) {
		t.Fatalf("Find = %d bytes, %v; want the %d-byte picture", len(got), ok, len(data))
	}
}

func mp4Box(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(typ), body...)...)
}

func m4aFile(picture []byte, mdatSize int) []byte {
	data := mp4Box("data", []byte{0, 0, 0, 14, 0, 0, 0, 0}, picture)
	ilst := mp4Box("ilst", mp4Box("covr", data))
	moov := mp4Box("moov", mp4Box("udta", mp4Box("meta", []byte{0, 0, 0, 0}, ilst)))
	return bytes.Join([][]byte{mp4Box("ftyp", []byte("M4A \x00\x00\x00\x00")), mp4Box("mdat", make([]byte, mdatSize)), moov}, nil)
}

func TestFindM4ACoverBehindMediaData(t *testing.T) {
	picture := append([]byte("\x89PNG"), bytes.Repeat([]byte("x"), 300)...)

	got, ok := Find(bytes.NewReader(m4aFile(picture, 1<<20)), "song.m4a")
	if !ok || !bytes.Equal(got, picture) {
		t.Fatalf("Find = %d bytes, %v; want the %d-byte picture", len(got), ok, len(picture))
	}
}

func TestFindMP4CoverBehindALargeTrack(t *testing.T) {
	picture := append([]byte("\x89PNG"), bytes.Repeat([]byte("x"), 300)...)
	data := mp4Box("data", []byte{0, 0, 0, 14, 0, 0, 0, 0}, picture)
	udta := mp4Box("udta", mp4Box("meta", []byte{0, 0, 0, 0}, mp4Box("ilst", mp4Box("covr", data))))
	file := append(mp4Box("ftyp", []byte("isom")), mp4Box("moov", mp4Box("trak", make([]byte, maxMetadata+1)), udta)...)

	got, ok := Find(bytes.NewReader(file), "film.mp4")
	if !ok || !bytes.Equal(got, picture) {
		t.Fatalf("Find = %d bytes, %v; want the %d-byte picture", len(got), ok, len(picture))
	}
}

func TestFindMP4RefusesUdtaOver16MiB(t *testing.T) {
	udta := append(binary.BigEndian.AppendUint32(nil, maxMetadata+9), "udta"...)
	file := append(mp4Box("ftyp", []byte("M4A ")), mp4Box("moov", udta)...)

	if _, ok := Find(bytes.NewReader(file), "song.m4a"); ok {
		t.Error("udta over 16 MiB was read")
	}
}

func TestFindMP4VideoAndAacWithTag(t *testing.T) {
	picture := append([]byte("\x89PNG"), bytes.Repeat([]byte("x"), 100)...)
	for _, name := range []string{"clip.mp4", "clip.m4v", "book.m4b", "clip.mov"} {
		if got, ok := Find(bytes.NewReader(m4aFile(picture, 100)), name); !ok || !bytes.Equal(got, picture) {
			t.Errorf("Find(%s) = %d bytes, %v; want the picture", name, len(got), ok)
		}
	}
	tag := id3Tag(3, 0, apicV23(3, picture))
	if got, ok := Find(bytes.NewReader(append(tag, "\xff\xf1frames"...)), "song.aac"); !ok || !bytes.Equal(got, picture) {
		t.Errorf("Find(aac with ID3 tag) = %d bytes, %v; want the picture", len(got), ok)
	}
}

func ebml(id []byte, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := append(append([]byte(nil), id...), 0x01)
	size := binary.BigEndian.AppendUint64(nil, uint64(len(body)))
	return append(append(out, size[1:]...), body...)
}

func TestFindMatroskaAttachmentBehindClustersViaSeekHead(t *testing.T) {
	picture := append([]byte("\xff\xd8\xff"), bytes.Repeat([]byte("x"), 200)...)
	attachments := ebml([]byte{0x19, 0x41, 0xA4, 0x69},
		ebml([]byte{0x61, 0xA7},
			ebml([]byte{0x46, 0x6E}, []byte("cover.jpg")),
			ebml([]byte{0x46, 0x60}, []byte("image/jpeg")),
			ebml([]byte{0x46, 0x5C}, picture)))
	cluster := ebml([]byte{0x1F, 0x43, 0xB6, 0x75}, make([]byte, 5000))
	seekHead := func(pos uint64) []byte {
		return ebml([]byte{0x11, 0x4D, 0x9B, 0x74}, ebml([]byte{0x4D, 0xBB},
			ebml([]byte{0x53, 0xAB}, []byte{0x19, 0x41, 0xA4, 0x69}),
			ebml([]byte{0x53, 0xAC}, binary.BigEndian.AppendUint64(nil, pos))))
	}
	head := seekHead(0)
	segment := ebml([]byte{0x18, 0x53, 0x80, 0x67}, seekHead(uint64(len(head)+len(cluster))), cluster, attachments)
	file := append(ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte("matroska"))), segment...)

	got, ok := Find(bytes.NewReader(file), "film.mkv")
	if !ok || !bytes.Equal(got, picture) {
		t.Fatalf("Find = %d bytes, %v; want the %d-byte picture", len(got), ok, len(picture))
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
