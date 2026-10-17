package main

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func withShare(t *testing.T, sub string, fd config.FileData) *testEnv {
	t.Helper()
	cfg := baseConfig()
	if sub != "" {
		cfg.Files[sub] = fd
	}
	return newEnv(t, cfg)
}

func shareOf(t *testing.T, e *testEnv, sub string) config.FileData {
	t.Helper()
	fd, ok := e.stored(t).Files[sub]
	if !ok {
		t.Fatalf("share %q missing", sub)
	}
	return fd
}

func postShare(t *testing.T, e *testEnv, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/shares", strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.srv.handleAdminShares(rec, req)
	return rec
}

func patchShare(t *testing.T, e *testEnv, sub, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/shares?subpath="+sub, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.srv.handleAdminShares(rec, req)
	return rec
}

func TestShareCreateStoresZipAndOffice(t *testing.T) {
	e := withShare(t, "", config.FileData{})

	rec := postShare(t, e, `{"subpath":"docs","path":"/tmp","no_zip":true,"office":"view"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	fd := shareOf(t, e, "docs")
	if !fd.NoZip {
		t.Error("NoZip = false, want true")
	}
	if fd.Office != config.OfficeView {
		t.Errorf("Office = %q, want view", fd.Office)
	}
}

func TestShareCreateRejectsUnknownOffice(t *testing.T) {
	e := withShare(t, "", config.FileData{})

	rec := postShare(t, e, `{"subpath":"docs","path":"/tmp","office":"print"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	cfg := e.stored(t)
	if _, exists := cfg.Files["docs"]; exists {
		t.Error("rejected share was stored anyway")
	}
}

func TestSharePatchOffice(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  int
		wantO string
	}{
		{"set view", `{"office":"view"}`, http.StatusOK, "view"},
		{"set edit", `{"office":"edit"}`, http.StatusOK, "edit"},
		{"clear", `{"office":""}`, http.StatusOK, ""},
		{"reject unknown", `{"office":"print"}`, http.StatusBadRequest, "view"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := withShare(t, "docs", config.FileData{Path: "/tmp", Office: config.OfficeView})

			rec := patchShare(t, e, "docs", tc.body)
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
			if got := shareOf(t, e, "docs").Office; got != tc.wantO {
				t.Errorf("Office = %q, want %q", got, tc.wantO)
			}
		})
	}
}

func TestSharePatchNoZip(t *testing.T) {
	e := withShare(t, "docs", config.FileData{Path: "/tmp"})

	if rec := patchShare(t, e, "docs", `{"no_zip":true}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !shareOf(t, e, "docs").NoZip {
		t.Error("NoZip = false after patch, want true")
	}

	if rec := patchShare(t, e, "docs", `{"no_zip":false}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if shareOf(t, e, "docs").NoZip {
		t.Error("NoZip = true after clearing, want false")
	}
}

func TestOldShareWithoutNoZipStillAllowsZip(t *testing.T) {
	var fd config.FileData
	if err := json.Unmarshal([]byte(`{"path":"/tmp","uses":-1}`), &fd); err != nil {
		t.Fatal(err)
	}
	if fd.NoZip {
		t.Error("NoZip = true for a share without the field, want false")
	}
	if fd.Office != config.OfficeOff {
		t.Errorf("Office = %q for a share without the field, want off", fd.Office)
	}
}

func TestNoZipIsOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(config.FileData{Path: "/tmp"})
	if strings.Contains(string(b), "no_zip") {
		t.Errorf("no_zip serialised for a default share: %s", b)
	}
	if strings.Contains(string(b), "office") {
		t.Errorf("office serialised for a default share: %s", b)
	}
}

func TestShareQRReturnsAPNG(t *testing.T) {
	e := withShare(t, "docs", config.FileData{Path: "/tmp"})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr?subpath=docs", nil)
	rec := httptest.NewRecorder()
	e.srv.handleAdminShareQR(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	cfg, err := png.DecodeConfig(rec.Body)
	if err != nil {
		t.Fatalf("body did not decode as a PNG: %v", err)
	}
	if cfg.Width != shareQRSize || cfg.Height != shareQRSize {
		t.Errorf("QR image is %dx%d, want %dx%d", cfg.Width, cfg.Height, shareQRSize, shareQRSize)
	}
}

func TestShareQRMissingShareIs404(t *testing.T) {
	e := withShare(t, "", config.FileData{})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr?subpath=nope", nil)
	rec := httptest.NewRecorder()
	e.srv.handleAdminShareQR(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestShareQRRequiresSubpath(t *testing.T) {
	e := withShare(t, "docs", config.FileData{Path: "/tmp"})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr", nil)
	rec := httptest.NewRecorder()
	e.srv.handleAdminShareQR(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestZipDownloadDeniedWhenNoZip(t *testing.T) {
	e := newEnv(t, baseConfig())
	dir := t.TempDir()
	ctx := &requestContext{
		subpath:  "docs",
		diskPath: dir,
		fileData: config.FileData{Path: dir, NoZip: true},
	}
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip", nil)
	rec := httptest.NewRecorder()

	e.srv.serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "zip") {
		t.Errorf("a zip was served anyway: Content-Type %s", ct)
	}
}

func TestShareCreateCleansTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notiz.txt"), []byte("plain"), 0o600)
	e := withShare(t, "", config.FileData{})

	rec := postShare(t, e, `{"subpath":"docs","path":"`+dir+`/"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got := shareOf(t, e, "docs").Path; got != dir {
		t.Errorf("Path = %q, want %q", got, dir)
	}
	if rec := get(t, e, "/docs/notiz.txt"); rec.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestSharePatchCleansTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	e := withShare(t, "docs", config.FileData{Path: "/tmp"})

	rec := patchShare(t, e, "docs", `{"path":"`+dir+`//"}`)
	if rec.Code/100 != 2 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := shareOf(t, e, "docs").Path; got != dir {
		t.Errorf("Path = %q, want %q", got, dir)
	}
}
