package main

import (
	"net/http"
	"time"

	"github.com/Wirezat/fileshare/internal/ttl"
)

const shareTokenTTL = 24 * time.Hour

var shareTokens = ttl.New[string]()

func storeShareToken(token, subpath string) {
	shareTokens.Set(token, subpath, shareTokenTTL)
}

func validateShareToken(token, subpath string) bool {
	sp, ok := shareTokens.Get(token)
	return ok && sp == subpath
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
