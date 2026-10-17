package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wirezat/GoLog"
)

// handleShareCSS and handleShareJS serve static assets.
func (s *server) handleShareCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("css/share.css"))
}

func (s *server) handleShareJS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("js/share.js"))
}

// handleLogEvent allows the client to send log messages to the server.
func handleLogEvent(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Message string `json:"message"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	GoLog.Infof("%s: Client: %s", clientIP(r), preventClientLogInjection(payload.Message))
	w.WriteHeader(http.StatusNoContent)
}
