package main

import (
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

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

func handleSetupCSS(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, setupCssPath) }
func handleSetupJS(w http.ResponseWriter, r *http.Request)  { http.ServeFile(w, r, setupJsPath) }

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
	var req struct {
		NewUsername string `json:"new_username"`
		NewPassword string `json:"new_password"`
	}
	if !decodeOrErr(w, r, &req) {
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
	GoLog.Infof("initial credentials set via setup page (username: %s)", config.AdminUsername)
	w.WriteHeader(http.StatusNoContent)
}
