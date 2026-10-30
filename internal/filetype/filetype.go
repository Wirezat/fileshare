package filetype

import (
	"path/filepath"
	"strings"
	"unicode/utf16"
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

const unknownIcon = "📎"

// Type describes what the server and the listing know about one file name.
type Type struct {
	Kind       string
	Icon       string
	Label      string
	Thumbnail  bool
	OfficeType string
}

var byExt = buildTable()

func buildTable() map[string]Type {
	m := map[string]Type{}
	add := func(t Type, exts ...string) {
		for _, e := range exts {
			m[e] = t
		}
	}
	add(Type{Kind: Image, Thumbnail: true}, "jpg", "jpeg", "png", "gif", "webp")
	add(Type{Kind: Image, Thumbnail: true, Label: "JPG"}, "jpeg")
	add(Type{Kind: Image}, "avif")
	add(Type{Kind: Video}, "mp4", "mov", "webm", "mkv", "avi", "wmv")
	add(Type{Kind: Audio}, "mp3", "wav", "flac", "aac", "ogg")
	add(Type{Kind: PDF, Icon: "📄"}, "pdf")
	add(Type{Kind: Office, Icon: "📄", OfficeType: "word"}, "docx", "doc", "odt")
	add(Type{Kind: Office, Icon: "📊", OfficeType: "cell"}, "xlsx", "xls", "ods")
	add(Type{Kind: Office, Icon: "📽️", OfficeType: "slide"}, "pptx", "ppt", "odp")
	add(Type{Kind: Unknown, Icon: "📦"}, "zip", "rar", "7z", "tar", "gz")
	add(Type{Kind: Text, Icon: unknownIcon}, "json", "xml", "yaml", "yml", "toml", "ini", "cfg", "conf",
		"sh", "py", "go", "js", "ts", "css", "html", "java", "c", "h", "cpp", "rs", "sql")
	add(Type{Kind: Text, Icon: "📝"}, "txt", "md", "log")
	add(Type{Kind: Text, Icon: "📊"}, "csv")
	return m
}

// Lookup returns the Type for name, chosen by its lower-cased extension.
// Names without a known extension get Kind Unknown, a paperclip icon and a label cut from the extension.
func Lookup(name string) Type {
	t, ok := byExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]
	if !ok {
		t = Type{Kind: Unknown, Icon: unknownIcon}
	}
	if t.Label == "" {
		t.Label = (name)
	}
	return t
}

func fallbackLabel(name string) string {
	ext := strings.ToUpper(name[strings.LastIndex(name, ".")+1:])
	units := utf16.Encode([]rune(ext))
	if len(units) > 4 {
		units = units[:4]
	}
	if len(units) == 0 {
		return "FILE"
	}
	return string(utf16.Decode(units))
}
