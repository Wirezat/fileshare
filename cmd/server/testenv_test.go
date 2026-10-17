package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/share"
	"github.com/Wirezat/fileshare/internal/store"
	"github.com/Wirezat/fileshare/internal/upload"
)

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

type testEnv struct {
	path    string
	store   *store.FileStore
	auth    *auth.Service
	srv     *server
	handler http.Handler
}

// newEnv builds a fresh server for one test with cfg as its config.
func newEnv(t *testing.T, cfg *config.Config) *testEnv {
	t.Helper()
	if cfg.Files == nil {
		cfg.Files = map[string]config.FileData{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e := &testEnv{path: filepath.Join(dir, "data.json")}
	if err := os.WriteFile(e.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if e.store, err = store.Open(e.path); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	e.auth = auth.New(e.store)
	e.srv, err = newHTTPServer(deps{
		Store:   e.store,
		Shares:  share.New(e.store),
		Auth:    e.auth,
		Uploads: upload.New(filepath.Join(dir, "chunks"), chunkTimeout(e.store)),
		Logs:    logstore.New(),
		Assets:  "../../assets/web",
		Started: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.handler = e.srv.handler()
	return e
}

// stored returns the config as it was last written to disk.
func (e *testEnv) stored(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile(e.path)
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

// cookie logs in with the baseConfig credentials and returns the session cookie.
func (e *testEnv) cookie(t *testing.T) *http.Cookie {
	t.Helper()
	token, err := e.auth.Login("admin", testAdminPassword, "test")
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
