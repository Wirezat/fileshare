package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
)

// normalizeOfficeURL validates a document server base URL and strips trailing slash, query and fragment; "" turns the integration off.
func normalizeOfficeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("document server URL must look like https://office.example.com")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// handleAdminSettingsOffice reads and updates the document server connection.
//
//	GET   /admin/api/settings/office → {"office_url":"…","secret_set":true}
//	PATCH /admin/api/settings/office   Body: {"office_url":"…","office_secret":"…"}
//
// The secret is write-only. On PATCH an omitted field keeps its value and "" deletes it.
func (s *server) handleAdminSettingsOffice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		cfg := s.deps.Store.Current()
		jsonResponse(w, map[string]any{
			"office_url": cfg.OfficeURL(),
			"secret_set": cfg.OfficeSecret() != "",
		})

	case http.MethodPatch:
		var req struct {
			OfficeURL    *string `json:"office_url"`
			OfficeSecret *string `json:"office_secret"`
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		var normalized string
		if req.OfficeURL != nil {
			var err error
			if normalized, err = normalizeOfficeURL(*req.OfficeURL); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		var saved config.Config
		if !s.updateOrErr(w, func(c *config.Config) error {
			if req.OfficeURL != nil {
				c.OfficeURL = normalized
			}
			if req.OfficeSecret != nil {
				c.OfficeSecret = strings.TrimSpace(*req.OfficeSecret)
			}
			saved = *c
			return nil
		}) {
			return
		}

		logOfficeChange(&saved)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// logOfficeChange records the new state without ever writing the secret itself.
func logOfficeChange(config *config.Config) {
	switch {
	case config.OfficeURL == "":
		GoLog.Infof("office integration disabled: no document server URL")
	case config.OfficeSecret == "":
		GoLog.Warnf("office document server set to %s, but no shared secret is stored", config.OfficeURL)
	default:
		GoLog.Infof("office document server set to %s (shared secret stored)", config.OfficeURL)
	}
}
