package main

import "net/http"

// securityHeaders sets the response headers that close off transport and
// browser-side attack paths. Applied to every response, including static files
// and error pages.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Only announced over a TLS connection. A plain-HTTP install must never
		// emit this — the browser would remember it and refuse to reach the
		// server for a year.
		//
		// This is the other half of the Secure cookie: Secure keeps the session
		// off a plaintext request, HSTS stops the browser making one at all.
		// No includeSubDomains: neighbouring hosts on the domain are not this
		// server's to speak for.
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}

		// Shared files are user-supplied. Without this a browser may sniff a
		// .txt into HTML and run script from it on this origin.
		h.Set("X-Content-Type-Options", "nosniff")

		// Share URLs are the secret in this app. Leaking one through a Referer
		// header to whatever a listing links to would hand it away.
		h.Set("Referrer-Policy", "same-origin")

		// Clickjacking: nothing here is meant to be embedded, and the admin
		// panel least of all. Set rather than added, so serveShareFile can
		// replace it with its stricter sandbox policy further down the chain.
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY") // for browsers predating frame-ancestors

		next.ServeHTTP(w, r)
	})
}
