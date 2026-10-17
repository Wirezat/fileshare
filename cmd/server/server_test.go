package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/logstore"
)

func TestNewHTTPServerFailsOnAMissingTemplate(t *testing.T) {
	assets := t.TempDir()
	if err := os.CopyFS(assets, os.DirFS("../../assets/web")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(assets, "html", "gate.html")); err != nil {
		t.Fatal(err)
	}
	_, err := newHTTPServer(deps{Logs: logstore.New(), Assets: assets, Started: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "gate.html") {
		t.Fatalf("err = %v, want an error naming gate.html", err)
	}
}

func TestTwoServersShareNoSessions(t *testing.T) {
	e := newEnv(t, baseConfig())
	h := e.handler
	cookie := e.cookie(t)
	if rec := send(h, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie)); rec.Code != http.StatusOK {
		t.Fatalf("own server: %d, want 200", rec.Code)
	}

	t.Run("other server", func(t *testing.T) {
		other := newEnv(t, baseConfig()).handler
		if rec := send(other, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie)); rec.Code != http.StatusUnauthorized {
			t.Errorf("session of one server accepted by another: %d", rec.Code)
		}
	})

	if rec := send(h, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie)); rec.Code != http.StatusOK {
		t.Errorf("building a second server ended the first server's session: %d", rec.Code)
	}
}

func TestPublicRequestsReachTheRequestLog(t *testing.T) {
	e := newEnv(t, baseConfig())
	var log bytes.Buffer
	d := e.srv.deps
	d.RequestLog = &log
	s, err := newHTTPServer(d)
	if err != nil {
		t.Fatal(err)
	}

	send(s.handler(), http.MethodGet, "/docs/a.txt", nil)
	send(s.handler(), http.MethodGet, "/admin/login", nil)

	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"message":"GET /docs/a.txt"`) {
		t.Errorf("request log = %q, want one line for the public request", log.String())
	}
}
