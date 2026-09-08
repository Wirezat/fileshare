package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	adminSessionCookie     = "admin_session"
	adminTokenTTL          = 7 * 24 * time.Hour //only map cleanup, not actual session TTL since we don't update expiry on use
	adminTokenReapInterval = 15 * time.Minute
)

type adminSession struct {
	expiresAt time.Time
}

var (
	adminSessionsMu sync.RWMutex
	adminSessions   = map[string]adminSession{}
)

func generateAdminToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func storeAdminToken(token string) {
	adminSessionsMu.Lock()
	adminSessions[token] = adminSession{
		expiresAt: time.Now().Add(adminTokenTTL),
	}
	adminSessionsMu.Unlock()
}

func validateAdminToken(token string) bool {
	adminSessionsMu.RLock()
	session, ok := adminSessions[token]
	adminSessionsMu.RUnlock()
	return ok && time.Now().Before(session.expiresAt)
}

func deleteAdminToken(token string) {
	adminSessionsMu.Lock()
	delete(adminSessions, token)
	adminSessionsMu.Unlock()
}

// deleteAdminTokensExcept drops every session but the one given, and reports
// how many it removed. Changing a credential has to end the sessions the old
// one could have opened — otherwise a stolen cookie survives the very password
// change made to get rid of it.
func deleteAdminTokensExcept(keep string) int {
	adminSessionsMu.Lock()
	defer adminSessionsMu.Unlock()
	removed := 0
	for token := range adminSessions {
		if token != keep {
			delete(adminSessions, token)
			removed++
		}
	}
	return removed
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

// setAdminCookie issues the session cookie.
//
// Secure is set from the actual connection rather than hardcoded: on an HTTPS
// deployment it keeps the session off any plaintext request (the redirect to
// HTTPS happens only after the browser has already sent one), while a plain
// HTTP install on a LAN still works instead of silently failing to log in.
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
	go func() {
		ticker := time.NewTicker(adminTokenReapInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			adminSessionsMu.Lock()
			for token, session := range adminSessions {
				if now.After(session.expiresAt) {
					delete(adminSessions, token)
				}
			}
			adminSessionsMu.Unlock()
		}
	}()
}
