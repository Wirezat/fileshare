package web

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
)

type countingThrottle struct {
	n      atomic.Int32
	client func(*http.Request) string
}

func (c *countingThrottle) Handler(next http.Handler, client func(*http.Request) string) http.Handler {
	c.n.Add(1)
	c.client = client
	return next
}

func TestThrottleSharesByRateKey(t *testing.T) {
	e := newEnv(t, baseConfig())
	ct := &countingThrottle{}
	e.srv.deps.Throttle = ct
	send(e.handler, http.MethodGet, "/missing/", nil)
	if ct.client == nil {
		t.Fatal("throttle got no client function")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[2001:db8:1:2::5]:4000"
	if got := ct.client(r); got != "2001:db8:1:2::/64" {
		t.Errorf("client = %q, want the /64 rate key", got)
	}
}

func TestOnlyThePublicChainIsThrottled(t *testing.T) {
	dir := shareDir(t)
	cfg := baseConfig()
	cfg.Files = map[string]config.FileData{
		"docs":   {Path: dir, AllowPost: true},
		"locked": {Path: dir, Password: mustHash("pw")},
		"old":    {Path: dir, Expired: true},
	}
	e := newEnv(t, cfg)
	ct := &countingThrottle{}
	e.srv.deps.Throttle = ct
	cookie := e.cookie(t)
	form := func(v url.Values) (*strings.Reader, func(*http.Request)) {
		return strings.NewReader(v.Encode()), withHeader("Content-Type", "application/x-www-form-urlencoded")
	}

	for _, target := range []string{
		"/docs/", "/docs/a.txt", "/docs/a.txt?view=text", "/docs?download=zip", "/locked/", "/old/", "/missing/",
	} {
		ct.n.Store(0)
		send(e.handler, http.MethodGet, target, nil)
		if ct.n.Load() != 1 {
			t.Errorf("GET %s not throttled", target)
		}
	}
	ct.n.Store(0)
	send(e.handler, http.MethodGet, "/docs/a.txt", nil, withHeader("Range", "bytes=1-2"))
	if ct.n.Load() != 1 {
		t.Error("Range request not throttled")
	}

	unthrottled := []func(){
		func() { send(e.handler, http.MethodGet, "/admin", nil, withCookie(cookie), asNavigation) },
		func() { send(e.handler, http.MethodGet, "/admin/api/me", nil, withCookie(cookie), asFetch) },
		func() { send(e.handler, http.MethodGet, "/admin/login", nil) },
		func() { send(e.handler, http.MethodGet, "/setup", nil) },
		func() { send(e.handler, http.MethodGet, "/static/share.css", nil) },
		func() { send(e.handler, http.MethodGet, "/static/share.js", nil) },
		func() { send(e.handler, http.MethodGet, "/static/theme.css", nil) },
		func() { send(e.handler, http.MethodGet, "/static/locales/de.json", nil) },
		func() {
			b, h := form(url.Values{"password": {"pw"}})
			send(e.handler, http.MethodPost, "/locked/unlock", b, h)
		},
		func() {
			b, h := form(url.Values{"uploadId": {"upload-0001"}, "filename": {"n.txt"}, "totalChunks": {"1"}})
			send(e.handler, http.MethodPost, "/docs/chunk-init", b, h)
		},
	}
	for i, req := range unthrottled {
		ct.n.Store(0)
		req()
		if ct.n.Load() != 0 {
			t.Errorf("unthrottled request %d went through the throttle", i)
		}
	}
}

func limitedShare(t *testing.T, size int) (*testEnv, []byte) {
	t.Helper()
	dir := t.TempDir()
	data := make([]byte, size)
	rand.Read(data)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig()
	cfg.Files = map[string]config.FileData{"docs": {Path: dir}}
	cfg.DownloadLimitEnabled, cfg.DownloadLimitMbit = true, 8
	return newEnv(t, cfg), data
}

func TestDownloadFollowsTheLimit(t *testing.T) {
	e, data := limitedShare(t, 64<<10+1_000_000)
	start := time.Now()
	rec := send(e.handler, http.MethodGet, "/docs/big.bin", nil)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Errorf("1 MB beyond the burst at 8 Mbit/s took %v, want about 1 s", d)
	}
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), data) {
		t.Errorf("download: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestRangeRequestUnderTheLimit(t *testing.T) {
	e, data := limitedShare(t, 300_000)
	rec := send(e.handler, http.MethodGet, "/docs/big.bin", nil, withHeader("Range", "bytes=100000-199999"))
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), data[100000:200000]) {
		t.Errorf("range: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 100000-199999/300000" {
		t.Errorf("Content-Range = %q", got)
	}
}
