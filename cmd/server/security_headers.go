package main

import "net/http"

// securityHeaders sets HSTS, nosniff, referrer and frame-ancestors headers on
// every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// HSTS only over TLS; a plain-HTTP install must never emit it.
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		// Set, not Add: serveShareFile replaces it with its sandbox policy.
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY") // for browsers predating frame-ancestors

		next.ServeHTTP(w, r)
	})
}
