package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/pkg/shared"
)

const testAdminHash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaA"

// withOfficeConfig installs a config through SaveConfig so both the file and
// the in-process cache hold it, then restores the baseline other tests expect.
func withOfficeConfig(t *testing.T, officeURL, officeSecret string) {
	t.Helper()
	baseline := func() *shared.Config {
		return &shared.Config{
			AdminUsername: "admin",
			AdminPassword: testAdminHash,
			Files:         map[string]shared.FileData{},
		}
	}
	cfg := baseline()
	cfg.OfficeURL = officeURL
	cfg.OfficeSecret = officeSecret
	if err := shared.SaveConfig(cfg); err != nil {
		t.Fatalf("save test config: %v", err)
	}
	t.Cleanup(func() {
		shared.SaveConfig(baseline())
		os.Remove("data.json")
	})
}

func patchOffice(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/settings/office", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleAdminSettingsOffice(rec, req)
	return rec
}

func loadOffice(t *testing.T) *shared.Config {
	t.Helper()
	cfg, err := shared.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestOfficeSettingsGetNeverReturnsTheSecret(t *testing.T) {
	withOfficeConfig(t, "https://office.example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/api/settings/office", nil)
	rec := httptest.NewRecorder()
	handleAdminSettingsOffice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("response leaked the secret: %s", rec.Body.String())
	}

	var got struct {
		OfficeURL string `json:"office_url"`
		SecretSet bool   `json:"secret_set"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.OfficeURL != "https://office.example.com" {
		t.Errorf("office_url = %q", got.OfficeURL)
	}
	if !got.SecretSet {
		t.Error("secret_set = false, want true")
	}
}

func TestOfficeSettingsGetReportsUnsetSecret(t *testing.T) {
	withOfficeConfig(t, "", "")

	req := httptest.NewRequest(http.MethodGet, "/admin/api/settings/office", nil)
	rec := httptest.NewRecorder()
	handleAdminSettingsOffice(rec, req)

	var got struct {
		SecretSet bool `json:"secret_set"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.SecretSet {
		t.Error("secret_set = true, want false")
	}
}

func TestOfficeSettingsPatchStoresBothValues(t *testing.T) {
	withOfficeConfig(t, "", "")

	rec := patchOffice(t, `{"office_url":"https://office.example.com","office_secret":"abc123"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	cfg := loadOffice(t)
	if cfg.OfficeURL != "https://office.example.com" {
		t.Errorf("OfficeURL = %q", cfg.OfficeURL)
	}
	if cfg.OfficeSecret != "abc123" {
		t.Errorf("OfficeSecret = %q", cfg.OfficeSecret)
	}
}

// An omitted field means "leave it alone" — the UI never sends the stored
// secret back, so a URL-only edit must not wipe it.
func TestOfficeSettingsPatchLeavesOmittedSecretAlone(t *testing.T) {
	withOfficeConfig(t, "https://old.example.com", "keepme")

	rec := patchOffice(t, `{"office_url":"https://new.example.com"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	cfg := loadOffice(t)
	if cfg.OfficeURL != "https://new.example.com" {
		t.Errorf("OfficeURL = %q", cfg.OfficeURL)
	}
	if cfg.OfficeSecret != "keepme" {
		t.Errorf("OfficeSecret = %q, want it untouched", cfg.OfficeSecret)
	}
}

// An explicit empty string is how the UI deletes a value.
func TestOfficeSettingsPatchClearsWithEmptyStrings(t *testing.T) {
	withOfficeConfig(t, "https://office.example.com", "abc123")

	rec := patchOffice(t, `{"office_url":"","office_secret":""}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	cfg := loadOffice(t)
	if cfg.OfficeURL != "" || cfg.OfficeSecret != "" {
		t.Errorf("not cleared: url=%q secret=%q", cfg.OfficeURL, cfg.OfficeSecret)
	}
}

func TestOfficeSettingsPatchRejectsUnusableURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"no scheme", "office.example.com"},
		{"wrong scheme", "ftp://office.example.com"},
		{"no host", "https://"},
		{"javascript", "javascript:alert(1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withOfficeConfig(t, "https://good.example.com", "keepme")

			rec := patchOffice(t, `{"office_url":`+quote(tc.url)+`}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if cfg := loadOffice(t); cfg.OfficeURL != "https://good.example.com" {
				t.Errorf("rejected value was stored anyway: %q", cfg.OfficeURL)
			}
		})
	}
}

func TestOfficeSettingsPatchNormalizesURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://office.example.com/", "https://office.example.com"},
		{"https://office.example.com///", "https://office.example.com"},
		{"  https://office.example.com  ", "https://office.example.com"},
		{"https://office.example.com/ds?x=1", "https://office.example.com/ds"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			withOfficeConfig(t, "", "")

			rec := patchOffice(t, `{"office_url":`+quote(tc.in)+`}`)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
			}
			if got := loadOffice(t).OfficeURL; got != tc.want {
				t.Errorf("OfficeURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOfficeSettingsRejectsOtherMethods(t *testing.T) {
	withOfficeConfig(t, "", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/api/settings/office", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handleAdminSettingsOffice(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
