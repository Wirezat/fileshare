package filetype

import (
	"strings"
	"testing"
)

func TestLookup(t *testing.T) {
	cases := []struct {
		name string
		want Type
	}{
		{"a.jpg", Type{Kind: Image, Icon: IconImage, Thumbnail: true}},
		{"a.avif", Type{Kind: Image, Icon: IconImage}},
		{"a.mp4", Type{Kind: Video, Icon: IconVideo}},
		{"a.flac", Type{Kind: Audio, Icon: IconAudio}},
		{"a.pdf", Type{Kind: PDF, Icon: IconPDF}},
		{"A.DOCX", Type{Kind: Office, Icon: IconDoc, OfficeType: "word"}},
		{"a.xlsx", Type{Kind: Office, Icon: IconSheet, OfficeType: "cell"}},
		{"a.pptx", Type{Kind: Office, Icon: IconSlides, OfficeType: "slide"}},
		{"a.txt", Type{Kind: Text, Icon: IconText}},
		{"a.csv", Type{Kind: Text, Icon: IconText}},
		{"a.md", Type{Kind: Text, Icon: IconMarkdown}},
		{"a.html", Type{Kind: Text, Icon: IconCode}},
		{"a.go", Type{Kind: Text, Icon: IconCode}},
		{"a.xml", Type{Kind: Text, Icon: IconCode}},
		{"a.tar.gz", Type{Kind: Unknown, Icon: IconArchive}},
		{"Makefile", Type{Kind: Unknown, Icon: IconFile}},
		{".env", Type{Kind: Unknown, Icon: IconFile}},
		{"", Type{Kind: Unknown, Icon: IconFile}},
	}
	for _, tc := range cases {
		got := Lookup(tc.name)
		got.view = ""
		if got != tc.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestViewFor(t *testing.T) {
	cases := []struct {
		name    string
		off, on string
	}{
		{"a.png", ViewImage, ViewImage},
		{"a.mov", ViewVideo, ViewVideo},
		{"a.ogg", ViewAudio, ViewAudio},
		{"a.pdf", ViewPDF, ViewPDF},
		{"a.odt", ViewInfo, ViewOffice},
		{"a.ods", ViewInfo, ViewOffice},
		{"a.log", ViewText, ViewText},
		{"a.md", ViewMarkdown, ViewMarkdown},
		{"a.html", ViewHTML, ViewHTML},
		{"a.py", ViewText, ViewText},
		{"a.zip", ViewInfo, ViewInfo},
		{"Makefile", ViewProbe, ViewProbe},
		{"a.svg", ViewProbe, ViewProbe},
	}
	for _, tc := range cases {
		if got := ViewFor(tc.name, false); got != tc.off {
			t.Errorf("ViewFor(%q, false) = %q, want %q", tc.name, got, tc.off)
		}
		if got := ViewFor(tc.name, true); got != tc.on {
			t.Errorf("ViewFor(%q, true) = %q, want %q", tc.name, got, tc.on)
		}
	}
}

func TestSniff(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want bool
	}{
		{"text", []byte("hello\nworld"), true},
		{"nul byte", []byte("ab\x00cd"), false},
		{"invalid utf-8", []byte("\xff\xfe"), false},
		{"sequence cut at the end", []byte("abc\xc3"), true},
		{"broken sequence inside", []byte("abc\xc3x"), false},
		{"sequence across byte 1024", []byte(strings.Repeat("a", 1023) + "é"), true},
		{"empty", nil, true},
	}
	for _, tc := range cases {
		if got := Sniff(tc.head); got != tc.want {
			t.Errorf("Sniff(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTextExtensions(t *testing.T) {
	for _, ext := range []string{
		"txt", "md", "log", "csv", "json", "xml", "yaml", "yml", "toml", "ini", "cfg", "conf",
		"sh", "py", "go", "js", "ts", "css", "html", "java", "c", "h", "cpp", "rs", "sql",
	} {
		if got := Lookup("a." + ext).Kind; got != Text {
			t.Errorf("Lookup(a.%s).Kind = %q, want %q", ext, got, Text)
		}
	}
}
