package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

// shareDir creates a folder with a.txt, sub/b.txt and a secret file next to it.
func shareDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "share")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("beta"), 0o600)
	os.WriteFile(filepath.Join(root, "secret.txt"), []byte("top secret"), 0o600)
	return dir
}

func publicServer(t *testing.T, shares map[string]config.FileData) http.Handler {
	t.Helper()
	h := newServer(t)
	cfg := baseConfig()
	cfg.Files = shares
	seedConfig(t, cfg)
	return h
}

func TestPublicFileAndFolder(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"docs": {Path: dir}})

	rec := send(h, http.MethodGet, "/docs/a.txt", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "alpha" {
		t.Fatalf("file: %d %q", rec.Code, rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("file CSP = %q, want sandbox", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", rec.Header())
	}

	rec = send(h, http.MethodGet, "/docs", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a.txt") {
		t.Fatalf("folder: %d", rec.Code)
	}

	rec = send(h, http.MethodGet, "/docs/sub/b.txt", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "beta" {
		t.Fatalf("nested file: %d %q", rec.Code, rec.Body.String())
	}
}

func TestPublicUnknownShareAndMissingFileAre404(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"docs": {Path: shareDir(t)}})

	for _, target := range []string{"/nope", "/docs/missing.txt"} {
		if rec := send(h, http.MethodGet, target, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
	}
}

func TestPublicTraversalNeverServesOutsideTheShare(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"docs": {Path: shareDir(t)}})

	for _, target := range []string{"/docs/../secret.txt", "/docs/%2e%2e/secret.txt", "/docs/..%2fsecret.txt"} {
		rec := send(h, http.MethodGet, target, nil)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "top secret") {
			t.Errorf("%s: status %d leaked the file", target, rec.Code)
		}
	}
}

func TestPublicExpiredShareIs410(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{
		"flag": {Path: dir, Expired: true},
		"date": {Path: dir, Expiration: time.Now().Add(-time.Hour).Unix()},
	})

	for _, target := range []string{"/flag/a.txt", "/date/a.txt"} {
		if rec := send(h, http.MethodGet, target, nil); rec.Code != http.StatusGone {
			t.Errorf("%s: status = %d, want 410", target, rec.Code)
		}
	}
	if storedConfig(t).Files["date"].Expired {
		t.Error("expiry by date set the manual expired flag")
	}
}

func TestPublicMethodsOtherThanGetAndHead(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"docs": {Path: shareDir(t)}})

	if rec := send(h, http.MethodHead, "/docs/a.txt", nil); rec.Code != http.StatusOK {
		t.Errorf("HEAD: %d", rec.Code)
	}
	if rec := send(h, http.MethodDelete, "/docs/a.txt", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: %d, want 405", rec.Code)
	}
}

func passwordShare(t *testing.T) http.Handler {
	t.Helper()
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	return publicServer(t, map[string]config.FileData{"priv": {Path: shareDir(t), Password: hash}})
}

func unlock(h http.Handler, pass string) *httpRecorder {
	form := url.Values{"password": {pass}}.Encode()
	rec := send(h, http.MethodPost, "/priv/unlock", strings.NewReader(form),
		withHeader("Content-Type", "application/x-www-form-urlencoded"))
	return &httpRecorder{rec.Code, rec.Result().Cookies(), rec.Header(), rec.Body.String()}
}

type httpRecorder struct {
	code    int
	cookies []*http.Cookie
	header  http.Header
	body    string
}

func TestPasswordShareShowsTheGate(t *testing.T) {
	h := passwordShare(t)

	rec := send(h, http.MethodGet, "/priv/a.txt", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/priv/unlock"`) || strings.Contains(rec.Body.String(), "alpha") {
		t.Fatalf("gate: %d", rec.Code)
	}
}

func TestUnlockWithTheRightPassword(t *testing.T) {
	h := passwordShare(t)

	res := unlock(h, "pw")
	if res.code != http.StatusSeeOther || res.header.Get("Location") != "/priv" {
		t.Fatalf("%d → %q, want 303 → /priv", res.code, res.header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range res.cookies {
		if c.Name == "share_pw_priv" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/priv" {
		t.Fatalf("share cookie: %+v", cookie)
	}
	if rec := send(h, http.MethodGet, "/priv/a.txt", nil, withCookie(cookie)); rec.Body.String() != "alpha" {
		t.Errorf("cookie does not open the share: %d", rec.Code)
	}
}

func TestUnlockWithAWrongPassword(t *testing.T) {
	h := passwordShare(t)

	res := unlock(h, "nope")
	if res.code != http.StatusOK || len(res.cookies) != 0 || !strings.Contains(res.body, `action="/priv/unlock"`) {
		t.Fatalf("status = %d, cookies = %d", res.code, len(res.cookies))
	}
}

func TestUnlockIsRateLimitedAfterFiveFailures(t *testing.T) {
	h := passwordShare(t)

	for i := 0; i < maxFailures; i++ {
		unlock(h, "nope")
	}
	if res := unlock(h, "pw"); res.code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", res.code)
	}
}

func TestUnlockOfShareWithoutPasswordRedirects(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"priv": {Path: shareDir(t)}})

	if res := unlock(h, "x"); res.code != http.StatusSeeOther || res.header.Get("Location") != "/priv" {
		t.Fatalf("%d → %q", res.code, res.header.Get("Location"))
	}
}

func TestZipOfWholeFolder(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"docs": {Path: shareDir(t)}})

	rec := send(h, http.MethodGet, "/docs?download=zip", nil)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("PK")) {
		t.Fatalf("zip: %d", rec.Code)
	}
}

func TestTextViewParameter(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"docs": {Path: shareDir(t)}})

	rec := send(h, http.MethodGet, "/docs/a.txt?view=text", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<pre>alpha</pre>") {
		t.Fatalf("text view: %d %q", rec.Code, rec.Body.String())
	}
}

func chunkInit(h http.Handler, sub string) int {
	form := url.Values{"uploadId": {"upload-0001"}, "filename": {"new.txt"}, "totalChunks": {"1"}}.Encode()
	return send(h, http.MethodPost, "/"+sub+"/chunk-init", strings.NewReader(form),
		withHeader("Content-Type", "application/x-www-form-urlencoded")).Code
}

func TestChunkUploadNeedsAllowPost(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{
		"ro":  {Path: dir},
		"old": {Path: dir, AllowPost: true, Expired: true},
	})

	if got := chunkInit(h, "ro"); got != http.StatusForbidden {
		t.Errorf("upload into read-only share: %d, want 403", got)
	}
	if got := chunkInit(h, "missing"); got != http.StatusNotFound {
		t.Errorf("upload into unknown share: %d, want 404", got)
	}
	if got := chunkInit(h, "old"); got != http.StatusGone {
		t.Errorf("upload into expired share: %d, want 410", got)
	}
}

func TestChunkUploadAssemblesTheFile(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}})

	if got := chunkInit(h, "up"); got != http.StatusOK {
		t.Fatalf("init: %d", got)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("uploadId", "upload-0001")
	mw.WriteField("chunkIndex", "0")
	fw, _ := mw.CreateFormFile("chunk", "blob")
	fw.Write([]byte("uploaded"))
	mw.Close()

	rec := send(h, http.MethodPost, "/up/chunk", &body, withHeader("Content-Type", mw.FormDataContentType()))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("chunk: %d %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != "uploaded" {
		t.Fatalf("assembled file: %q, %v", got, err)
	}
}

func TestStaticAssets(t *testing.T) {
	h := newServer(t)
	seedConfig(t, baseConfig())

	for _, target := range []string{"/static/share.css", "/static/share.js", "/static/theme.css", "/static/locales/de.json"} {
		rec := send(h, http.MethodGet, target, nil)
		if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}
