package main

import (
	"net/http"
	"time"
)

const (
	adminSessionCookie     = "admin_session"
	adminTokenTTL          = 7 * 24 * time.Hour // only map cleanup, not actual session TTL since we don't update expiry on use
	adminTokenReapInterval = 15 * time.Minute
)

var adminSessions = newTTLStore[struct{}]()

func generateAdminToken() (string, error) { return generateToken() }

func storeAdminToken(token string) {
	adminSessions.set(token, struct{}{}, adminTokenTTL)
}

func validateAdminToken(token string) bool {
	_, ok := adminSessions.get(token)
	return ok
}

func deleteAdminToken(token string) {
	adminSessions.delete(token)
}

// deleteAdminTokensExcept drops every session but the one given and reports
// how many it removed.
func deleteAdminTokensExcept(keep string) int {
	return adminSessions.deleteExcept(keep)
}

// currentAdminToken returns the session token carried by this request, if any.
func currentAdminToken(r *http.Request) string {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func hasAdminCookie(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return false
	}
	return validateAdminToken(cookie.Value)
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

func startAdminTokenReaper() {
	adminSessions.startReaper(adminTokenReapInterval)
}
