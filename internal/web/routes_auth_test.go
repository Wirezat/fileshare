package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
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
	e := newEnv(t, configWithAdmin(t, "secret"))
	h := e.handler

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
	e := newEnv(t, configWithAdmin(t, "secret"))
	h := e.handler

	for _, tc := range []struct{ user, pass string }{{"admin", "wrong"}, {"other", "secret"}} {
		res := login(h, tc.user, tc.pass)
		if res.code != http.StatusUnauthorized || sessionCookie(res.cookies) != nil {
			t.Errorf("%s/%s: status = %d, cookie set = %v", tc.user, tc.pass, res.code, sessionCookie(res.cookies) != nil)
		}
	}
}

func TestLoginIsRateLimitedAfterFiveFailures(t *testing.T) {
	e := newEnv(t, configWithAdmin(t, "secret"))
	h := e.handler

	for i := 0; i < ratelimit.MaxFailures; i++ {
		login(h, "admin", "wrong")
	}
	res := login(h, "admin", "secret")
	if res.code != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q, want 429 with Retry-After", res.code, res.header.Get("Retry-After"))
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	e := newEnv(t, baseConfig())
	h := e.handler
	cookie := e.cookie(t)

	rec := send(h, http.MethodGet, "/admin/logout", nil, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("%d → %q, want 303 → /admin/login", rec.Code, rec.Header().Get("Location"))
	}
	if rec := send(h, http.MethodGet, "/admin/api/me", nil, asFetch, withCookie(cookie)); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session still accepted: %d", rec.Code)
	}
}

func TestLoginPageBeforeSetupRedirects(t *testing.T) {
	cfg := baseConfig()
	cfg.AdminPassword = ""
	e := newEnv(t, cfg)
	h := e.handler

	rec := send(h, http.MethodGet, "/admin/login", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("%d → %q, want 302 → /setup", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginPageAfterSetup(t *testing.T) {
	h := newEnv(t, baseConfig()).handler

	rec := send(h, http.MethodGet, "/admin/login", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%d %q, want 200 text/html", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func setupInit(h http.Handler, code, pass string) int {
	body := `{"new_username":"boss","new_password":"` + pass + `","setup_token":"` + code + `"}`
	return send(h, http.MethodPost, "/setup/api/init", strings.NewReader(body),
		withHeader("Content-Type", "application/json")).Code
}

func TestSetupFlow(t *testing.T) {
	cfg := baseConfig()
	cfg.AdminPassword = ""
	e := newEnv(t, cfg)
	h := e.handler
	code := e.auth.SetupCode()

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
	if stored := e.stored(t); stored.AdminUsername != "boss" || !password.Check("pw", stored.AdminPassword) {
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
	cfg := baseConfig()
	cfg.AdminPassword = ""
	e := newEnv(t, cfg)
	h := e.handler

	for i := 0; i < ratelimit.MaxFailures; i++ {
		setupInit(h, "wrong", "pw")
	}
	if got := setupInit(h, e.auth.SetupCode(), "pw"); got != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", got)
	}
}

func TestLoginWithABrokenBodyIs400EvenWhileLocked(t *testing.T) {
	e := newEnv(t, configWithAdmin(t, "secret"))
	h := e.handler

	for range ratelimit.MaxFailures {
		login(h, "admin", "wrong")
	}
	rec := send(h, http.MethodPost, "/admin/login", strings.NewReader("{"), withHeader("Content-Type", "application/json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPasswordChangeIsRateLimitedAfterFiveWrongCurrentPasswords(t *testing.T) {
	e := newEnv(t, baseConfig())
	h := e.handler
	cookie := e.cookie(t)
	change := func(current string) *httptest.ResponseRecorder {
		body := `{"current_password":"` + current + `","new_password":"neu"}`
		return send(h, http.MethodPost, "/admin/api/settings/password", strings.NewReader(body),
			asFetch, withCookie(cookie), withHeader("Content-Type", "application/json"))
	}

	for range ratelimit.MaxFailures {
		if rec := change("wrong"); rec.Code != http.StatusForbidden {
			t.Fatalf("wrong current password: %d, want 403", rec.Code)
		}
	}
	rec := change(testAdminPassword)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q, want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestSetupWithABrokenBodyIs400EvenWhileLocked(t *testing.T) {
	cfg := baseConfig()
	cfg.AdminPassword = ""
	e := newEnv(t, cfg)
	h := e.handler

	for range ratelimit.MaxFailures {
		setupInit(h, "wrong", "pw")
	}
	rec := send(h, http.MethodPost, "/setup/api/init", strings.NewReader("{"), withHeader("Content-Type", "application/json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
