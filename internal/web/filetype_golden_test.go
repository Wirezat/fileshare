package web

import (
	"testing"

	"github.com/Wirezat/fileshare/internal/filetype"
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
	return typeCase{name: name, kind: "audio", shownInline: true}
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
		audioCase("a.mp3"), audioCase("a.wav"), audioCase("a.flac"), audioCase("a.aac"), audioCase("a.ogg"), typeCase{name: "a.m4a", shownInline: true}, typeCase{name: "a.opus", shownInline: true},
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

func mediaKind(name string) string {
	switch k := filetype.Lookup(name).Kind; k {
	case filetype.Image, filetype.Video, filetype.Audio:
		return k
	}
	return ""
}

func TestFileTypeGolden(t *testing.T) {
	for _, tc := range goldenTypeCases() {
		if got := mediaKind(tc.name); got != tc.kind {
			t.Errorf("mediaKind(%q) = %q, want %q", tc.name, got, tc.kind)
		}
		if got := filetype.Lookup(tc.name).Thumbnail; got != tc.thumbnail {
			t.Errorf("Lookup(%q).Thumbnail = %v, want %v", tc.name, got, tc.thumbnail)
		}
		if got := filetype.Lookup(tc.name).Kind == filetype.Text; got != tc.text {
			t.Errorf("Lookup(%q).Kind == Text = %v, want %v", tc.name, got, tc.text)
		}
		if got := filetype.Lookup(tc.name).OfficeType; got != tc.docType {
			t.Errorf("Lookup(%q).OfficeType = %q, want %q", tc.name, got, tc.docType)
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
