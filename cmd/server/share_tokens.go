package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

var (
	shareTokensMu sync.RWMutex
	shareTokens   = map[string]string{} // token → subpath
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
	shareTokens[token] = subpath
	shareTokensMu.Unlock()
}

func validateShareToken(token, subpath string) bool {
	shareTokensMu.RLock()
	stored, ok := shareTokens[token]
	shareTokensMu.RUnlock()
	return ok && stored == subpath
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
