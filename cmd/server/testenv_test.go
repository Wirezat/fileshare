package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/share"
	"github.com/Wirezat/fileshare/internal/store"
	"github.com/Wirezat/fileshare/internal/upload"
)

var testConfigPath string

const testAdminPassword = "test-admin-pw"

var testAdminHash = mustHash(testAdminPassword)

func mustHash(pw string) string {
	h, err := password.Hash(pw)
	if err != nil {
		panic(err)
	}
	return h
}

func baseConfig() *config.Config {
	return &config.Config{
		AdminUsername: "admin",
		AdminPassword: testAdminHash,
		Files:         map[string]config.FileData{},
	}
}

// seedConfig installs cfg as the server's config for the rest of the test.
func seedConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	if cfg.Files == nil {
		cfg.Files = map[string]config.FileData{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	testConfigPath = filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(testConfigPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if cfgStore, err = store.Open(testConfigPath); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	shares = share.New(cfgStore)
	admins = auth.New(cfgStore)
}

// storedConfig returns the config as it was last written to disk.
func storedConfig(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile(testConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	cfg.ApplyDefaults()
	return &cfg
}

// newServer returns the full production handler in a fresh working directory with all in-memory state reset.
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

	storage = upload.New(filepath.Join(dir, "chunks"), chunkTimeout)
	return securityHeaders(buildMux())
}

// adminCookie logs in with the baseConfig credentials and returns the session cookie.
func adminCookie(t *testing.T) *http.Cookie {
	t.Helper()
	token, err := admins.Login("admin", testAdminPassword, "test")
	if err != nil {
		t.Fatal(err)
	}
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
