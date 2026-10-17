package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withConfig seeds a server that is past setup.
func withConfig(t *testing.T) *testEnv {
	t.Helper()
	return newEnv(t, baseConfig())
}

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestAdminAuthRedirectsPageNavigation(t *testing.T) {
	e := withConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()

	e.srv.adminAuth(okHandler)(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("Location = %q, want /admin/login", loc)
	}
}

func TestAdminAuthRejectsScriptRequestsWith401(t *testing.T) {
	e := withConfig(t)

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

			e.srv.adminAuth(okHandler)(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("script request got redirected to %q", loc)
			}
		})
	}
}

func TestAdminAuthFallsBackToAcceptHeader(t *testing.T) {
	e := withConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	e.srv.adminAuth(okHandler)(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("html client: status = %d, want %d", rec.Code, http.StatusFound)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/shares", nil)
	rec = httptest.NewRecorder()
	e.srv.adminAuth(okHandler)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api client: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCredentialChangeRejectsWithForbiddenNotUnauthorized(t *testing.T) {
	e := withConfig(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		body    string
	}{
		{
			"change username",
			e.srv.handleAdminSettingsUsername,
			`{"current_password":"wrong","new_username":"other"}`,
		},
		{
			"change password",
			e.srv.handleAdminSettingsPassword,
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
