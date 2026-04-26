package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// basicAuth wraps a handler with HTTP Basic Auth against the admin credentials.
func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, err := shared.LoadConfig()
		if err != nil {
			GoLog.Errorf("basicAuth: failed to load config: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		// redirect to setup page if no admin password is set — forces user to set a password on first run.
		if config.AdminPassword == "" {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if username != config.AdminUsername || !shared.CheckPassword(password, config.AdminPassword) {
			GoLog.Warnf("basicAuth: failed login attempt from %s", clientIP(r))
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// handleAdminSettingsMaxPostSize updates the maximum per-chunk POST size.
// PATCH /admin/api/settings/max_post_size
// Body: {"maxPostSize": 94371840}
func handleAdminSettingsMaxPostSize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxPostSize int `json:"maxPostSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MaxPostSize < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.MaxPostSize = body.MaxPostSize
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminSettingsChunkInactivityTimeout updates the inactivity timeout
// for incomplete chunked upload sessions.
// PATCH /admin/api/settings/chunk_inactivity_timeout
// Body: {"chunkInactivityTimeout": 1800}  (seconds)
func handleAdminSettingsChunkInactivityTimeout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seconds int `json:"chunkInactivityTimeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Seconds < 60 {
		http.Error(w, "Bad Request: minimum 60 seconds", http.StatusBadRequest)
		return
	}

	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.ChunkInactivityTimeout = body.Seconds
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Apply immediately — no restart required.
	storage.SetInactivityTimeout(time.Duration(body.Seconds) * time.Second)
	w.WriteHeader(http.StatusNoContent)
}
