package filetype

import "bytes"

// DetectBytes is the number of leading file bytes Detect looks at.
const DetectBytes = 64

type signature struct {
	offset int
	magic  string
	ext    string
}

var signatures = []signature{
	{0, "\x89PNG\r\n\x1a\n", "png"},
	{0, "\xff\xd8\xff", "jpg"},
	{0, "GIF8", "gif"},
	{0, "%PDF-", "pdf"},
	{0, "FUJIFILMCCD-RAW", "raf"},
	{0, "8BPS", "psd"},
	{0, "II*\x00", "tif"},
	{0, "MM\x00*", "tif"},
	{0, "fLaC", "flac"},
	{0, "ID3", "mp3"},
	{0, "\xff\xfb", "mp3"},
	{0, "\xff\xf3", "mp3"},
	{0, "\xff\xf2", "mp3"},
	{0, "\xff\xf1", "aac"},
	{0, "\xff\xf9", "aac"},
	{0, "PK\x03\x04", "zip"},
	{0, "Rar!", "rar"},
	{0, "7z\xbc\xaf\x27\x1c", "7z"},
	{0, "\x1f\x8b", "gz"},
}

var ftypBrands = map[string]string{
	"M4A ": "m4a", "M4B ": "m4b", "M4V ": "m4v", "qt  ": "mov", "crx ": "cr3",
	"heic": "heic", "heix": "heic", "mif1": "heic", "msf1": "heic",
	"avif": "avif", "avis": "avif",
}

// Detect returns the extension of the format whose signature starts head, the first bytes of a file,
// or "" when none matches. Only the first DetectBytes bytes count.
func Detect(head []byte) string {
	h := head[:min(len(head), DetectBytes)]
	for _, s := range signatures {
		if bytes.HasPrefix(h[min(s.offset, len(h)):], []byte(s.magic)) {
			return s.ext
		}
	}
	switch {
	case len(h) >= 12 && string(h[:4]) == "RIFF":
		return map[string]string{"WEBP": "webp", "WAVE": "wav", "AVI ": "avi"}[string(h[8:12])]
	case len(h) >= 12 && string(h[4:8]) == "ftyp":
		if ext, ok := ftypBrands[string(h[8:12])]; ok {
			return ext
		}
		return "mp4"
	case bytes.HasPrefix(h, []byte("\x1a\x45\xdf\xa3")):
		if bytes.Contains(h, []byte("webm")) {
			return "webm"
		}
		return "mkv"
	case bytes.HasPrefix(h, []byte("OggS")):
		if bytes.Contains(h, []byte("OpusHead")) {
			return "opus"
		}
		return "ogg"
	}
	return ""
}
