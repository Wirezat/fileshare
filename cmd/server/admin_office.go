package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// normalizeOfficeURL validates a document server base URL and strips the
// trailing slash, query and fragment. An empty string is allowed and turns the
// integration off.
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
// The secret is write-only: GET reports only whether one is stored. On PATCH an
// omitted field keeps its stored value and an empty string deletes it, so the
// UI can save a URL change without ever holding the secret.
func handleAdminSettingsOffice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		config, ok := configOrErr(w)
		if !ok {
			return
		}
		jsonResponse(w, map[string]any{
			"office_url": config.OfficeURL,
			"secret_set": config.OfficeSecret != "",
		})

	case http.MethodPatch:
		var req struct {
			OfficeURL    *string `json:"office_url"`
			OfficeSecret *string `json:"office_secret"`
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		config, ok := configOrErr(w)
		if !ok {
			return
		}

		if req.OfficeURL != nil {
			normalized, err := normalizeOfficeURL(*req.OfficeURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			config.OfficeURL = normalized
		}
		if req.OfficeSecret != nil {
			config.OfficeSecret = strings.TrimSpace(*req.OfficeSecret)
		}
		if !saveOrErr(w, config) {
			return
		}

		logOfficeChange(config)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// logOfficeChange records the new state without ever writing the secret itself.
func logOfficeChange(config *shared.Config) {
	switch {
	case config.OfficeURL == "":
		GoLog.Infof("office integration disabled: no document server URL")
	case config.OfficeSecret == "":
		GoLog.Warnf("office document server set to %s, but no shared secret is stored", config.OfficeURL)
	default:
		GoLog.Infof("office document server set to %s (shared secret stored)", config.OfficeURL)
	}
}
