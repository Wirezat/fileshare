package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

func editShare(t *testing.T, fd config.FileData, maxPost int) (*testEnv, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("old text"), 0o640)
	os.WriteFile(filepath.Join(dir, "photo.png"), pngHeaderOnly(4, 4), 0o640)
	fd.Path = dir
	cfg := baseConfig()
	cfg.MaxPostSize = maxPost
	cfg.Files["docs"] = fd
	return newEnv(t, cfg), dir
}

func put(t *testing.T, e *testEnv, target, body, ifMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	e.srv.handleRequest(rec, req)
	return rec
}

func TestSaveReplacesATextFileUnchangedSinceItWasLoaded(t *testing.T) {
	e, dir := editShare(t, config.FileData{Office: config.OfficeEdit}, 1<<20)
	etag := get(t, e, "/docs/notes.txt").Header().Get("ETag")

	rec := put(t, e, "/docs/notes.txt", "new text", etag)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(got) != "new text" {
		t.Errorf("file holds %q", got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "notes.txt")); info.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want the old 0640", info.Mode().Perm())
	}
	if next := rec.Header().Get("ETag"); next == "" || next == etag || next != get(t, e, "/docs/notes.txt").Header().Get("ETag") {
		t.Errorf("ETag after save %q, before %q; want the new one of the file", next, etag)
	}
	if r := e.stats.Report(func(string) bool { return true }); r.Total.Saves != 1 {
		t.Errorf("saves counted %d, want 1", r.Total.Saves)
	}
}

func TestSaveRefusesAFileChangedSinceItWasLoaded(t *testing.T) {
	e, dir := editShare(t, config.FileData{Office: config.OfficeEdit}, 1<<20)
	etag := get(t, e, "/docs/notes.txt").Header().Get("ETag")
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("changed by someone else"), 0o640)

	if rec := put(t, e, "/docs/notes.txt", "mine", etag); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status %d, want 412", rec.Code)
	}
	if rec := put(t, e, "/docs/notes.txt", "mine", ""); rec.Code != http.StatusPreconditionRequired {
		t.Errorf("without If-Match: status %d, want 428", rec.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(got) != "changed by someone else" {
		t.Errorf("file holds %q", got)
	}
}

func TestSaveNeedsAnEditShare(t *testing.T) {
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	for name, fd := range map[string]config.FileData{
		"view share":   {Office: config.OfficeView},
		"locked share": {Office: config.OfficeEdit, Password: hash},
	} {
		e, dir := editShare(t, fd, 1<<20)
		if rec := put(t, e, "/docs/notes.txt", "new", `"any"`); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, rec.Code)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(got) != "old text" {
			t.Errorf("%s: file holds %q", name, got)
		}
	}
}

func TestSaveTakesOnlyTextUpToTheUploadLimit(t *testing.T) {
	e, _ := editShare(t, config.FileData{Office: config.OfficeEdit}, 16)
	etag := get(t, e, "/docs/notes.txt").Header().Get("ETag")

	if rec := put(t, e, "/docs/notes.txt", strings.Repeat("x", 17), etag); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("17 bytes: status %d, want 413", rec.Code)
	}
	pngTag := get(t, e, "/docs/photo.png").Header().Get("ETag")
	if rec := put(t, e, "/docs/photo.png", "text", pngTag); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("png: status %d, want 415", rec.Code)
	}
	if rec := put(t, e, "/docs/missing.txt", "text", `"x"`); rec.Code != http.StatusNotFound {
		t.Errorf("missing file: status %d, want 404", rec.Code)
	}
	if !bytes.Equal([]byte("old text"), mustRead(t, e, "/docs/notes.txt")) {
		t.Error("notes.txt changed")
	}
}

func mustRead(t *testing.T, e *testEnv, target string) []byte {
	t.Helper()
	return get(t, e, target).Body.Bytes()
}
