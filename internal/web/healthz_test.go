package web

import (
	"net/http"
	"testing"
)

func TestHealthz(t *testing.T) {
	e := publicEnv(t, nil)
	rec := send(e.handler, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("healthz: %d %q", rec.Code, rec.Body.String())
	}
	if entries := e.srv.deps.Logs.Recent(10); len(entries) != 0 {
		t.Errorf("request log = %v, want empty", entries)
	}
}
