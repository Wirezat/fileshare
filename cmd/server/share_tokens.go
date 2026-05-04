package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	shareTokenTTL  = 24 * time.Hour
	shareTokenReap = 5 * time.Minute
)

type tokenEntry struct {
	subpath   string
	expiresAt time.Time
}

var (
	shareTokensMu sync.RWMutex
	shareTokens   = map[string]tokenEntry{}
)

func generateShareToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func storeShareToken(token, subpath string) {
	shareTokensMu.Lock()
	shareTokens[token] = tokenEntry{
		subpath:   subpath,
		expiresAt: time.Now().Add(shareTokenTTL),
	}
	shareTokensMu.Unlock()
}

// validateShareToken checks if the provided token is valid for the given subpath and not expired.
func validateShareToken(token, subpath string) bool {
	shareTokensMu.RLock()
	entry, ok := shareTokens[token]
	shareTokensMu.RUnlock()
	return ok && entry.subpath == subpath && time.Now().Before(entry.expiresAt)
}

func startTokenReaper() {
	go func() {
		ticker := time.NewTicker(shareTokenReap)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			shareTokensMu.Lock()
			for token, entry := range shareTokens {
				if now.After(entry.expiresAt) {
					delete(shareTokens, token)
				}
			}
			shareTokensMu.Unlock()
		}
	}()
}

func hasPasswordCookie(r *http.Request, subpath string) bool {
	cookie, err := r.Cookie("share_pw_" + subpath)
	if err != nil {
		return false
	}
	return validateShareToken(cookie.Value, subpath)
}

func setPasswordCookie(w http.ResponseWriter, subpath, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "share_pw_" + subpath,
		Value:    token,
		Path:     "/" + subpath,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
