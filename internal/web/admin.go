package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/config"
)

// wantsLoginPage reports whether the request is a browser navigation rather than a fetch() call.
func wantsLoginPage(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// adminAuth gates every /admin route: navigations without a valid session are
// redirected to /setup or /admin/login, script requests get 401.
func (s *Server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		noPassword := s.deps.Auth.NeedsSetup()
		if noPassword || !s.hasAdminCookie(r) {
			if !wantsLoginPage(r) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			target := "/admin/login"
			if noPassword {
				target = "/setup"
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// handleAdminLogin serves the login page (GET) and validates credentials (POST).
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if s.deps.Auth.NeedsSetup() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		http.ServeFile(w, r, s.asset("html/admin-login.html"))

	case http.MethodPost:
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

		ip := clientIP(r)
		token, err := s.deps.Auth.Login(creds.Username, creds.Password, ip)
		var limited auth.RateLimitedError
		switch {
		case err == nil:
			setAdminCookie(w, r, token)
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, auth.ErrNotSetUp):
			http.Redirect(w, r, "/setup", http.StatusFound)
		case errors.As(err, &limited):
			tooManyAttempts(w, limited.RetryAfter, ip, "admin login")
		case errors.Is(err, auth.ErrWrongCredentials):
			GoLog.Warnf("handleAdminLogin: failed login attempt from %s", ip)
			http.Error(w, "Wrong username or password", http.StatusUnauthorized)
		default:
			GoLog.Errorf("handleAdminLogin: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleAdminLogout invalidates the session token and clears the cookie.
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	s.deps.Auth.Logout(currentAdminToken(r))
	clearAdminCookie(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// handleAdminSettingsMaxPostSize updates the maximum per-chunk POST size.
// PATCH /admin/api/settings/max_post_size
// Body: {"maxPostSize": 94371840}
func (s *Server) handleAdminSettingsMaxPostSize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxPostSize int `json:"maxPostSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MaxPostSize < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !s.updateOrErr(w, func(c *config.Config) error {
		c.MaxPostSize = body.MaxPostSize
		return nil
	}) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminSettingsChunkInactivityTimeout updates the inactivity timeout of incomplete chunked uploads.
// PATCH /admin/api/settings/chunk_inactivity_timeout
// Body: {"chunkInactivityTimeout": 1800}  (seconds)
func (s *Server) handleAdminSettingsChunkInactivityTimeout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seconds int `json:"chunkInactivityTimeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Seconds < 60 {
		http.Error(w, "Bad Request: minimum 60 seconds", http.StatusBadRequest)
		return
	}
	if !s.updateOrErr(w, func(c *config.Config) error {
		c.ChunkInactivityTimeout = body.Seconds
		return nil
	}) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
