package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if raw := r.URL.Query().Get("n"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}

	entries := shared.Logger.Recent(n)
	if entries == nil {
		entries = []shared.LogEntry{} // return [] instead of null
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func handleAdminLogsStream(w http.ResponseWriter, r *http.Request) {
	// SSE requires these headers — flushing must be supported.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable Nginx buffering if present

	ch := shared.Logger.Subscribe()
	defer shared.Logger.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected — clean up via deferred Unsubscribe.
			return

		case entry, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		config, err := shared.LoadConfig()
		if err != nil || username != "admin" || !shared.CheckPassword(password, config.AdminPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}
