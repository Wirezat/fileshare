package filetype

import (
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	Image   = "image"
	Video   = "video"
	Audio   = "audio"
	PDF     = "pdf"
	Office  = "office"
	Text    = "text"
	Unknown = "unknown"
)

const (
	IconImage    = "image"
	IconVideo    = "video"
	IconAudio    = "audio"
	IconPDF      = "pdf"
	IconDoc      = "doc"
	IconSheet    = "sheet"
	IconSlides   = "slides"
	IconText     = "text"
	IconMarkdown = "markdown"
	IconCode     = "code"
	IconArchive  = "archive"
	IconFile     = "file"
	IconFolder   = "folder"
)

const (
	ViewImage    = "image"
	ViewVideo    = "video"
	ViewAudio    = "audio"
	ViewPDF      = "pdf"
	ViewOffice   = "office"
	ViewText     = "text"
	ViewMarkdown = "markdown"
	ViewHTML     = "html"
	ViewProbe    = "probe"
	ViewInfo     = "info"
)

const sniffBytes = 1024

// Type describes what the server and the listing know about one file name.
type Type struct {
	Kind       string
	Icon       string
	Thumbnail  bool
	OfficeType string
	view       string
}

var byExt = buildTable()

func buildTable() map[string]Type {
	m := map[string]Type{}
	add := func(t Type, exts ...string) {
		for _, e := range exts {
			m[e] = t
		}
	}
	add(Type{Kind: Image, Icon: IconImage, Thumbnail: true, view: ViewImage}, "jpg", "jpeg", "png", "gif", "webp")
	add(Type{Kind: Image, Icon: IconImage, view: ViewImage}, "avif")
	add(Type{Kind: Video, Icon: IconVideo, view: ViewVideo}, "mp4", "mov", "webm", "mkv", "avi", "wmv")
	add(Type{Kind: Audio, Icon: IconAudio, view: ViewAudio}, "mp3", "wav", "flac", "aac", "ogg")
	add(Type{Kind: PDF, Icon: IconPDF, view: ViewPDF}, "pdf")
	add(Type{Kind: Office, Icon: IconDoc, OfficeType: "word", view: ViewOffice}, "docx", "doc", "odt")
	add(Type{Kind: Office, Icon: IconSheet, OfficeType: "cell", view: ViewOffice}, "xlsx", "xls", "ods")
	add(Type{Kind: Office, Icon: IconSlides, OfficeType: "slide", view: ViewOffice}, "pptx", "ppt", "odp")
	add(Type{Kind: Unknown, Icon: IconArchive, view: ViewInfo}, "zip", "rar", "7z", "tar", "gz")
	add(Type{Kind: Text, Icon: IconCode, view: ViewText}, "json", "xml", "yaml", "yml", "toml", "ini", "cfg", "conf",
		"sh", "py", "go", "js", "ts", "css", "java", "c", "h", "cpp", "rs", "sql")
	add(Type{Kind: Text, Icon: IconCode, view: ViewHTML}, "html")
	add(Type{Kind: Text, Icon: IconText, view: ViewText}, "txt", "log", "csv")
	add(Type{Kind: Text, Icon: IconMarkdown, view: ViewMarkdown}, "md")
	return m
}

// Lookup returns the Type for name, chosen by its lower-cased extension.
// Names without a known extension get Kind Unknown, the file icon and the probe view.
func Lookup(name string) Type {
	if t, ok := byExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]; ok {
		return t
	}
	return Type{Kind: Unknown, Icon: IconFile, view: ViewProbe}
}

// ViewFor returns how the share page shows name; officeOn reports whether the share has a usable office suite,
// without one office documents get the info view.
func ViewFor(name string, officeOn bool) string {
	v := Lookup(name).view
	if v == ViewOffice && !officeOn {
		return ViewInfo
	}
	return v
}

// Sniff reports whether head, the start of a file, looks like text: no NUL byte and valid UTF-8 in the first 1024 bytes,
// where a sequence cut off at the end still counts as valid.
func Sniff(head []byte) bool {
	h := head[:min(len(head), sniffBytes)]
	if bytes.IndexByte(h, 0) >= 0 {
		return false
	}
	return utf8.Valid(TrimIncompleteRune(h))
}

// TrimIncompleteRune returns b without a UTF-8 sequence cut off at its end; any other b is returned unchanged.
func TrimIncompleteRune(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			return b
		}
	}
	return b
}
