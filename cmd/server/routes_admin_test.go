package main

import (
	"net/http"
	"strings"
	"testing"
)

var adminRouteTable = []struct {
	method string
	path   string
	authed int
}{
	{http.MethodGet, "/admin", http.StatusOK},
	{http.MethodGet, "/admin/logs", http.StatusOK},
	{http.MethodGet, "/admin/settings", http.StatusOK},
	{http.MethodGet, "/admin/static/js/settings.js", http.StatusOK},
	{http.MethodGet, "/admin/api/shares", http.StatusOK},
	{http.MethodGet, "/admin/api/shares/qr?subpath=missing", http.StatusNotFound},
	{http.MethodGet, "/admin/api/browse?path=/", http.StatusOK},
	{http.MethodGet, "/admin/api/logs", http.StatusOK},
	{http.MethodGet, "/admin/api/me", http.StatusOK},
	{http.MethodGet, "/admin/api/uptime", http.StatusOK},
	{http.MethodGet, "/admin/api/settings/office", http.StatusOK},
	{http.MethodPost, "/admin/api/settings/username", http.StatusBadRequest},
	{http.MethodPost, "/admin/api/settings/password", http.StatusBadRequest},
	{http.MethodPatch, "/admin/api/settings/max_post_size", http.StatusBadRequest},
	{http.MethodPatch, "/admin/api/settings/chunk_inactivity_timeout", http.StatusBadRequest},
	{http.MethodPost, "/admin/api/settings/prune_expired", http.StatusOK},
}

func TestAdminRoutesWithoutSession(t *testing.T) {
	h := newServer(t)
	seedConfig(t, baseConfig())

	for _, rt := range adminRouteTable {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if rec := send(h, rt.method, rt.path, nil, asFetch); rec.Code != http.StatusUnauthorized {
				t.Errorf("fetch: status = %d, want 401", rec.Code)
			}
			rec := send(h, rt.method, rt.path, nil, asNavigation)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/login" {
				t.Errorf("navigation: %d → %q, want 302 → /admin/login", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

func TestAdminRoutesBeforeSetupRedirectToSetup(t *testing.T) {
	h := newServer(t)
	cfg := baseConfig()
	cfg.AdminPassword = ""
	seedConfig(t, cfg)

	rec := send(h, http.MethodGet, "/admin", nil, asNavigation)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("%d → %q, want 302 → /setup", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminRoutesWithSession(t *testing.T) {
	h := newServer(t)
	seedConfig(t, baseConfig())
	cookie := adminCookie(t)

	for _, rt := range adminRouteTable {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := send(h, rt.method, rt.path, strings.NewReader(""), asFetch, withCookie(cookie))
			if rec.Code != rt.authed {
				t.Errorf("status = %d, want %d (%s)", rec.Code, rt.authed, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

func TestForgedSessionCookieIsRejected(t *testing.T) {
	h := newServer(t)
	seedConfig(t, baseConfig())

	rec := send(h, http.MethodGet, "/admin/api/shares", nil, asFetch,
		withCookie(&http.Cookie{Name: adminSessionCookie, Value: "forged"}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
