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

func hasAdminCookie(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return false
	}
	return validateAdminToken(cookie.Value)
}

func setAdminCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   0,
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
