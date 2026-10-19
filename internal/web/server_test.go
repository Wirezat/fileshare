package web

import (
	"bytes"
	"encoding/json"
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
	_, err := New(Deps{Logs: logstore.New(), Assets: assets, Started: time.Now()})
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
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}

	send(s.Handler(), http.MethodGet, "/docs/a.txt", nil)
	send(s.Handler(), http.MethodGet, "/admin/login", nil)

	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"message":"GET /docs/a.txt"`) {
		t.Errorf("request log = %q, want one line for the public request", log.String())
	}
	if entries := d.Logs.Recent(10); len(entries) != 1 || entries[0].Message != "GET /docs/a.txt" {
		t.Errorf("admin log buffer = %+v, want one entry for the public request", entries)
	}
}

func TestUIAssetsComeFromTheAssetsDir(t *testing.T) {
	e := newEnv(t, baseConfig())
	assets := t.TempDir()
	if err := os.CopyFS(filepath.Join(assets, "html"), os.DirFS("../../assets/web/html")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(assets, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "ui", "probe.css"), []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := e.srv.deps
	d.Assets = assets
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}

	rec := send(s.Handler(), http.MethodGet, "/static/ui/probe.css", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "probe" {
		t.Errorf("%d %q, want the file from <assets>/ui", rec.Code, rec.Body.String())
	}
}

type refusingStore struct{ configStore }

func (refusingStore) Refused() string {
	return "data.json has revision 1, older than the server's last save (2)"
}

func TestAdminMeReportsARefusedConfig(t *testing.T) {
	e := newEnv(t, baseConfig())
	cookie := e.cookie(t)
	d := e.srv.deps
	d.Store = refusingStore{d.Store}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}

	rec := send(s.Handler(), http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie))
	var me struct {
		ConfigProblem string `json:"config_problem"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(me.ConfigProblem, "older than the server's last save") {
		t.Errorf("config_problem = %q, want the refusal reason", me.ConfigProblem)
	}
}
