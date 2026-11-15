package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func listingFixture(t *testing.T, office string) string {
	t.Helper()
	e, dir := withOfficeShare(t, config.FileData{Office: office}, "https://office.example.com", testSecret)
	for _, name := range []string{"a.jpg", "a.mp4", "a.mp3", "a.pdf", "notes.txt", "README.md", "page.html", "main.go", "a.zip", "Makefile"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	return get(t, e, "/docs").Body.String()
}

func TestListingMarksViewAndIcon(t *testing.T) {
	views := map[string]string{
		"a.jpg": "image", "a.mp4": "video", "a.mp3": "audio", "a.pdf": "pdf", "notes.txt": "text",
		"README.md": "markdown", "page.html": "html", "main.go": "text", "a.zip": "info", "Makefile": "probe",
	}
	icons := map[string]string{"a.pdf": "📕", "main.go": "💻", "sub": "📁"}
	for mode, docx := range map[string]string{config.OfficeOff: "info", config.OfficeView: "office"} {
		body := listingFixture(t, mode)
		entries := listingEntries(t, body)
		for name, want := range views {
			if got := entries[name].View; got != want {
				t.Errorf("office %q: %s has view %q, want %q", mode, name, got, want)
			}
		}
		if got := entries["bericht.docx"].View; got != docx {
			t.Errorf("office %q: bericht.docx has view %q, want %q", mode, got, docx)
		}
		for name, want := range icons {
			if got := entries[name].Icon; got != want {
				t.Errorf("office %q: %s has icon %q, want %q", mode, name, got, want)
			}
		}
		for _, gone := range []string{"data-text", "card-ext", "row-type"} {
			if strings.Contains(body, gone) {
				t.Errorf("office %q: listing still has %s", mode, gone)
			}
		}
	}
}

func TestListingTextFollowsAcceptLanguage(t *testing.T) {
	e, _ := previewEnv(t)

	if body := get(t, e, "/docs", "Accept-Language", "de-DE,de;q=0.9").Body.String(); !strings.Contains(body, `"share.lightbox.download":"Herunterladen"`) {
		t.Error("German listing lacks the German download label")
	}
	if body := get(t, e, "/docs").Body.String(); !strings.Contains(body, `"share.lightbox.download":"Download"`) {
		t.Error("default listing lacks the English download label")
	}
}

func TestLinksEncodeReservedCharactersInNames(t *testing.T) {
	e, dir := withOfficeShare(t, config.FileData{}, "https://office.example.com", testSecret)
	os.MkdirAll(filepath.Join(dir, "b #2", "c"), 0o755)
	os.WriteFile(filepath.Join(dir, "b #2", "a #1?%.png"), []byte("x"), 0o600)

	body := get(t, e, "/docs/b%20%232/c").Body.String()
	if want := `<a href="/docs/b%20%232">b #2</a>`; !strings.Contains(body, want) {
		t.Errorf("breadcrumb lacks %s", want)
	}
	entries := listingEntries(t, get(t, e, "/docs/b%20%232").Body.String())
	if got, want := entries["a #1?%.png"].Href, "/docs/b%20%232/a%20%231%3F%25.png"; got != want {
		t.Errorf("entry href = %q, want %q", got, want)
	}
	if rec := get(t, e, "/docs/b%20%232/a%20%231%3F%25.png"); rec.Code != 200 || rec.Body.String() != "x" {
		t.Errorf("encoded link answers %d %q, want the file", rec.Code, rec.Body.String())
	}
}

func listingEntries(t *testing.T, body string) map[string]ListEntry {
	t.Helper()
	const open = `<script id="entries" type="application/json">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatal("listing has no entries script")
	}
	raw := body[start+len(open):]
	raw = raw[:strings.Index(raw, "</script>")]
	var list []ListEntry
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("entries script is not JSON: %v", err)
	}
	byName := make(map[string]ListEntry, len(list))
	for _, e := range list {
		byName[e.Name] = e
	}
	return byName
}

func TestListingEmbedsEntries(t *testing.T) {
	e, dir := withOfficeShare(t, config.FileData{Office: config.OfficeView}, "https://office.example.com", testSecret)
	for _, name := range []string{"a.zip", "a.pdf", "a #1?%.png"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	got := listingEntries(t, get(t, e, "/docs").Body.String())
	for name, want := range map[string]ListEntry{
		"sub":        {Name: "sub", Href: "/docs/sub", Dir: true, Icon: "📁"},
		"a.zip":      {Name: "a.zip", Href: "/docs/a.zip", Size: 1, View: "info", Icon: "📦", Download: true},
		"a.pdf":      {Name: "a.pdf", Href: "/docs/a.pdf", Size: 1, View: "pdf", Icon: "📕"},
		"a #1?%.png": {Name: "a #1?%.png", Href: "/docs/a%20%231%3F%25.png", Size: 1, View: "image", Icon: "🖼️", Download: true},
	} {
		if got[name] != want {
			t.Errorf("entry %q = %+v, want %+v", name, got[name], want)
		}
	}
}
