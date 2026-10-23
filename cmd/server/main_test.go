package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/settings"
	"github.com/Wirezat/fileshare/internal/stats"
	"github.com/Wirezat/fileshare/internal/store"
)

func startServe(t *testing.T, h http.Handler, timeout time.Duration) (addr string, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, &http.Server{Handler: h}, ln, timeout) }()
	return ln.Addr().String(), cancel, errc
}

func TestServeLetsOpenRequestsFinishAndRefusesNewOnes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	addr, cancel, done := startServe(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write([]byte("ok"))
	}), 5*time.Second)

	res := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr)
		if err == nil {
			resp.Body.Close()
		}
		res <- err
	}()
	<-started
	cancel()
	time.Sleep(100 * time.Millisecond)

	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("a new connection was accepted after shutdown began")
	}
	close(release)
	if err := <-res; err != nil {
		t.Errorf("open request: %v, want it to finish", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("serve did not return after the last request finished")
	}
}

func TestServeClosesRequestsThatOutlastTheTimeout(t *testing.T) {
	started := make(chan struct{})
	addr, cancel, done := startServe(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}), 200*time.Millisecond)

	res := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr)
		if err == nil {
			resp.Body.Close()
		}
		res <- err
	}()
	<-started
	begin := time.Now()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve = %v, want nil after closing late requests", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return after the timeout")
	}
	if took := time.Since(begin); took < 200*time.Millisecond {
		t.Errorf("serve returned after %s, before the timeout", took)
	}
	if err := <-res; err == nil {
		t.Error("the open request finished normally, want it closed")
	}
}

func buildTestStore(t *testing.T, dir string) *store.FileStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	cfg := `{"admin_username":"admin","admin_password":"x","chunkInactivityTimeout":120,"files":{"docs":{"path":"` + dir + `"}}}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestBuildWiresRequestLogAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	srv, _, err := build(buildTestStore(t, dir), stats.Open(filepath.Join(t.TempDir(), "stats.json")), logstore.New(), &log, "../../assets/web", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/a.txt", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "alpha" {
		t.Fatalf("share file: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(log.String(), `"message":"GET /docs/a.txt"`) {
		t.Errorf("request log = %q, want the public request", log.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/share.css", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("asset: %d, want 200", rec.Code)
	}
}

func TestChunkTimeoutFollowsTheConfig(t *testing.T) {
	st := buildTestStore(t, t.TempDir())
	timeout := chunkTimeout(st)
	if got := timeout(); got != 120*time.Second {
		t.Fatalf("timeout = %s, want 2m", got)
	}
	st.Update(func(c *config.Config) error { c.ChunkInactivityTimeout = 300; return nil })
	if got := timeout(); got != 300*time.Second {
		t.Errorf("timeout = %s after update, want 5m", got)
	}
}

func TestServeReturnsAListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if err := serve(context.Background(), &http.Server{}, ln, time.Second); err == nil {
		t.Error("serve = nil on a closed listener, want the error")
	}
}

func TestShutdownEndsAdminLogStreamsAtOnce(t *testing.T) {
	st := buildTestStore(t, t.TempDir())
	srv, admins, err := build(st, stats.Open(filepath.Join(t.TempDir(), "stats.json")), logstore.New(), nil, "../../assets/web", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	st.Update(func(c *config.Config) error {
		c.AdminPassword, err = password.Hash("pw")
		return err
	})
	token, err := admins.Login("admin", "pw", "test")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, httpServer(srv), ln, 5*time.Second) }()

	req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/admin/api/logs/stream", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d, want 200", resp.StatusCode)
	}

	begin := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve still waits for the log stream")
	}
	if took := time.Since(begin); took > time.Second {
		t.Errorf("shutdown took %s with an open log stream", took)
	}
}

func TestBuildWiresStats(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.bin"), []byte("alpha"), 0o600)
	path := filepath.Join(t.TempDir(), "data.json")
	cfg := `{"admin_username":"admin","admin_password":"x","files":{"up":{"path":"` + dir + `","allow_post":true}}}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	counts := stats.Open(filepath.Join(t.TempDir(), "stats.json"))
	srv, _, err := build(st, counts, logstore.New(), nil, "../../assets/web", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	ua := "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"
	req := httptest.NewRequest(http.MethodPost, "/up/chunk?uploadId=wired-0001&chunkIndex=0&filename=b.txt&size=3&chunkSize=65536", strings.NewReader("abc"))
	req.Header.Set("User-Agent", ua)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/up/a.bin?dl=1", nil)
	req.Header.Set("User-Agent", ua)
	h.ServeHTTP(httptest.NewRecorder(), req)
	p := counts.Report(func(string) bool { return true }).Total
	if p.Uploads != 1 || p.BytesUp != 3 || p.Downloads != 1 || p.Visitors != 1 {
		t.Errorf("uploads %d bytes up %d downloads %d visitors %d, want 1 3 1 1", p.Uploads, p.BytesUp, p.Downloads, p.Visitors)
	}
}

func TestPathsFollowSettings(t *testing.T) {
	config, stats, chunks := paths(settings.Settings{DataDir: "/srv/fs"})
	if config != "/srv/fs/data.json" || stats != "/srv/fs/stats.json" || chunks != "/srv/fs/chunks" {
		t.Errorf("paths = %q %q %q", config, stats, chunks)
	}
}

func TestListenAddress(t *testing.T) {
	if got := listenAddr(settings.Settings{Bind: "127.0.0.1"}, 8080); got != "127.0.0.1:8080" {
		t.Errorf("listenAddr = %q, want 127.0.0.1:8080", got)
	}
}

func TestEnsureConfigCreatesMissingFileOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "data.json")
	if err := ensureConfig(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != initialConfig {
		t.Fatalf("content %q err %v, want the initial config", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	if _, err := store.Open(path); err != nil {
		t.Errorf("initial config does not load: %v", err)
	}

	os.WriteFile(path, []byte("keep"), 0o600)
	if err := ensureConfig(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "keep" {
		t.Errorf("existing file changed to %q", got)
	}
}
