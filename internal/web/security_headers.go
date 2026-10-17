package web

import "net/http"

// securityHeaders sets HSTS (over TLS only), nosniff, referrer and frame headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")

		next.ServeHTTP(w, r)
	})
}
