package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wirezat/GoLog"
)

// staticAsset serves the file rel inside the assets directory, revalidated on every use.
func (s *Server) staticAsset(rel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, s.asset(rel))
	}
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
	GoLog.Infof("%s: Client: %s", GoLog.IP(clientIP(r)), preventClientLogInjection(payload.Message))
	w.WriteHeader(http.StatusNoContent)
}
