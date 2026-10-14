package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

func configWithAdmin(t *testing.T, pw string) *config.Config {
	t.Helper()
	hash, err := password.Hash(pw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig()
	cfg.AdminPassword = hash
	return cfg
}

func login(h http.Handler, user, pass string) *loginResult {
	body := `{"username":"` + user + `","password":"` + pass + `"}`
	rec := send(h, http.MethodPost, "/admin/login", strings.NewReader(body),
		withHeader("Content-Type", "application/json"))
	return &loginResult{rec.Code, rec.Result().Cookies(), rec.Header()}
}

type loginResult struct {
	code    int
	cookies []*http.Cookie
	header  http.Header
}

func sessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == adminSessionCookie {
			return c
		}
	}
	return nil
}

func TestLoginSetsAStrictHttpOnlySessionCookie(t *testing.T) {
	h := newServer(t)
	seedConfig(t, configWithAdmin(t, "secret"))

	res := login(h, "admin", "secret")

	if res.code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.code)
	}
	c := sessionCookie(res.cookies)
	if c == nil || c.Value == "" {
		t.Fatal("no session cookie")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" {
		t.Errorf("cookie flags: HttpOnly=%v SameSite=%v Path=%q", c.HttpOnly, c.SameSite, c.Path)
	}
	if rec := send(h, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(c)); rec.Code != http.StatusOK {
		t.Errorf("session not accepted: %d", rec.Code)
	}
}

func TestLoginRejectsWrongCredentials(t *testing.T) {
	h := newServer(t)
	seedConfig(t, configWithAdmin(t, "secret"))

	for _, tc := range []struct{ user, pass string }{{"admin", "wrong"}, {"other", "secret"}} {
		res := login(h, tc.user, tc.pass)
		if res.code != http.StatusUnauthorized || sessionCookie(res.cookies) != nil {
			t.Errorf("%s/%s: status = %d, cookie set = %v", tc.user, tc.pass, res.code, sessionCookie(res.cookies) != nil)
		}
	}
}

func TestLoginIsRateLimitedAfterFiveFailures(t *testing.T) {
	h := newServer(t)
	seedConfig(t, configWithAdmin(t, "secret"))

	for i := 0; i < maxFailures; i++ {
		login(h, "admin", "wrong")
	}
	res := login(h, "admin", "secret")
	if res.code != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q, want 429 with Retry-After", res.code, res.header.Get("Retry-After"))
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	h := newServer(t)
	seedConfig(t, baseConfig())
	cookie := adminCookie(t)

	rec := send(h, http.MethodGet, "/admin/logout", nil, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("%d → %q, want 303 → /admin/login", rec.Code, rec.Header().Get("Location"))
	}
	if rec := send(h, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie)); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session still accepted: %d", rec.Code)
	}
}

func TestLoginPageBeforeSetupRedirects(t *testing.T) {
	h := newServer(t)
	cfg := baseConfig()
	cfg.AdminPassword = ""
	seedConfig(t, cfg)

	rec := send(h, http.MethodGet, "/admin/login", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("%d → %q, want 302 → /setup", rec.Code, rec.Header().Get("Location"))
	}
}

func setupInit(h http.Handler, code, pass string) int {
	body := `{"new_username":"boss","new_password":"` + pass + `","setup_token":"` + code + `"}`
	return send(h, http.MethodPost, "/setup/api/init", strings.NewReader(body),
		withHeader("Content-Type", "application/json")).Code
}

func TestSetupFlow(t *testing.T) {
	h := newServer(t)
	cfg := baseConfig()
	cfg.AdminPassword = ""
	seedConfig(t, cfg)
	initSetupToken(cfgStore.Current())
	code := setupToken

	if rec := send(h, http.MethodGet, "/setup", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /setup = %d, want 200", rec.Code)
	}
	if got := setupInit(h, "wrong", "pw"); got != http.StatusForbidden {
		t.Fatalf("wrong code: %d, want 403", got)
	}
	if got := setupInit(h, code, ""); got != http.StatusBadRequest {
		t.Fatalf("empty password: %d, want 400", got)
	}
	if got := setupInit(h, code, "pw"); got != http.StatusNoContent {
		t.Fatalf("right code: %d, want 204", got)
	}
	if stored := storedConfig(t); stored.AdminUsername != "boss" || !password.Check("pw", stored.AdminPassword) {
		t.Errorf("credentials not stored: user %q", stored.AdminUsername)
	}
	if got := setupInit(h, code, "again"); got != http.StatusForbidden {
		t.Errorf("second setup: %d, want 403", got)
	}
	if rec := send(h, http.MethodGet, "/setup", nil); rec.Code == http.StatusOK {
		t.Errorf("GET /setup after setup = 200, want a redirect")
	}
}

func TestSetupIsRateLimited(t *testing.T) {
	h := newServer(t)
	cfg := baseConfig()
	cfg.AdminPassword = ""
	seedConfig(t, cfg)
	initSetupToken(cfgStore.Current())

	for i := 0; i < maxFailures; i++ {
		setupInit(h, "wrong", "pw")
	}
	if got := setupInit(h, setupToken, "pw"); got != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", got)
	}
}
