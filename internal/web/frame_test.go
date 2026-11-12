package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameViewAllowsSameOriginFraming(t *testing.T) {
	e, dir := previewEnv(t)
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4"), 0o600)
	os.WriteFile(filepath.Join(dir, "page.html"), []byte("<p>hi</p>"), 0o600)

	cases := []struct{ target, csp, xfo, body string }{
		{"/docs/a.pdf?view=frame", "frame-ancestors 'self'", "SAMEORIGIN", "%PDF-1.4"},
		{"/docs/page.html?view=frame", "sandbox; frame-ancestors 'self'", "SAMEORIGIN", "<p>hi</p>"},
	}
	for _, tc := range cases {
		rec := send(e.handler, http.MethodGet, tc.target, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
			t.Errorf("%s: %d %q", tc.target, rec.Code, rec.Body.String())
		}
		if csp, xfo := rec.Header().Get("Content-Security-Policy"), rec.Header().Get("X-Frame-Options"); csp != tc.csp || xfo != tc.xfo {
			t.Errorf("%s: CSP %q XFO %q, want %q %q", tc.target, csp, xfo, tc.csp, tc.xfo)
		}
	}
	if rec := send(e.handler, http.MethodGet, "/docs/notiz.txt?view=frame", nil); rec.Code != http.StatusNotFound {
		t.Errorf("txt frame: status %d, want 404", rec.Code)
	}
	if xfo := send(e.handler, http.MethodGet, "/docs/a.pdf", nil).Header().Get("X-Frame-Options"); xfo != "DENY" {
		t.Errorf("plain pdf: XFO %q, want DENY", xfo)
	}
}
