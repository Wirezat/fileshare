package main

import (
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// basicAuth wraps a handler with HTTP Basic Auth against the admin credentials.
func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		config, err := shared.LoadConfig()
		if err != nil {
			GoLog.Errorf("basicAuth: failed to load config: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
