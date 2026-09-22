package main

import (
	"net/http"
	"time"
)

const (
	shareTokenTTL  = 24 * time.Hour
	shareTokenReap = 5 * time.Minute
)

var shareTokens = newTTLStore[string]() // value: subpath the token grants access to

func generateShareToken() (string, error) { return generateToken() }

func storeShareToken(token, subpath string) {
	shareTokens.set(token, subpath, shareTokenTTL)
}

// validateShareToken checks if the provided token is valid for the given subpath and not expired.
func validateShareToken(token, subpath string) bool {
	sp, ok := shareTokens.get(token)
	return ok && sp == subpath
}

func startTokenReaper() {
	shareTokens.startReaper(shareTokenReap)
}

func hasPasswordCookie(r *http.Request, subpath string) bool {
	cookie, err := r.Cookie("share_pw_" + subpath)
	if err != nil {
		return false
	}
	return validateShareToken(cookie.Value, subpath)
}

func setPasswordCookie(w http.ResponseWriter, r *http.Request, subpath, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "share_pw_" + subpath,
		Value:    token,
		Path:     "/" + subpath,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}
