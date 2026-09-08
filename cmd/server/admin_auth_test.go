package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// withConfig writes a data.json next to the package so shared.LoadConfig — which
// reads "./data.json" relative to the working directory — finds one with a
// password set, i.e. a server that is past setup.
func withConfig(t *testing.T) {
	t.Helper()
	const cfg = `{"admin_username":"admin","admin_password":"$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaA","files":{}}`
	if err := os.WriteFile("data.json", []byte(cfg), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	t.Cleanup(func() { os.Remove("data.json") })
}

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

// A browser navigating to an admin page with no session belongs on the login
// page, so the redirect stays.
func TestAdminAuthRedirectsPageNavigation(t *testing.T) {
	withConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()

	adminAuth(okHandler)(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("Location = %q, want /admin/login", loc)
	}
}

// A fetch() must not be redirected: it follows the 302 without telling the
// caller, so the admin UI parses the login page as JSON (GET) or has its
// PATCH/DELETE replayed against /admin/login, which answers 405.
func TestAdminAuthRejectsScriptRequestsWith401(t *testing.T) {
	withConfig(t)

	cases := []struct {
		name   string
		method string
		mode   string
	}{
		{"GET shares", http.MethodGet, "cors"},
		{"PATCH share", http.MethodPatch, "cors"},
		{"DELETE share", http.MethodDelete, "cors"},
		{"same-origin fetch", http.MethodGet, "same-origin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/admin/api/shares?subpath=x", nil)
			req.Header.Set("Sec-Fetch-Mode", tc.mode)
			req.Header.Set("Accept", "*/*")
			rec := httptest.NewRecorder()

			adminAuth(okHandler)(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("script request got redirected to %q", loc)
			}
		})
	}
}

// Clients that send no Sec-Fetch-Mode at all fall back to what they accept: a
// browser navigation asks for HTML, fetch() defaults to */*.
func TestAdminAuthFallsBackToAcceptHeader(t *testing.T) {
	withConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	adminAuth(okHandler)(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("html client: status = %d, want %d", rec.Code, http.StatusFound)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/shares", nil)
	rec = httptest.NewRecorder()
	adminAuth(okHandler)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api client: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// A wrong current password is not a dead session. The admin UI routes every
// call through wui's session layer, which reads 401 as "signed out" and
// navigates to the login page — so answering a typo with 401 would sign the
// admin out instead of showing the error next to the field.
func TestCredentialChangeRejectsWithForbiddenNotUnauthorized(t *testing.T) {
	withConfig(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		body    string
	}{
		{
			"change username",
			handleAdminSettingsUsername,
			`{"current_password":"wrong","new_username":"other"}`,
		},
		{
			"change password",
			handleAdminSettingsPassword,
			`{"current_password":"wrong","new_password":"other"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/api/settings", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			tc.handler(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("answered 401 — the session layer would sign the admin out over a typo")
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}
}
