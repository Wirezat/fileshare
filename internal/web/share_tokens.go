package web

import "net/http"

func shareCookie(r *http.Request, subpath string) string {
	if cookie, err := r.Cookie("share_pw_" + subpath); err == nil {
		return cookie.Value
	}
	return ""
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
