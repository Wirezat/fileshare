package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func textShare(t *testing.T, name, content string) *requestContext {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return &requestContext{
		config:   config.NewView(&config.Config{Files: map[string]config.FileData{}}),
		subpath:  "docs",
		diskPath: path,
		fileInfo: info,
		fileData: config.FileData{Path: dir},
	}
}

func textView(t *testing.T, ctx *requestContext) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/docs/"+ctx.fileInfo.Name()+"?view=text", nil)
	rec := httptest.NewRecorder()
	newEnv(t, baseConfig()).srv.handleGet(rec, req, ctx)
	return rec
}

func TestTextViewEscapesPlainText(t *testing.T) {
	rec := textView(t, textShare(t, "notes.txt", "a <b> & c"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<pre") || !strings.Contains(body, "a &lt;b&gt; &amp; c") {
		t.Errorf("body = %q, want escaped text inside <pre>", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestTextViewRendersMarkdown(t *testing.T) {
	rec := textView(t, textShare(t, "README.md", "# Title\n\nsome *text*\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "<h1") || !strings.Contains(body, "<em>text</em>") {
		t.Errorf("body = %q, want rendered markdown", body)
	}
}

func TestTextViewDropsRawHtmlAndScriptLinks(t *testing.T) {
	rec := textView(t, textShare(t, "evil.md", "<script>alert(1)</script>\n\n[x](javascript:alert(1))\n"))

	body := rec.Body.String()
	if strings.Contains(body, "<script") {
		t.Errorf("raw html survived: %q", body)
	}
	if strings.Contains(body, "javascript:") {
		t.Errorf("script link survived: %q", body)
	}
}

func TestTextViewRefusesNonTextFiles(t *testing.T) {
	rec := textView(t, textShare(t, "archive.zip", "PK"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func viewOf(t *testing.T, ctx *requestContext, view string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/docs/"+ctx.fileInfo.Name()+"?view="+view, nil)
	rec := httptest.NewRecorder()
	newEnv(t, baseConfig()).srv.handleGet(rec, req, ctx)
	return rec
}

func TestTextViewTruncatesLargeFiles(t *testing.T) {
	rec := textView(t, textShare(t, "big.log", strings.Repeat("a", textViewMaxBytes-1)+"é"+"tail"))

	if rec.Code != http.StatusOK || rec.Header().Get("X-Fileshare-Truncated") != "1" {
		t.Fatalf("status %d, truncated header %q", rec.Code, rec.Header().Get("X-Fileshare-Truncated"))
	}
	if want := "<pre>" + strings.Repeat("a", textViewMaxBytes-1) + "</pre>"; rec.Body.String() != want {
		t.Errorf("body has %d bytes, want the first MiB cut before the split character", rec.Body.Len())
	}
}

func TestTextViewProbe(t *testing.T) {
	rec := textView(t, textShare(t, "Makefile", "all:\n\techo hi\n"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<pre>all:") {
		t.Errorf("text without extension: %d %q", rec.Code, rec.Body.String())
	}
	if rec := textView(t, textShare(t, "blob.bin", "\x00\x01")); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("binary without extension: status %d, want 415", rec.Code)
	}
}

func TestSourceViewEscapesMarkdownAndHTML(t *testing.T) {
	if got := viewOf(t, textShare(t, "README.md", "# T <b>"), "source").Body.String(); got != "<pre># T &lt;b&gt;</pre>" {
		t.Errorf("markdown source = %q", got)
	}
	if got := viewOf(t, textShare(t, "page.html", "<script>x</script>"), "source").Body.String(); got != "<pre>&lt;script&gt;x&lt;/script&gt;</pre>" {
		t.Errorf("html source = %q", got)
	}
	if rec := viewOf(t, textShare(t, "notes.txt", "plain"), "source"); rec.Code != http.StatusNotFound {
		t.Errorf("txt source: status %d, want 404", rec.Code)
	}
}

func TestWithoutViewParamTheFileIsServedAsIs(t *testing.T) {
	ctx := textShare(t, "notes.txt", "plain")
	req := httptest.NewRequest(http.MethodGet, "/docs/notes.txt", nil)
	rec := httptest.NewRecorder()

	newEnv(t, baseConfig()).srv.handleGet(rec, req, ctx)

	if body := rec.Body.String(); body != "plain" {
		t.Errorf("body = %q, want the raw file", body)
	}
}
