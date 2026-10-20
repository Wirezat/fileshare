package web

import (
	"errors"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/auth"
)

// GET /setup — serves the setup page for initial admin password configuration.
func (s *Server) handleSetupUI(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Auth.NeedsSetup() {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	http.ServeFile(w, r, s.asset("html/setup.html"))
}

// POST /setup/api/init — set initial admin username and password.
// Only allowed if no password is set yet, otherwise 403 Forbidden.
func (s *Server) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Auth.NeedsSetup() {
		http.Error(w, "Setup already complete", http.StatusForbidden)
		return
	}
	var req struct {
		NewUsername string `json:"new_username"`
		NewPassword string `json:"new_password"`
		SetupToken  string `json:"setup_token"`
	}
	if !decodeOrErr(w, r, &req) {
		return
	}
	key := rateKey(r)
	err := s.deps.Auth.Setup(req.SetupToken, req.NewUsername, req.NewPassword, key)
	var limited auth.RateLimitedError
	switch {
	case err == nil:
		GoLog.Infof("initial credentials set via setup page (username: %s)", GoLog.Personal(s.deps.Store.Current().AdminUsername()))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrSetupDone):
		http.Error(w, "Setup already complete", http.StatusForbidden)
	case errors.As(err, &limited):
		tooManyAttempts(w, limited.RetryAfter, key, "setup")
	case errors.Is(err, auth.ErrWrongSetupCode):
		GoLog.Warnf("setup: rejected init with wrong setup code from %s", GoLog.IP(clientIP(r)))
		http.Error(w, "Wrong setup code — it is printed in the server log", http.StatusForbidden)
	case errors.Is(err, auth.ErrEmptyPassword):
		http.Error(w, "Password cannot be empty", http.StatusBadRequest)
	default:
		GoLog.Errorf("setup: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
