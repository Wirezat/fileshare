package web

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

	"github.com/Wirezat/fileshare/internal/config"
)

func selectionShare(t *testing.T) (*testEnv, *requestContext, string) {
	t.Helper()
	e := newEnv(t, baseConfig())
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
	must(os.WriteFile(filepath.Join(dir, ".fileshare-upload-0123abcd.part"), []byte("p"), 0o644))
	return e, &requestContext{
		subpath:  "docs",
		diskPath: dir,
		fileData: config.FileData{Path: dir},
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
	e, ctx, _ := selectionShare(t)
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f=a.txt&f=sub", nil)
	rec := httptest.NewRecorder()

	e.srv.serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := zipNames(t, rec.Body.Bytes())
	want := []string{"a.txt", "sub/.hidden", "sub/c.txt", "sub/deep/d.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="`+filepath.Base(ctx.diskPath)+`-selection.zip"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestSelectionZipRejectsPathsOutsideTheDirectory(t *testing.T) {
	e, ctx, _ := selectionShare(t)
	for _, f := range []string{"../x", "..", "/etc/passwd", "sub/../../x"} {
		req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f="+f, nil)
		rec := httptest.NewRecorder()

		e.srv.serveDirectory(rec, req, ctx)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("f=%q: status = %d, want 400", f, rec.Code)
		}
	}
}

func TestSelectionZipDeniedWhenNoZip(t *testing.T) {
	e, ctx, _ := selectionShare(t)
	ctx.fileData.NoZip = true
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f=a.txt", nil)
	rec := httptest.NewRecorder()

	e.srv.serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSelectionZipTakesHiddenEntries(t *testing.T) {
	e, ctx, _ := selectionShare(t)
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip&f=.secret&f=.git", nil)
	rec := httptest.NewRecorder()

	e.srv.serveDirectory(rec, req, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := zipNames(t, rec.Body.Bytes())
	want := []string{".git/HEAD", ".secret"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestListingShowsHiddenEntriesButNoUploadParts(t *testing.T) {
	_, ctx, dir := selectionShare(t)
	infos, err := getFileInfos(dir, ctx.subpath)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range infos {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{".git", ".secret", "a.txt", "b.txt", "sub"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("listing = %v, want %v", names, want)
	}
}

func TestWholeFolderZipIncludesHiddenButNoUploadParts(t *testing.T) {
	e, ctx, _ := selectionShare(t)
	req := httptest.NewRequest(http.MethodGet, "/docs?download=zip", nil)
	rec := httptest.NewRecorder()

	e.srv.serveDirectory(rec, req, ctx)

	got := zipNames(t, rec.Body.Bytes())
	want := []string{".git/HEAD", ".secret", "a.txt", "b.txt", "sub/.hidden", "sub/c.txt", "sub/deep/d.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestFolderSwitcherListsHiddenFolders(t *testing.T) {
	_, _, dir := selectionShare(t)
	var names []string
	for _, l := range subdirLinks(dir, "/docs", "sub") {
		names = append(names, l.Name)
	}
	sort.Strings(names)
	if want := []string{".git", "sub"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("folders = %v, want %v", names, want)
	}
}
