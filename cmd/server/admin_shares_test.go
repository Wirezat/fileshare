package main

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func withShare(t *testing.T, sub string, fd shared.FileData) {
	t.Helper()
	cfg := &shared.Config{
		AdminUsername: "admin",
		AdminPassword: testAdminHash,
		Files:         map[string]shared.FileData{},
	}
	if sub != "" {
		cfg.Files[sub] = fd
	}
	if err := shared.SaveConfig(cfg); err != nil {
		t.Fatalf("save test config: %v", err)
	}
	t.Cleanup(func() {
		shared.SaveConfig(&shared.Config{AdminUsername: "admin", AdminPassword: testAdminHash, Files: map[string]shared.FileData{}})
		os.Remove("data.json")
	})
}

func shareOf(t *testing.T, sub string) shared.FileData {
	t.Helper()
	cfg, err := shared.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	fd, ok := cfg.Files[sub]
	if !ok {
		t.Fatalf("share %q missing", sub)
	}
	return fd
}

func postShare(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/shares", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleAdminShares(rec, req)
	return rec
}

func patchShare(t *testing.T, sub, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/shares?subpath="+sub, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleAdminShares(rec, req)
	return rec
}

func TestShareCreateStoresZipAndOffice(t *testing.T) {
	withShare(t, "", shared.FileData{})

	rec := postShare(t, `{"subpath":"docs","path":"/tmp","no_zip":true,"office":"view"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	fd := shareOf(t, "docs")
	if !fd.NoZip {
		t.Error("NoZip = false, want true")
	}
	if fd.Office != shared.OfficeView {
		t.Errorf("Office = %q, want view", fd.Office)
	}
}

func TestShareCreateRejectsUnknownOffice(t *testing.T) {
	withShare(t, "", shared.FileData{})

	rec := postShare(t, `{"subpath":"docs","path":"/tmp","office":"print"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	cfg, _ := shared.LoadConfig()
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
			withShare(t, "docs", shared.FileData{Path: "/tmp", Office: shared.OfficeView})

			rec := patchShare(t, "docs", tc.body)
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
			if got := shareOf(t, "docs").Office; got != tc.wantO {
				t.Errorf("Office = %q, want %q", got, tc.wantO)
			}
		})
	}
}

func TestSharePatchNoZip(t *testing.T) {
	withShare(t, "docs", shared.FileData{Path: "/tmp"})

	if rec := patchShare(t, "docs", `{"no_zip":true}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !shareOf(t, "docs").NoZip {
		t.Error("NoZip = false after patch, want true")
	}

	if rec := patchShare(t, "docs", `{"no_zip":false}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if shareOf(t, "docs").NoZip {
		t.Error("NoZip = true after clearing, want false")
	}
}

func TestOldShareWithoutNoZipStillAllowsZip(t *testing.T) {
	var fd shared.FileData
	if err := json.Unmarshal([]byte(`{"path":"/tmp","uses":-1}`), &fd); err != nil {
		t.Fatal(err)
	}
	if fd.NoZip {
		t.Error("NoZip = true for a share without the field, want false")
	}
	if fd.Office != shared.OfficeOff {
		t.Errorf("Office = %q for a share without the field, want off", fd.Office)
	}
}

func TestNoZipIsOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(shared.FileData{Path: "/tmp"})
	if strings.Contains(string(b), "no_zip") {
		t.Errorf("no_zip serialised for a default share: %s", b)
	}
	if strings.Contains(string(b), "office") {
		t.Errorf("office serialised for a default share: %s", b)
	}
}

func TestShareQRReturnsAPNG(t *testing.T) {
	withShare(t, "docs", shared.FileData{Path: "/tmp"})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr?subpath=docs", nil)
	rec := httptest.NewRecorder()
	handleAdminShareQR(rec, req)

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
	withShare(t, "", shared.FileData{})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr?subpath=nope", nil)
	rec := httptest.NewRecorder()
	handleAdminShareQR(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestShareQRRequiresSubpath(t *testing.T) {
	withShare(t, "docs", shared.FileData{Path: "/tmp"})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/shares/qr", nil)
	rec := httptest.NewRecorder()
	handleAdminShareQR(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestZipDownloadDeniedWhenNoZip(t *testing.T) {
	dir := t.TempDir()
	ctx := &requestContext{
		subpath:  "docs",
		diskPath: dir,
		fileData: shared.FileData{Path: dir, NoZip: true},
	}
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip", nil)
	rec := httptest.NewRecorder()

	serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "zip") {
		t.Errorf("a zip was served anyway: Content-Type %s", ct)
	}
}
