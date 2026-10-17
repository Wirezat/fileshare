package main

import (
	"net/http"
	"time"
)

const adminSessionCookie = "admin_session"

// currentAdminToken returns the session token carried by this request, if any.
func currentAdminToken(r *http.Request) string {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func (s *server) hasAdminCookie(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return false
	}
	return s.deps.Auth.Valid(cookie.Value)
}

// setAdminCookie issues the session cookie; Secure follows the actual connection.
func setAdminCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAdminCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
