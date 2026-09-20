package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func selectionShare(t *testing.T) (*requestContext, string) {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644))
	must(os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "sub", "c.txt"), []byte("c"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "sub", "deep", "d.txt"), []byte("d"), 0o644))
	must(os.WriteFile(filepath.Join(dir, ".secret"), []byte("s"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "sub", ".hidden"), []byte("h"), 0o644))
	must(os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	must(os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644))
	return &requestContext{
		subpath:  "docs",
		diskPath: dir,
		fileData: shared.FileData{Path: dir},
	}, dir
}

func zipNames(t *testing.T, body []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestSelectionZipContainsOnlyChosenEntries(t *testing.T) {
	ctx, _ := selectionShare(t)
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f=a.txt&f=sub", nil)
	rec := httptest.NewRecorder()

	serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := zipNames(t, rec.Body.Bytes())
	want := []string{"a.txt", "sub/c.txt", "sub/deep/d.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="`+filepath.Base(ctx.diskPath)+`-selection.zip"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestSelectionZipRejectsPathsOutsideTheDirectory(t *testing.T) {
	ctx, _ := selectionShare(t)
	for _, f := range []string{"../x", "/etc/passwd", ".secret", "sub/../../x"} {
		req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f="+f, nil)
		rec := httptest.NewRecorder()

		serveDirectory(rec, req, ctx)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("f=%q: status = %d, want 400", f, rec.Code)
		}
	}
}

func TestSelectionZipDeniedWhenNoZip(t *testing.T) {
	ctx, _ := selectionShare(t)
	ctx.fileData.NoZip = true
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f=a.txt", nil)
	rec := httptest.NewRecorder()

	serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestWholeFolderZipSkipsHiddenEntries(t *testing.T) {
	ctx, _ := selectionShare(t)
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip", nil)
	rec := httptest.NewRecorder()

	serveDirectory(rec, req, ctx)

	got := zipNames(t, rec.Body.Bytes())
	want := []string{"a.txt", "b.txt", "sub/c.txt", "sub/deep/d.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}
