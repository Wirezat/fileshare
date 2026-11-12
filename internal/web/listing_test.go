package web

import (
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
	common := []string{
		`data-href="/docs/a.jpg" data-view="image"`,
		`data-href="/docs/a.mp4" data-view="video"`,
		`data-href="/docs/a.mp3" data-view="audio"`,
		`data-href="/docs/a.pdf" data-view="pdf"`,
		`data-href="/docs/notes.txt" data-view="text"`,
		`data-href="/docs/README.md" data-view="markdown"`,
		`data-href="/docs/page.html" data-view="html"`,
		`data-href="/docs/main.go" data-view="text"`,
		`data-href="/docs/a.zip" data-view="info"`,
		`data-href="/docs/Makefile" data-view="probe"`,
		`<span class="card-icon">📕</span>`,
		`<span class="card-icon">💻</span>`,
		`<td class="row-icon">📁</td>`,
	}
	for mode, docx := range map[string]string{
		config.OfficeOff:  `data-href="/docs/bericht.docx" data-view="info"`,
		config.OfficeView: `data-href="/docs/bericht.docx" data-view="office"`,
	} {
		body := listingFixture(t, mode)
		for _, want := range append(common, docx) {
			if !strings.Contains(body, want) {
				t.Errorf("office %q: listing lacks %s", mode, want)
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
