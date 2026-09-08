package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// wantsLoginPage reports whether this request is a browser navigating to a
// page — the only case in which sending it to the login page is an answer it
// can act on.
//
// A fetch() must get a status code instead, because it follows a redirect
// without ever telling its caller: a GET then hands the admin UI the login
// page where it expected JSON ("unexpected character at line 1 column 1"), and
// a PATCH or DELETE is replayed against /admin/login — the fetch spec rewrites
// the method only for POST — which answers 405 Method Not Allowed. Both read
// as bugs in the page rather than as "your session is gone".
func wantsLoginPage(r *http.Request) bool {
	// Sec-Fetch-Mode states exactly this distinction and is sent by every
	// current browser; the Accept sniff covers clients that omit it, where a
	// navigation asks for HTML and fetch() defaults to */*.
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// adminAuth gates every /admin route. A browser navigating to a page is sent
// to /setup if no password is set yet and to /admin/login if its session is
// gone; script requests get 401 and let the session layer decide what to do.
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
			// The login page passes a browser on to /setup itself when there
			// is no password yet, so script requests need no second answer.
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
		// Static: the page's own initAuth renders a failed attempt, so there is
		// nothing left for the server to template in.
		http.ServeFile(w, r, adminLoginHtmlPath)

	case http.MethodPost:
		ip := clientIP(r)
		if !loginLimiter.allow(w, ip, "admin login") {
			return
		}

		// wui's initAuth posts JSON and reads the outcome from the status code;
		// the form fallback keeps a plain POST working for anything that still
		// sends one.
		var creds struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		// Dispatch on the content type, not on a failed decode: Decode() reads
		// the body, so a form POST that fell through to FormValue would find
		// nothing left to parse.
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
			// One message for both a wrong name and a wrong password — saying
			// which was wrong would confirm that an account name exists.
			http.Error(w, "Wrong username or password", http.StatusUnauthorized)
			return
		}
		loginLimiter.recordSuccess(ip)

		// A successful login is the only moment the plaintext exists, so it is
		// the only moment an outdated hash can be replaced.
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
		// initAuth navigates on its own once this comes back ok.
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
