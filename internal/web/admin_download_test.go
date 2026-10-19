package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func downloadLimit(t *testing.T, e *testEnv, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/admin/api/settings/download_limit", strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.srv.handleAdminSettingsDownloadLimit(rec, req)
	return rec
}

func TestDownloadLimitDefaultsToOff(t *testing.T) {
	e := newEnv(t, baseConfig())
	rec := downloadLimit(t, e, http.MethodGet, "")
	var got struct {
		Enabled bool `json:"enabled"`
		Mbit    int  `json:"mbit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Enabled || got.Mbit != 0 {
		t.Errorf("GET = %d %s, want off with no rate", rec.Code, rec.Body.String())
	}
}

func TestDownloadLimitIsSavedAndTakesEffect(t *testing.T) {
	e := newEnv(t, baseConfig())
	if rec := downloadLimit(t, e, http.MethodPatch, `{"enabled":true,"mbit":50}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body.String())
	}
	if c := e.stored(t); !c.DownloadLimitEnabled || c.DownloadLimitMbit != 50 {
		t.Errorf("stored = %v, %d; want true, 50", c.DownloadLimitEnabled, c.DownloadLimitMbit)
	}
	if on, mbit := e.store.Current().DownloadLimit(); !on || mbit != 50 {
		t.Errorf("current = %v, %d; want true, 50", on, mbit)
	}

	if rec := downloadLimit(t, e, http.MethodPatch, `{"enabled":false}`); rec.Code != http.StatusNoContent {
		t.Fatalf("switch off = %d", rec.Code)
	}
	if c := e.stored(t); c.DownloadLimitEnabled || c.DownloadLimitMbit != 50 {
		t.Errorf("after switching off = %v, %d; want false with the rate kept", c.DownloadLimitEnabled, c.DownloadLimitMbit)
	}
	if rec := downloadLimit(t, e, http.MethodGet, ""); !strings.Contains(rec.Body.String(), `"mbit":50`) {
		t.Errorf("GET = %s, want the kept rate", rec.Body.String())
	}
}

func TestInvalidDownloadLimitIsRefused(t *testing.T) {
	e := newEnv(t, baseConfig())
	for _, body := range []string{
		`{"enabled":true}`,
		`{"enabled":true,"mbit":0}`,
		`{"enabled":true,"mbit":100001}`,
		`{"enabled":false,"mbit":-1}`,
		`{"mbit":10}`,
		`{"enabled":"ja"}`,
	} {
		if rec := downloadLimit(t, e, http.MethodPatch, body); rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s = %d, want 400", body, rec.Code)
		}
	}
	if c := e.stored(t); c.DownloadLimitEnabled || c.DownloadLimitMbit != 0 {
		t.Errorf("stored after refusals = %v, %d; want unchanged", c.DownloadLimitEnabled, c.DownloadLimitMbit)
	}
}

func TestDownloadLimitOtherMethods(t *testing.T) {
	e := newEnv(t, baseConfig())
	if rec := downloadLimit(t, e, http.MethodPost, `{"enabled":true,"mbit":5}`); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}
}
