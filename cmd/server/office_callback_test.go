package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
)

// fakeDS stands in for the document server: it hands out one saved document
// and records every callback response it gets back.
func fakeDS(t *testing.T, saved string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/cache/") {
			w.Write([]byte(saved))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callback(t *testing.T, target string, body map[string]any, secret string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		tok, _ := signJWT(secret, map[string]any{"payload": body})
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	handleRequest(rec, req)
	return rec
}

func dsError(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var out struct {
		Error int `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("callback answered non-json (%d): %s", rec.Code, rec.Body.String())
	}
	return out.Error
}

func TestCallbackSavesTheDocument(t *testing.T) {
	ds := fakeDS(t, "PK new version")
	dir := withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, ds.URL, testSecret)
	os.Chmod(filepath.Join(dir, "bericht.docx"), 0o644)

	rec := callback(t, "/docs/bericht.docx?callback=1",
		map[string]any{"key": "k", "status": 2, "url": ds.URL + "/cache/files/out.docx"}, testSecret)

	if rec.Code != http.StatusOK || dsError(t, rec) != 0 {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "bericht.docx"))
	if string(got) != "PK new version" {
		t.Errorf("file = %q, want the saved version", got)
	}
	info, _ := os.Stat(filepath.Join(dir, "bericht.docx"))
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want the original 644 kept", info.Mode().Perm())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".*.tmp*")); len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestCallbackForceSaveAlsoWrites(t *testing.T) {
	ds := fakeDS(t, "forced")
	dir := withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, ds.URL, testSecret)

	rec := callback(t, "/docs/bericht.docx?callback=1",
		map[string]any{"key": "k", "status": 6, "url": ds.URL + "/cache/x"}, testSecret)
	if dsError(t, rec) != 0 {
		t.Fatal(rec.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bericht.docx")); string(got) != "forced" {
		t.Errorf("file = %q", got)
	}
}

func TestCallbackStatusWithoutSaveLeavesFileAlone(t *testing.T) {
	ds := fakeDS(t, "should never land")
	for _, status := range []int{1, 3, 4, 7} {
		dir := withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, ds.URL, testSecret)
		rec := callback(t, "/docs/bericht.docx?callback=1",
			map[string]any{"key": "k", "status": status, "url": ds.URL + "/cache/x"}, testSecret)
		if dsError(t, rec) != 0 {
			t.Errorf("status %d: error %d", status, dsError(t, rec))
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "bericht.docx")); string(got) != docBytes {
			t.Errorf("status %d overwrote the file", status)
		}
	}
}

func TestCallbackRefusals(t *testing.T) {
	ds := fakeDS(t, "must not land")
	other := fakeDS(t, "ssrf")
	cases := []struct {
		name   string
		fd     shared.FileData
		target string
		url    string
		secret string
		want   int
	}{
		{"view share", shared.FileData{Office: shared.OfficeView}, "/docs/bericht.docx?callback=1", ds.URL + "/cache/x", testSecret, http.StatusForbidden},
		{"office off", shared.FileData{}, "/docs/bericht.docx?callback=1", ds.URL + "/cache/x", testSecret, http.StatusForbidden},
		{"unsigned", shared.FileData{Office: shared.OfficeEdit}, "/docs/bericht.docx?callback=1", ds.URL + "/cache/x", "", http.StatusUnauthorized},
		{"wrong secret", shared.FileData{Office: shared.OfficeEdit}, "/docs/bericht.docx?callback=1", ds.URL + "/cache/x", "nope", http.StatusUnauthorized},
		{"url on another host", shared.FileData{Office: shared.OfficeEdit}, "/docs/bericht.docx?callback=1", other.URL + "/cache/x", testSecret, http.StatusForbidden},
		{"path traversal", shared.FileData{Office: shared.OfficeEdit}, "/docs/../data.json?callback=1", ds.URL + "/cache/x", testSecret, http.StatusForbidden},
		{"expired share", shared.FileData{Office: shared.OfficeEdit, Expired: true}, "/docs/bericht.docx?callback=1", ds.URL + "/cache/x", testSecret, http.StatusGone},
		{"not an office file", shared.FileData{Office: shared.OfficeEdit}, "/docs/notiz.txt?callback=1", ds.URL + "/cache/x", testSecret, http.StatusForbidden},
		{"unknown share", shared.FileData{Office: shared.OfficeEdit}, "/nope/bericht.docx?callback=1", ds.URL + "/cache/x", testSecret, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := withOfficeShare(t, tc.fd, ds.URL, testSecret)
			rec := callback(t, tc.target, map[string]any{"key": "k", "status": 2, "url": tc.url}, tc.secret)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "bericht.docx")); string(got) != docBytes {
				t.Error("file was overwritten despite the refusal")
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "notiz.txt")); got != nil && string(got) != "plain" {
				t.Error("notiz.txt was overwritten")
			}
		})
	}
}

func TestCallbackWithoutTheFlagIsANormalPost(t *testing.T) {
	withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, "https://office.example.com", testSecret)
	req := httptest.NewRequest(http.MethodPost, "/docs/bericht.docx", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handleRequest(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestEditShareGetsAnEditableConfigWithCallback(t *testing.T) {
	withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, "https://office.example.com", testSecret)

	body := get(t, "/docs/bericht.docx").Body.String()
	for _, want := range []string{
		`"mode":"edit"`,
		`"edit":true`,
		`"callbackUrl":"http://example.com/docs/bericht.docx?callback=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit viewer lacks %s", want)
		}
	}
}

func TestViewShareGetsNoCallback(t *testing.T) {
	withOfficeShare(t, shared.FileData{Office: shared.OfficeView}, "https://office.example.com", testSecret)
	if body := get(t, "/docs/bericht.docx").Body.String(); strings.Contains(body, "callbackUrl") {
		t.Error("view config carries a callbackUrl")
	}
}

func withSingleFileShare(t *testing.T, officeURL string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "vertrag.docx")
	os.WriteFile(file, []byte(docBytes), 0o600)
	cfg := &shared.Config{
		AdminUsername: "admin", AdminPassword: testAdminHash,
		OfficeURL: officeURL, OfficeSecret: testSecret,
		Files: map[string]shared.FileData{"vertrag": {Path: file, Office: shared.OfficeEdit}},
	}
	if err := shared.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	prev := officeHtmlPath
	officeHtmlPath = "../../assets/web/html/office.html"
	t.Cleanup(func() {
		officeHtmlPath = prev
		shared.SaveConfig(&shared.Config{AdminUsername: "admin", AdminPassword: testAdminHash, Files: map[string]shared.FileData{}})
		os.Remove("data.json")
	})
	return file
}

func TestSingleFileShareRoundTrip(t *testing.T) {
	ds := fakeDS(t, "signed version")
	file := withSingleFileShare(t, ds.URL)

	page := get(t, "/vertrag").Body.String()
	for _, want := range []string{`"mode":"edit"`, `"url":"http://example.com/vertrag?dl=1"`, `"callbackUrl":"http://example.com/vertrag?callback=1"`} {
		if !strings.Contains(page, want) {
			t.Errorf("viewer lacks %s", want)
		}
	}
	if strings.Contains(page, `href="/"`) {
		t.Error("viewer links back to the server root, which is not a listing")
	}

	if rec := get(t, "/vertrag?dl=1"); rec.Body.String() != docBytes {
		t.Fatalf("dl served %q", rec.Body.String())
	}

	rec := callback(t, "/vertrag?callback=1", map[string]any{"key": "k", "status": 2, "url": ds.URL + "/cache/x"}, testSecret)
	if rec.Code != http.StatusOK || dsError(t, rec) != 0 {
		t.Fatalf("callback: status %d, body %s", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "signed version" {
		t.Errorf("file = %q, want the saved version", got)
	}
}

func TestCallbackRefusesToWriteADirectory(t *testing.T) {
	ds := fakeDS(t, "x")
	dir := withOfficeShare(t, shared.FileData{Office: shared.OfficeEdit}, ds.URL, testSecret)
	os.Mkdir(filepath.Join(dir, "ordner.docx"), 0o755)

	rec := callback(t, "/docs/ordner.docx?callback=1", map[string]any{"key": "k", "status": 2, "url": ds.URL + "/cache/x"}, testSecret)
	if rec.Code == http.StatusOK {
		t.Error("callback accepted a directory as save target")
	}
	if _, err := os.Stat(filepath.Join(dir, "ordner.docx")); err != nil {
		t.Error("directory vanished")
	}
}
