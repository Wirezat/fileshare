package filetype

import "testing"

func TestLookup(t *testing.T) {
	cases := []struct {
		name string
		want Type
	}{
		{"a.jpg", Type{Kind: Image, Label: "JPG", Thumbnail: true}},
		{"a.JPEG", Type{Kind: Image, Label: "JPG", Thumbnail: true}},
		{"a.png", Type{Kind: Image, Label: "PNG", Thumbnail: true}},
		{"a.webp", Type{Kind: Image, Label: "WEBP", Thumbnail: true}},
		{"a.avif", Type{Kind: Image, Label: "AVIF"}},
		{"a.mp4", Type{Kind: Video, Label: "MP4"}},
		{"a.mp3", Type{Kind: Audio, Label: "MP3"}},
		{"a.flac", Type{Kind: Audio, Label: "FLAC"}},
		{"a.pdf", Type{Kind: PDF, Icon: "📄", Label: "PDF"}},
		{"a.docx", Type{Kind: Office, Icon: "📄", Label: "DOCX", OfficeType: "word"}},
		{"a.odt", Type{Kind: Office, Icon: "📄", Label: "ODT", OfficeType: "word"}},
		{"a.xlsx", Type{Kind: Office, Icon: "📊", Label: "XLSX", OfficeType: "cell"}},
		{"a.pptx", Type{Kind: Office, Icon: "📽️", Label: "PPTX", OfficeType: "slide"}},
		{"A.DOCX", Type{Kind: Office, Icon: "📄", Label: "DOCX", OfficeType: "word"}},
		{"a.md", Type{Kind: Text, Icon: "📝", Label: "MD"}},
		{"a.txt", Type{Kind: Text, Icon: "📝", Label: "TXT"}},
		{"a.log", Type{Kind: Text, Icon: "📝", Label: "LOG"}},
		{"a.csv", Type{Kind: Text, Icon: "📊", Label: "CSV"}},
		{"a.go", Type{Kind: Text, Icon: "📎", Label: "GO"}},
		{"a.html", Type{Kind: Text, Icon: "📎", Label: "HTML"}},
		{"a.zip", Type{Kind: Unknown, Icon: "📦", Label: "ZIP"}},
		{"a.tar.gz", Type{Kind: Unknown, Icon: "📦", Label: "GZ"}},
		{"a.TAR.GZ", Type{Kind: Unknown, Icon: "📦", Label: "GZ"}},
		{"a.7z", Type{Kind: Unknown, Icon: "📦", Label: "7Z"}},
		{"a.svg", Type{Kind: Unknown, Icon: "📎", Label: "SVG"}},
		{"a.bmp", Type{Kind: Unknown, Icon: "📎", Label: "BMP"}},
		{"a.m4a", Type{Kind: Unknown, Icon: "📎", Label: "M4A"}},
		{"a.opus", Type{Kind: Unknown, Icon: "📎", Label: "OPUS"}},
		{"a.tsv", Type{Kind: Unknown, Icon: "📎", Label: "TSV"}},
		{"Makefile", Type{Kind: Unknown, Icon: "📎", Label: "MAKE"}},
		{"Dockerfile", Type{Kind: Unknown, Icon: "📎", Label: "DOCK"}},
		{".env", Type{Kind: Unknown, Icon: "📎", Label: "ENV"}},
		{"blob.unknownext", Type{Kind: Unknown, Icon: "📎", Label: "UNKN"}},
		{"a.b.c.d", Type{Kind: Unknown, Icon: "📎", Label: "D"}},
		{"a.", Type{Kind: Unknown, Icon: "📎", Label: "FILE"}},
		{"..", Type{Kind: Unknown, Icon: "📎", Label: "FILE"}},
		{"", Type{Kind: Unknown, Icon: "📎", Label: "FILE"}},
		{"a.日本語拡張", Type{Kind: Unknown, Icon: "📎", Label: "日本語拡"}},
		{"a.😀😀😀", Type{Kind: Unknown, Icon: "📎", Label: "😀😀"}},
		{"a.ı", Type{Kind: Unknown, Icon: "📎", Label: "I"}},
	}
	for _, tc := range cases {
		if got := Lookup(tc.name); got != tc.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", tc.name, got, tc.want)
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
