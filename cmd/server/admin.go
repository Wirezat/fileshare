package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// wantsLoginPage reports whether the request is a browser navigation (as
// opposed to a fetch() call, which needs a status code rather than a redirect).
func wantsLoginPage(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// adminAuth gates every /admin route: navigations without a valid session are
// redirected to /setup or /admin/login, script requests get 401.
func adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, err := shared.LoadConfig()
		if err != nil {
			GoLog.Errorf("adminAuth: failed to load config: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if config.AdminPassword == "" || !hasAdminCookie(r) {
			if !wantsLoginPage(r) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			target := "/admin/login"
			if config.AdminPassword == "" {
				target = "/setup"
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// handleAdminLogin serves the login page (GET) and validates credentials (POST).
func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if config.AdminPassword == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		http.ServeFile(w, r, adminLoginHtmlPath)

	case http.MethodPost:
		ip := clientIP(r)
		if !loginLimiter.allow(w, ip, "admin login") {
			return
		}

		// Credentials arrive as JSON (wui initAuth) or as a plain form POST.
		var creds struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
		} else {
			creds.Username = r.FormValue("username")
			creds.Password = r.FormValue("password")
		}

		usernameOK := config.AdminUsername == "" || creds.Username == config.AdminUsername
		passwordOK := shared.CheckPassword(creds.Password, config.AdminPassword)

		if !usernameOK || !passwordOK {
			loginLimiter.recordFailure(ip)
			GoLog.Warnf("handleAdminLogin: failed login attempt from %s", ip)
			http.Error(w, "Wrong username or password", http.StatusUnauthorized)
			return
		}
		loginLimiter.recordSuccess(ip)

		// Upgrade an outdated hash while the plaintext is available.
		if shared.NeedsRehash(config.AdminPassword) {
			if rehashed, err := shared.HashPassword(creds.Password); err == nil {
				config.AdminPassword = rehashed
				if err := shared.SaveConfig(config); err != nil {
					GoLog.Errorf("handleAdminLogin: failed to store upgraded hash: %v", err)
				} else {
					GoLog.Infof("admin password hash upgraded to argon2id")
				}
			}
		}

		token, err := generateAdminToken()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		storeAdminToken(token)
		setAdminCookie(w, r, token)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleAdminLogout invalidates the session token and clears the cookie.
func handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		deleteAdminToken(cookie.Value)
	}
	clearAdminCookie(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// handleAdminSettingsMaxPostSize updates the maximum per-chunk POST size.
// PATCH /admin/api/settings/max_post_size
// Body: {"maxPostSize": 94371840}
func handleAdminSettingsMaxPostSize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxPostSize int `json:"maxPostSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MaxPostSize < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.MaxPostSize = body.MaxPostSize
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminSettingsChunkInactivityTimeout updates the inactivity timeout
// for incomplete chunked upload sessions.
// PATCH /admin/api/settings/chunk_inactivity_timeout
// Body: {"chunkInactivityTimeout": 1800}  (seconds)
func handleAdminSettingsChunkInactivityTimeout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seconds int `json:"chunkInactivityTimeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Seconds < 60 {
		http.Error(w, "Bad Request: minimum 60 seconds", http.StatusBadRequest)
		return
	}
	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.ChunkInactivityTimeout = body.Seconds
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Apply immediately — no restart required.
	storage.SetInactivityTimeout(time.Duration(body.Seconds) * time.Second)
	w.WriteHeader(http.StatusNoContent)
}
