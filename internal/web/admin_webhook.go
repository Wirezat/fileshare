package web

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
)

var topicName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// normalizeWebhookServer validates an ntfy server base URL and strips trailing slash, query and fragment; "" stays empty.
func normalizeWebhookServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("ntfy server must look like https://ntfy.sh")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// normalizeWebhookTopic validates an ntfy topic name; "" turns the notification off.
func normalizeWebhookTopic(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !topicName.MatchString(raw) {
		return "", errors.New("topic may only contain letters, digits, - and _ (up to 64 characters)")
	}
	return raw, nil
}

// handleAdminSettingsWebhook reads and sets where a message goes for every finished upload: the ntfy server, the topic and the access token.
//
//	GET   /admin/api/settings/webhook → {"webhook_server":"…","webhook_topic":"…","webhook_token":"…"}
//	PATCH /admin/api/settings/webhook   Body: any of the three fields
//
// On PATCH an omitted field keeps its value and "" deletes it; an empty topic turns the notification off.
func (s *Server) handleAdminSettingsWebhook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.deps.Store.Current()
		jsonResponse(w, map[string]any{
			"webhook_server": cfg.WebhookServer(),
			"webhook_topic":  cfg.WebhookTopic(),
			"webhook_token":  cfg.WebhookToken(),
		})

	case http.MethodPatch:
		var req struct {
			WebhookServer *string `json:"webhook_server"`
			WebhookTopic  *string `json:"webhook_topic"`
			WebhookToken  *string `json:"webhook_token"`
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		if req.WebhookServer == nil && req.WebhookTopic == nil && req.WebhookToken == nil {
			http.Error(w, "webhook_server, webhook_topic or webhook_token is required", http.StatusBadRequest)
			return
		}
		var server, topic string
		var err error
		if req.WebhookServer != nil {
			if server, err = normalizeWebhookServer(*req.WebhookServer); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if req.WebhookTopic != nil {
			if topic, err = normalizeWebhookTopic(*req.WebhookTopic); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		var saved config.Config
		if !s.updateOrErr(w, func(c *config.Config) error {
			if req.WebhookServer != nil {
				c.WebhookServer = server
			}
			if req.WebhookTopic != nil {
				c.WebhookTopic = topic
			}
			if req.WebhookToken != nil {
				c.WebhookToken = strings.TrimSpace(*req.WebhookToken)
			}
			saved = *c
			return nil
		}) {
			return
		}
		if saved.WebhookServer == "" || saved.WebhookTopic == "" {
			GoLog.Infof("upload webhook disabled")
		} else {
			GoLog.Infof("upload webhook enabled")
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
