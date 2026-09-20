package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
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
		config:   &shared.Config{Files: map[string]shared.FileData{}},
		subpath:  "docs",
		diskPath: path,
		fileInfo: info,
		fileData: shared.FileData{Path: dir, Uses: -1},
	}
}

func textView(t *testing.T, ctx *requestContext) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/docs/"+ctx.fileInfo.Name()+"?view=text", nil)
	rec := httptest.NewRecorder()
	handleGet(rec, req, ctx)
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

func TestTextViewRefusesLargeFiles(t *testing.T) {
	rec := textView(t, textShare(t, "big.log", strings.Repeat("x", textViewMaxBytes+1)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestWithoutViewParamTheFileIsServedAsIs(t *testing.T) {
	ctx := textShare(t, "notes.txt", "plain")
	req := httptest.NewRequest(http.MethodGet, "/docs/notes.txt", nil)
	rec := httptest.NewRecorder()

	handleGet(rec, req, ctx)

	if body := rec.Body.String(); body != "plain" {
		t.Errorf("body = %q, want the raw file", body)
	}
}

func TestIsTextKnowsTheExtensions(t *testing.T) {
	for name, want := range map[string]bool{
		"a.txt": true, "a.md": true, "a.go": true, "a.json": true, "a.YML": true,
		"a.zip": false, "a.pdf": false, "a.docx": false, "noext": false, "a.png": false,
	} {
		if got := isText(name); got != want {
			t.Errorf("isText(%q) = %v, want %v", name, got, want)
		}
	}
}
