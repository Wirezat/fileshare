package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"github.com/Wirezat/fileshare/internal/upload"
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
	return publicEnv(t, shares).handler
}

func publicEnv(t *testing.T, shares map[string]config.FileData) *testEnv {
	t.Helper()
	cfg := baseConfig()
	cfg.Files = shares
	return newEnv(t, cfg)
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
	e := publicEnv(t, map[string]config.FileData{
		"flag": {Path: dir, Expired: true},
		"date": {Path: dir, Expiration: time.Now().Add(-time.Hour).Unix()},
	})
	h := e.handler

	for _, target := range []string{"/flag/a.txt", "/date/a.txt"} {
		if rec := send(h, http.MethodGet, target, nil); rec.Code != http.StatusGone {
			t.Errorf("%s: status = %d, want 410", target, rec.Code)
		}
	}
	if e.stored(t).Files["date"].Expired {
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

	for i := 0; i < ratelimit.MaxFailures; i++ {
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

func chunkInitWith(h http.Handler, sub string, v url.Values) *httptest.ResponseRecorder {
	return send(h, http.MethodPost, "/"+sub+"/chunk-init", strings.NewReader(v.Encode()),
		withHeader("Content-Type", "application/x-www-form-urlencoded"))
}

const testChunk = 64 << 10

func chunkInit(h http.Handler, sub string) int {
	return chunkInitWith(h, sub, url.Values{"uploadId": {"upload-0001"}, "filename": {"new.txt"},
		"size": {strconv.Itoa(testChunk + 3)}, "chunkSize": {strconv.Itoa(testChunk)}}).Code
}

func sendChunk(h http.Handler, sub, index string, body io.Reader) *httptest.ResponseRecorder {
	return send(h, http.MethodPost, "/"+sub+"/chunk?uploadId=upload-0001&chunkIndex="+index, body,
		withHeader("Content-Type", "application/octet-stream"))
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

func TestChunkUploadWritesTheFile(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}})

	rec := chunkInitWith(h, "up", url.Values{"uploadId": {"upload-0001"}, "filename": {"new.txt"},
		"size": {strconv.Itoa(testChunk + 3)}, "chunkSize": {strconv.Itoa(testChunk)}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"missingChunks":[0,1]`) {
		t.Fatalf("init: %d %s", rec.Code, rec.Body.String())
	}
	if rec := sendChunk(h, "up", "1", strings.NewReader("ded")); rec.Code != http.StatusAccepted {
		t.Fatalf("chunk 1: %d %s", rec.Code, rec.Body.String())
	}
	first := strings.Repeat("u", testChunk)
	if rec := sendChunk(h, "up", "0", strings.NewReader(first)); rec.Code != http.StatusNoContent {
		t.Fatalf("chunk 0: %d %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != first+"ded" {
		t.Fatalf("file: %d bytes, %v", len(got), err)
	}
}

func TestSmallFileNeedsNoInit(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}, "ro": {Path: dir}})
	q := "uploadId=small-0001&chunkIndex=0&filename=tiny.txt&size=4&chunkSize=5"
	if rec := send(h, http.MethodPost, "/ro/chunk?"+q, strings.NewReader("tiny")); rec.Code != http.StatusForbidden {
		t.Errorf("into read-only share: %d, want 403", rec.Code)
	}
	if rec := send(h, http.MethodPost, "/up/chunk?"+q, strings.NewReader("tiny")); rec.Code != http.StatusNoContent {
		t.Fatalf("single-request upload: %d %s", rec.Code, rec.Body.String())
	}
	if got, err := os.ReadFile(filepath.Join(dir, "tiny.txt")); err != nil || string(got) != "tiny" {
		t.Fatalf("file: %q, %v", got, err)
	}
	big := "uploadId=small-0002&chunkIndex=0&filename=big.txt&size=9&chunkSize=5"
	if rec := send(h, http.MethodPost, "/up/chunk?"+big, strings.NewReader("12345")); rec.Code != http.StatusNotFound {
		t.Errorf("multi-chunk file without init: %d, want 404", rec.Code)
	}
}

func TestUploadIntoAPasswordShareNeedsTheUnlock(t *testing.T) {
	hash, _ := password.Hash("pw")
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"priv": {Path: dir, AllowPost: true, Password: hash}})
	v := url.Values{"uploadId": {"upload-0001"}, "filename": {"new.txt"}, "size": {"8"}, "chunkSize": {"5"}}
	if rec := chunkInitWith(h, "priv", v); rec.Code != http.StatusForbidden {
		t.Errorf("chunk-init without unlock: %d, want 403", rec.Code)
	}
	q := "uploadId=small-0001&chunkIndex=0&filename=tiny.txt&size=4&chunkSize=5"
	if rec := send(h, http.MethodPost, "/priv/chunk?"+q, strings.NewReader("tiny")); rec.Code != http.StatusForbidden {
		t.Errorf("single-request upload without unlock: %d, want 403", rec.Code)
	}
	cookie := unlock(h, "pw").cookies[0]
	if rec := send(h, http.MethodPost, "/priv/chunk?"+q, strings.NewReader("tiny"), withCookie(cookie)); rec.Code != http.StatusNoContent {
		t.Errorf("single-request upload after unlock: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChunkMustGoToTheShareOfItsSession(t *testing.T) {
	dir, other := shareDir(t), shareDir(t)
	h := publicServer(t, map[string]config.FileData{
		"up":  {Path: dir, AllowPost: true},
		"up2": {Path: other, AllowPost: true},
		"ro":  {Path: dir},
	})
	chunkInit(h, "up")
	for sub, want := range map[string]int{"ro": http.StatusForbidden, "up2": http.StatusNotFound, "missing": http.StatusNotFound} {
		if rec := sendChunk(h, sub, "0", strings.NewReader("uploa")); rec.Code != want {
			t.Errorf("chunk of up's session sent to %s: %d, want %d", sub, rec.Code, want)
		}
	}
}

func TestChunkSizeIsCappedByMaxPostSize(t *testing.T) {
	cfg := baseConfig()
	cfg.MaxPostSize = 100_000
	cfg.Files = map[string]config.FileData{"up": {Path: shareDir(t), AllowPost: true}}
	h := newEnv(t, cfg).handler
	v := url.Values{"uploadId": {"upload-0001"}, "filename": {"a"}, "size": {"500000"}, "chunkSize": {"200000"}}
	if rec := chunkInitWith(h, "up", v); rec.Code != http.StatusBadRequest {
		t.Errorf("chunks above maxPostSize: %d, want 400", rec.Code)
	}
}

func TestShareScriptIsRevalidated(t *testing.T) {
	h := newEnv(t, baseConfig()).handler
	for _, p := range []string{"/static/share.js", "/static/share.css"} {
		if got := send(h, http.MethodGet, p, nil).Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s Cache-Control = %q, want no-cache", p, got)
		}
	}
}

func TestChunkInitNeedsSizes(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"up": {Path: shareDir(t), AllowPost: true}})
	for _, v := range []url.Values{
		{"uploadId": {"upload-0001"}, "filename": {"a"}, "totalChunks": {"1"}},
		{"uploadId": {"upload-0001"}, "filename": {"a"}, "size": {"-1"}, "chunkSize": {"5"}},
		{"uploadId": {"upload-0001"}, "filename": {"a"}, "size": {"5"}, "chunkSize": {"0"}},
		{"uploadId": {"upload-0001"}, "filename": {"a"}, "size": {"5"}, "chunkSize": {strconv.Itoa(upload.MaxChunkSize + 1)}},
	} {
		if rec := chunkInitWith(h, "up", v); rec.Code != http.StatusBadRequest {
			t.Errorf("init %v: %d, want 400", v, rec.Code)
		}
	}
}

func TestChunkOfWrongSizeIsRefused(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}})
	chunkInit(h, "up")
	for _, body := range []string{"upl", strings.Repeat("u", testChunk+1)} {
		body := body
		if rec := sendChunk(h, "up", "0", strings.NewReader(body)); rec.Code != http.StatusBadRequest {
			t.Errorf("chunk of %d bytes: %d, want 400", len(body), rec.Code)
		}
	}
	if rec := sendChunk(h, "up", "9", strings.NewReader("x")); rec.Code != http.StatusBadRequest {
		t.Errorf("chunk index out of range: %d, want 400", rec.Code)
	}
	if rec := send(h, http.MethodPost, "/up/chunk?uploadId=unknown-0001&chunkIndex=0", strings.NewReader("x")); rec.Code != http.StatusNotFound {
		t.Errorf("chunk of unknown session: %d, want 404", rec.Code)
	}
}

func TestOversizedChunkIsRefusedWithoutBeingRead(t *testing.T) {
	dir := shareDir(t)
	h := publicServer(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}})
	chunkInit(h, "up")
	huge := &countingReader{left: 256 << 20}
	if rec := sendChunk(h, "up", "0", huge); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized chunk: %d, want 400", rec.Code)
	}
	if huge.read > 2*testChunk {
		t.Errorf("server read %d bytes of an oversized chunk, want only the chunk's length and a little more", huge.read)
	}
}

type countingReader struct{ left, read int64 }

func (c *countingReader) Read(p []byte) (int, error) {
	if c.left == 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > c.left {
		n = c.left
	}
	clear(p[:n])
	c.left -= n
	c.read += n
	return int(n), nil
}

func TestOldMultipartChunkIsRefused(t *testing.T) {
	h := publicServer(t, map[string]config.FileData{"up": {Path: shareDir(t), AllowPost: true}})
	chunkInit(h, "up")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("uploadId", "upload-0001")
	mw.WriteField("chunkIndex", "0")
	fw, _ := mw.CreateFormFile("chunk", "blob")
	fw.Write([]byte("uploa"))
	mw.Close()
	if rec := send(h, http.MethodPost, "/up/chunk", &body, withHeader("Content-Type", mw.FormDataContentType())); rec.Code != http.StatusBadRequest {
		t.Errorf("multipart chunk: %d, want 400", rec.Code)
	}
}

func TestStaticAssets(t *testing.T) {
	e := newEnv(t, baseConfig())
	h := e.handler

	for _, target := range []string{"/static/share.css", "/static/share.js", "/static/theme.css", "/static/locales/de.json"} {
		rec := send(h, http.MethodGet, target, nil)
		if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}

func TestDateExpiredPasswordShareIs410NotTheGate(t *testing.T) {
	hash, _ := password.Hash("pw")
	h := publicServer(t, map[string]config.FileData{
		"priv": {Path: shareDir(t), Password: hash, Expiration: time.Now().Add(-time.Hour).Unix()},
	})
	if rec := send(h, http.MethodGet, "/priv", nil, asNavigation); rec.Code != http.StatusGone {
		t.Errorf("status = %d, want 410 before any password prompt", rec.Code)
	}
}

func lockedPair(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("no"), 0o000)
	locked := filepath.Join(dir, "locked")
	os.Mkdir(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "in.txt"), []byte("in"), 0o644)
	os.Chmod(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	return dir
}

func TestUnreadableShareIs404NotAnError(t *testing.T) {
	dir := lockedPair(t)
	h := publicServer(t, map[string]config.FileData{
		"dir":  {Path: filepath.Join(dir, "locked")},
		"file": {Path: filepath.Join(dir, "secret.txt")},
	})
	for _, target := range []string{"/dir", "/file", "/dir/in.txt"} {
		if rec := send(h, http.MethodGet, target, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
	}
}

func TestListingAndZipLeaveOutUnreadableEntries(t *testing.T) {
	dir := lockedPair(t)
	h := publicServer(t, map[string]config.FileData{"docs": {Path: dir}})

	body := send(h, http.MethodGet, "/docs", nil).Body.String()
	if !strings.Contains(body, "ok.txt") || strings.Contains(body, "secret.txt") || strings.Contains(body, "locked") {
		t.Errorf("listing shows unreadable entries: %s", body)
	}

	rec := send(h, http.MethodGet, "/docs?download=zip", nil)
	if names := zipNames(t, rec.Body.Bytes()); strings.Join(names, ",") != "ok.txt" {
		t.Errorf("zip = %v, want only ok.txt", names)
	}
}
