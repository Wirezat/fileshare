package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func patchWebhook(e *testEnv, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/settings/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.srv.handleAdminSettingsWebhook(rec, req)
	return rec
}

func getWebhook(e *testEnv) (server, topic, token string) {
	rec := httptest.NewRecorder()
	e.srv.handleAdminSettingsWebhook(rec, httptest.NewRequest(http.MethodGet, "/admin/api/settings/webhook", nil))
	var got struct {
		Server string `json:"webhook_server"`
		Topic  string `json:"webhook_topic"`
		Token  string `json:"webhook_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	return got.Server, got.Topic, got.Token
}

func TestWebhookSettingsPatchStoresAndGetReturnsAll(t *testing.T) {
	e := newEnv(t, baseConfig())

	body := `{"webhook_server":" https://ntfy.example.com/ ","webhook_topic":" my-topic_1 ","webhook_token":" abc123 "}`
	if rec := patchWebhook(e, body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	server, topic, token := getWebhook(e)
	if server != "https://ntfy.example.com" || topic != "my-topic_1" || token != "abc123" {
		t.Errorf("GET = %q %q %q", server, topic, token)
	}
}

func TestWebhookSettingsOmittedFieldsStayAndEmptyOnesDelete(t *testing.T) {
	cfg := baseConfig()
	cfg.WebhookServer = "https://ntfy.sh"
	cfg.WebhookTopic = "topic"
	cfg.WebhookToken = "abc123"
	e := newEnv(t, cfg)

	patchWebhook(e, `{"webhook_topic":"other"}`)
	if server, topic, token := getWebhook(e); server != "https://ntfy.sh" || topic != "other" || token != "abc123" {
		t.Errorf("after topic change: %q %q %q", server, topic, token)
	}
	patchWebhook(e, `{"webhook_topic":"","webhook_token":""}`)
	if _, topic, token := getWebhook(e); topic != "" || token != "" {
		t.Errorf("after clearing: topic %q token %q", topic, token)
	}
}

func TestWebhookSettingsPatchRejectsBadInput(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"webhook_server":"ntfy.sh"}`,
		`{"webhook_server":"ftp://x"}`,
		`{"webhook_topic":"a/b"}`,
		`{"webhook_topic":"mit leerzeichen"}`,
		`{"webhook_topic":"` + strings.Repeat("a", 65) + `"}`,
		`nope`,
	} {
		e := newEnv(t, baseConfig())
		if rec := patchWebhook(e, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
}
