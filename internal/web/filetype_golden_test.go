package web

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Wirezat/fileshare/internal/media"
	"github.com/Wirezat/fileshare/internal/office"
)

type typeCase struct {
	name        string
	kind        string
	thumbnail   bool
	text        bool
	docType     string
	inlineOff   bool
	inlineOn    bool
	shownInline bool
}

func imageCase(name string, thumbnail bool) typeCase {
	return typeCase{name: name, kind: "image", thumbnail: thumbnail, shownInline: true}
}

func videoCase(name string) typeCase {
	return typeCase{name: name, kind: "video", shownInline: true}
}

func audioCase(name string) typeCase {
	return typeCase{name: name, shownInline: true}
}

func textCase(name string) typeCase {
	return typeCase{name: name, text: true, shownInline: true}
}

func docCase(name, docType string) typeCase {
	return typeCase{name: name, docType: docType, inlineOn: true}
}

func plainCase(name string) typeCase {
	return typeCase{name: name}
}

func goldenTypeCases() []typeCase {
	cases := []typeCase{
		imageCase("a.jpg", true), imageCase("A.JPG", true), imageCase("a.jpeg", true), imageCase("a.JPEG", true),
		imageCase("a.png", true), imageCase("a.gif", true), imageCase("a.webp", true), imageCase("a.avif", false),
		{name: "a.bmp", shownInline: true}, {name: "a.svg", shownInline: true},
		videoCase("a.mp4"), videoCase("a.mov"), videoCase("a.webm"), videoCase("a.mkv"), videoCase("a.avi"), videoCase("a.wmv"),
		audioCase("a.mp3"), audioCase("a.wav"), audioCase("a.flac"), audioCase("a.aac"), audioCase("a.ogg"), audioCase("a.m4a"), audioCase("a.opus"),
		{name: "a.pdf", inlineOff: true, inlineOn: true, shownInline: true},
		{name: "A.PDF", inlineOff: true, inlineOn: true, shownInline: true},
		{name: "résumé.pdf", inlineOff: true, inlineOn: true, shownInline: true},
		docCase("a.docx", "word"), docCase("a.doc", "word"), docCase("a.odt", "word"),
		docCase("a.xlsx", "cell"), docCase("a.xls", "cell"), docCase("a.ods", "cell"),
		docCase("a.pptx", "slide"), docCase("a.ppt", "slide"), docCase("a.odp", "slide"),
		docCase("A.DOCX", "word"),
		{name: "a.tsv", shownInline: true},
		plainCase("a.zip"), plainCase("a.tar.gz"), plainCase("a.TAR.GZ"), plainCase("a.tar"), plainCase("a.gz"), plainCase("a.rar"), plainCase("a.7z"),
		plainCase("Makefile"), plainCase("Dockerfile"), plainCase(".env"), plainCase(".bashrc"),
		plainCase("blob.unknownext"), plainCase("a.verylongext"), plainCase("a.b.c.zzz"), plainCase("a."), plainCase(".."), plainCase(""),
		plainCase("a.日本語拡張"), plainCase("a.😀😀😀"),
	}
	for _, ext := range []string{
		"txt", "md", "log", "csv", "json", "xml", "yaml", "yml", "toml", "ini", "cfg", "conf",
		"sh", "py", "go", "js", "ts", "css", "html", "java", "c", "h", "cpp", "rs", "sql",
	} {
		cases = append(cases, textCase("a."+ext))
	}
	return append(cases, textCase("A.MD"), textCase("a.b.go"))
}

func TestFileTypeGolden(t *testing.T) {
	for _, tc := range goldenTypeCases() {
		if got := media.Kind(tc.name); got != tc.kind {
			t.Errorf("media.Kind(%q) = %q, want %q", tc.name, got, tc.kind)
		}
		if got := media.CanThumbnail(tc.name); got != tc.thumbnail {
			t.Errorf("media.CanThumbnail(%q) = %v, want %v", tc.name, got, tc.thumbnail)
		}
		if got := media.IsText(tc.name); got != tc.text {
			t.Errorf("media.IsText(%q) = %v, want %v", tc.name, got, tc.text)
		}
		if got := office.DocType(tc.name); got != tc.docType {
			t.Errorf("office.DocType(%q) = %q, want %q", tc.name, got, tc.docType)
		}
		if got := office.OpensInline(false, tc.name); got != tc.inlineOff {
			t.Errorf("office.OpensInline(false, %q) = %v, want %v", tc.name, got, tc.inlineOff)
		}
		if got := office.OpensInline(true, tc.name); got != tc.inlineOn {
			t.Errorf("office.OpensInline(true, %q) = %v, want %v", tc.name, got, tc.inlineOn)
		}
		if got := shownInline(tc.name); got != tc.shownInline {
			t.Errorf("shownInline(%q) = %v, want %v", tc.name, got, tc.shownInline)
		}
	}
}

func fileLabel(name string) string {
	ext := strings.ToLower(name[strings.LastIndex(name, ".")+1:])
	if ext == "jpeg" {
		return "JPG"
	}
	units := utf16.Encode([]rune(strings.ToUpper(ext)))
	if len(units) > 4 {
		units = units[:4]
	}
	if len(units) == 0 {
		return "FILE"
	}
	return string(utf16.Decode(units))
}

func TestFileLabelGolden(t *testing.T) {
	for name, want := range map[string]string{
		"a.jpg": "JPG", "a.JPEG": "JPG", "UPPER.JPG": "JPG", "A.JPG": "JPG", "a.png": "PNG", "a.webp": "WEBP",
		"a.mp4": "MP4", "a.mp3": "MP3", "a.pdf": "PDF", "A.PDF": "PDF", "résumé.pdf": "PDF",
		"a.docx": "DOCX", "a.xlsx": "XLSX", "a.pptx": "PPTX",
		"a.md": "MD", "a.go": "GO", "a.html": "HTML", "a.json": "JSON",
		"a.zip": "ZIP", "a.tar.gz": "GZ", "a.TAR.GZ": "GZ", "a.7z": "7Z",
		"a.svg": "SVG", "a.bmp": "BMP", "a.m4a": "M4A", "a.opus": "OPUS", "a.tsv": "TSV",
		"Makefile": "MAKE", "Dockerfile": "DOCK", ".env": "ENV", ".bashrc": "BASH",
		"blob.unknownext": "UNKN", "a.verylongext": "VERY", "a.b.c.d": "D",
		"a.": "FILE", "..": "FILE", "a..": "FILE", "": "FILE",
		"a.日本語拡張": "日本語拡", "a.😀😀😀": "😀😀", "a.ı": "I",
	} {
		if got := fileLabel(name); got != want {
			t.Errorf("fileLabel(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFileLabelDiffersFromJSOnMultiLetterUppercase(t *testing.T) {
	const js = "SS"
	if got := fileLabel("a.ß"); got != "ß" || got == js {
		t.Errorf("fileLabel(%q) = %q, want %q (JS gives %q)", "a.ß", got, "ß", js)
	}
}
