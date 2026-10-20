package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/ratelimit"
)

func TestLogsHoldNoFullIPHeaderOrBody(t *testing.T) {
	GoLog.SetPrivacy(true)
	t.Cleanup(func() { GoLog.SetPrivacy(false) })
	var mu sync.Mutex
	var lines []string
	GoLog.OnRecord = func(r GoLog.Record) {
		mu.Lock()
		lines = append(lines, r.Message)
		mu.Unlock()
	}
	t.Cleanup(func() { GoLog.OnRecord = nil })

	cfg := baseConfig()
	cfg.Files = map[string]config.FileData{"locked": {Path: shareDir(t), Password: mustHash("right")}}
	e := newEnv(t, cfg)
	var file bytes.Buffer
	e.srv.deps.RequestLog = &file

	body := url.Values{"password": {"hunter2"}}.Encode()
	targets := []string{"/lib/terminal-xhr.php"}
	for range ratelimit.MaxFailures + 1 {
		targets = append(targets, "/locked/unlock")
	}
	for _, target := range targets {
		send(e.handler, http.MethodPost, target, strings.NewReader(body),
			withHeader("Content-Type", "application/x-www-form-urlencoded"),
			withHeader("X-Secret", "topsecret"),
			withHeader("User-Agent", "probe/1.0"),
			func(r *http.Request) { r.RemoteAddr = "198.51.100.77:4000" })
	}

	var all strings.Builder
	all.WriteString(file.String())
	for _, entry := range e.srv.deps.Logs.Recent(100) {
		all.WriteString(entry.Message)
		all.Write(entry.Request)
	}
	mu.Lock()
	all.WriteString(strings.Join(lines, "\n"))
	mu.Unlock()
	got := all.String()

	for _, leak := range []string{"198.51.100.77", "hunter2", "topsecret", "X-Secret"} {
		if strings.Contains(got, leak) {
			t.Errorf("logs contain %q:\n%s", leak, got)
		}
	}
	for _, want := range []string{"198.51.100.0", "probe/1.0", "POST /lib/terminal-xhr.php", "failed unlock attempt", "rate limited 198.51.100.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("logs miss %q:\n%s", want, got)
		}
	}
}
