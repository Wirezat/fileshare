package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// setupToken gates the one-shot setup endpoint; it is only ever printed to the
// server log.
var setupToken string

// initSetupToken mints the token when the instance has no admin password yet,
// and prints it where the operator will see it.
func initSetupToken(config *shared.Config) {
	if config.AdminPassword != "" {
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		GoLog.Errorf("setup: failed to generate setup token: %v", err)
		return
	}
	setupToken = hex.EncodeToString(b)
	GoLog.Infof("no admin password set — open /setup and enter this setup code: %s", setupToken)
}

// GET /setup — serves the setup page for initial admin password configuration.
func handleSetupUI(w http.ResponseWriter, r *http.Request) {
	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if config.AdminPassword != "" {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	http.ServeFile(w, r, setupHtmlPath)
}

// POST /setup/api/init — set initial admin username and password.
// Only allowed if no password is set yet, otherwise 403 Forbidden.
func handleSetupInit(w http.ResponseWriter, r *http.Request) {
	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if config.AdminPassword != "" {
		http.Error(w, "Setup already complete", http.StatusForbidden)
		return
	}
	if !loginLimiter.allow(w, "setup|"+clientIP(r), "setup") {
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
	if setupToken == "" || subtle.ConstantTimeCompare([]byte(req.SetupToken), []byte(setupToken)) != 1 {
		loginLimiter.recordFailure("setup|" + clientIP(r))
		GoLog.Warnf("setup: rejected init with wrong setup code from %s", clientIP(r))
		http.Error(w, "Wrong setup code — it is printed in the server log", http.StatusForbidden)
		return
	}
	if req.NewPassword == "" {
		http.Error(w, "Password cannot be empty", http.StatusBadRequest)
		return
	}
	// Username is optional — keep the existing default if not provided.
	if req.NewUsername != "" {
		config.AdminUsername = req.NewUsername
	}
	hashed, err := shared.HashPassword(req.NewPassword)
	if err != nil {
		GoLog.Errorf("setup: failed to hash password: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.AdminPassword = hashed
	if err := shared.SaveConfig(config); err != nil {
		GoLog.Errorf("setup: failed to save config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	setupToken = "" // one shot
	GoLog.Infof("initial credentials set via setup page (username: %s)", config.AdminUsername)
	w.WriteHeader(http.StatusNoContent)
}
