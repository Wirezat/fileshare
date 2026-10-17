package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

const docBytes = "PK\x03\x04 pretend this is a docx"

func withOfficeShare(t *testing.T, fd config.FileData, officeURL, secret string) (*testEnv, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bericht.docx"), []byte(docBytes), 0o600)
	os.WriteFile(filepath.Join(dir, "notiz.txt"), []byte("plain"), 0o600)
	fd.Path = dir

	cfg := baseConfig()
	cfg.OfficeURL, cfg.OfficeSecret = officeURL, secret
	cfg.Files["docs"] = fd
	return newEnv(t, cfg), dir
}

func get(t *testing.T, e *testEnv, target string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.srv.handleRequest(rec, req)
	return rec
}

func TestViewerPageForOfficeDoc(t *testing.T) {
	e, _ := withOfficeShare(t, config.FileData{Office: config.OfficeView}, "https://office.example.com", testSecret)

	rec := get(t, e, "/docs/bericht.docx", "Accept-Language", "de-DE,de;q=0.9")
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want html", ct)
	}
	for _, want := range []string{
		`https://office.example.com/web-apps/apps/api/documents/api.js`,
		`"mode":"view"`,
		`"lang":"de"`,
		`"url":"http://example.com/docs/bericht.docx?dl=1"`,
		`"title":"bericht.docx"`,
		`"token":"`,
		`href="/docs/bericht.docx?dl=1" download`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("viewer page lacks %s", want)
		}
	}
	if strings.Contains(body, docBytes) {
		t.Error("viewer page contains the document bytes")
	}
	if strings.Contains(body, testSecret) {
		t.Error("viewer page leaks the shared secret")
	}
}

func TestDlServesTheFileInsideTheSandbox(t *testing.T) {
	e, _ := withOfficeShare(t, config.FileData{Office: config.OfficeView}, "https://office.example.com", testSecret)

	rec := get(t, e, "/docs/bericht.docx?dl=1")
	if rec.Code != http.StatusOK || rec.Body.String() != docBytes {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("CSP = %q, want sandbox", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestFileServedDirectlyWhenViewerDoesNotApply(t *testing.T) {
	cases := []struct {
		name      string
		fd        config.FileData
		officeURL string
		secret    string
		target    string
	}{
		{"office off", config.FileData{}, "https://office.example.com", testSecret, "/docs/bericht.docx"},
		{"no server configured", config.FileData{Office: config.OfficeView}, "", "", "/docs/bericht.docx"},
		{"secret missing", config.FileData{Office: config.OfficeView}, "https://office.example.com", "", "/docs/bericht.docx"},
		{"not an office file", config.FileData{Office: config.OfficeView}, "https://office.example.com", testSecret, "/docs/notiz.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := withOfficeShare(t, tc.fd, tc.officeURL, tc.secret)
			rec := get(t, e, tc.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
				t.Errorf("got an html page, want the file: %s", rec.Body.String()[:80])
			}
		})
	}
}

func TestPasswordShareOpensForTheDocumentServer(t *testing.T) {
	hash, _ := password.Hash("geheim")
	e, _ := withOfficeShare(t, config.FileData{Office: config.OfficeView, Password: hash}, "https://office.example.com", testSecret)
	exp := time.Now().Add(5 * time.Minute).Unix()

	if rec := get(t, e, "/docs/bericht.docx?dl=1"); strings.Contains(rec.Body.String(), docBytes) {
		t.Fatal("file served without password or token")
	}
	if rec := get(t, e, "/docs/bericht.docx"); strings.Contains(rec.Body.String(), "api.js") {
		t.Fatal("viewer page served without password")
	}

	rec := get(t, e, "/docs/bericht.docx?dl=1",
		"Authorization", dsBearer(t, testSecret, "https://fileshare.example.com/docs/bericht.docx?dl=1", exp))
	if rec.Code != http.StatusOK || rec.Body.String() != docBytes {
		t.Fatalf("with document server token: status = %d, body = %q", rec.Code, rec.Body.String())
	}

	rec = get(t, e, "/docs/bericht.docx?dl=1",
		"Authorization", dsBearer(t, testSecret, "https://fileshare.example.com/docs/notiz.txt?dl=1", exp))
	if strings.Contains(rec.Body.String(), docBytes) {
		t.Fatal("token for another file unlocked this one")
	}

	rec = get(t, e, "/docs/bericht.docx?dl=1",
		"Authorization", dsBearer(t, "wrong", "https://fileshare.example.com/docs/bericht.docx?dl=1", exp))
	if strings.Contains(rec.Body.String(), docBytes) {
		t.Fatal("token with the wrong secret unlocked the file")
	}
}

func TestPdfIsServedWithoutTheSandbox(t *testing.T) {
	e, dir := withOfficeShare(t, config.FileData{}, "", "")
	os.WriteFile(filepath.Join(dir, "scan.pdf"), []byte("%PDF-1.4 fake"), 0o600)

	pdf := get(t, e, "/docs/scan.pdf")
	if pdf.Code != http.StatusOK || pdf.Header().Get("Content-Security-Policy") != "" {
		t.Errorf("pdf: status %d, CSP %q, want 200 and no sandbox", pdf.Code, pdf.Header().Get("Content-Security-Policy"))
	}
	if ct := pdf.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Errorf("pdf Content-Type = %q", ct)
	}

	txt := get(t, e, "/docs/notiz.txt")
	if txt.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("txt lost its sandbox: %q", txt.Header().Get("Content-Security-Policy"))
	}

	os.WriteFile(filepath.Join(dir, "fake.pdf"), []byte("<html><script>alert(1)</script>"), 0o600)
	fake := get(t, e, "/docs/fake.pdf")
	if ct := fake.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Errorf("html disguised as .pdf served as %q", ct)
	}
}
