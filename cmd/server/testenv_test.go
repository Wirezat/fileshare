package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
)

// baseConfig returns a config past setup with no shares.
func baseConfig() *shared.Config {
	return &shared.Config{
		AdminUsername: "admin",
		AdminPassword: testAdminHash,
		Files:         map[string]shared.FileData{},
	}
}

// seedConfig installs cfg as the server's config for the rest of the test.
func seedConfig(t *testing.T, cfg *shared.Config) {
	t.Helper()
	if cfg.Files == nil {
		cfg.Files = map[string]shared.FileData{}
	}
	if err := shared.SaveConfig(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	t.Cleanup(func() { os.Remove("data.json") })
}

// storedConfig returns the config as the server currently holds it.
func storedConfig(t *testing.T) *shared.Config {
	t.Helper()
	cfg, err := shared.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// newServer returns the full production handler, running in a fresh working
// directory whose ./web points at assets/web, with all in-memory state reset.
func newServer(t *testing.T) http.Handler {
	t.Helper()
	assets, err := filepath.Abs("../../assets/web")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(assets, filepath.Join(dir, "web")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	loginLimiter, unlockLimiter = newLimiter(), newLimiter()
	adminSessions, shareTokens = newTTLStore[struct{}](), newTTLStore[string]()
	setupToken = ""
	prevChunks := chunkTempBase
	chunkTempBase = filepath.Join(dir, "chunks")
	t.Cleanup(func() { chunkTempBase = prevChunks })
	storage = NewLocalStorage()
	return securityHeaders(buildMux())
}

// adminCookie returns a valid admin session cookie.
func adminCookie(t *testing.T) *http.Cookie {
	t.Helper()
	token, err := generateAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	storeAdminToken(token)
	return &http.Cookie{Name: adminSessionCookie, Value: token}
}

// send runs one request through h; opts adjust the request before it is sent.
func send(h http.Handler, method, target string, body io.Reader, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func withHeader(k, v string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func asNavigation(r *http.Request) {
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Accept", "text/html")
}

func asFetch(r *http.Request) {
	r.Header.Set("Sec-Fetch-Mode", "cors")
	r.Header.Set("Accept", "*/*")
}
