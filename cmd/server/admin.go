package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func handleAdminUI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "admin.html")
}

func handleAdminShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config, err := shared.LoadConfig()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config.Files)
	case http.MethodPost:
		var req struct {
			Subpath string `json:"subpath"`
			shared.FileData
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if req.Subpath == "" || req.Path == "" {
			http.Error(w, "subpath and path are required", http.StatusBadRequest)
			return
		}
		config, err := shared.LoadConfig()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if _, exists := config.Files[req.Subpath]; exists {
			http.Error(w, "Subpath already exists", http.StatusConflict)
			return
		}
		req.FileData.UploadTime = time.Now().Unix()
		config.Files[req.Subpath] = req.FileData
		if err := shared.SaveConfig(config); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		subpath := r.URL.Query().Get("subpath")
		if subpath == "" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		config, err := shared.LoadConfig()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		delete(config.Files, subpath)
		if err := shared.SaveConfig(config); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		config, err := shared.LoadConfig()
		if err != nil || username != "admin" || !shared.CheckPassword(password, config.AdminPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}
