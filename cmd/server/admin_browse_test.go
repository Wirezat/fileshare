package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func browse(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/browse?path="+url.QueryEscape(path), nil)
	rec := httptest.NewRecorder()
	handleAdminBrowse(rec, req)
	return rec
}

func browseOK(t *testing.T, path string) browseResult {
	t.Helper()
	rec := browse(t, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse %q: status = %d: %s", path, rec.Code, rec.Body.String())
	}
	var res browseResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return res
}

func TestBrowseListsFoldersFirstThenByName(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "zeta"), 0o755)
	os.Mkdir(filepath.Join(dir, "Alpha"), 0o755)
	os.WriteFile(filepath.Join(dir, "beta.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644)

	res := browseOK(t, dir+"/")

	if res.Path != dir {
		t.Errorf("Path = %q, want %q", res.Path, dir)
	}
	want := []browseEntry{{"Alpha", true}, {"zeta", true}, {".hidden", false}, {"beta.txt", false}}
	if !reflect.DeepEqual(res.Entries, want) {
		t.Errorf("Entries = %v, want %v", res.Entries, want)
	}
}

func TestBrowseEmptyDirectoryIsEmptyArray(t *testing.T) {
	rec := browse(t, t.TempDir())
	if !strings.Contains(rec.Body.String(), `"entries":[]`) {
		t.Errorf("body = %s, want an empty entries array", rec.Body.String())
	}
}

func TestBrowseFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "real"), 0o755)
	os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link"))
	os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken"))

	res := browseOK(t, dir)

	want := []browseEntry{{"link", true}, {"real", true}, {"broken", false}}
	if !reflect.DeepEqual(res.Entries, want) {
		t.Errorf("Entries = %v, want %v", res.Entries, want)
	}
}

func TestBrowseNamesWithURLSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	name := "a #b?c%d&e"
	os.Mkdir(filepath.Join(dir, name), 0o755)
	os.WriteFile(filepath.Join(dir, name, "inner.txt"), nil, 0o644)

	res := browseOK(t, dir)
	if len(res.Entries) != 1 || res.Entries[0].Name != name {
		t.Fatalf("Entries = %v, want one entry %q", res.Entries, name)
	}
	inner := browseOK(t, filepath.Join(dir, name))
	if len(inner.Entries) != 1 || inner.Entries[0].Name != "inner.txt" {
		t.Errorf("inner Entries = %v, want inner.txt", inner.Entries)
	}
}

func TestBrowseErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	os.WriteFile(file, nil, 0o644)

	cases := []struct {
		name string
		path string
		want int
	}{
		{"missing path", "", http.StatusBadRequest},
		{"relative path", "some/dir", http.StatusBadRequest},
		{"does not exist", filepath.Join(dir, "nope"), http.StatusNotFound},
		{"a file", file, http.StatusBadRequest},
		{"below a file", filepath.Join(file, "x"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := browse(t, tc.path); rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestBrowseUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	os.Mkdir(dir, 0o000)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if rec := browse(t, dir); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestBrowseRejectsPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/api/browse?path=/", nil)
	rec := httptest.NewRecorder()
	handleAdminBrowse(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestBrowseRequiresAdmin(t *testing.T) {
	withConfig(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/browse?path=/", nil)
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Accept", "*/*")
	rec := httptest.NewRecorder()

	buildMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
