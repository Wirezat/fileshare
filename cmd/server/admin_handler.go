package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

func configOrErr(w http.ResponseWriter) (*shared.Config, bool) {
	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}
	return config, true
}

func saveOrErr(w http.ResponseWriter, config *shared.Config) bool {
	if err := shared.SaveConfig(config); err != nil {
		GoLog.Errorf("failed to save config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return false
	}
	return true
}

func decodeOrErr(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return false
	}
	return true
}

func subpathOrErr(w http.ResponseWriter, r *http.Request) (string, bool) {
	sp := r.URL.Query().Get("subpath")
	if sp == "" {
		http.Error(w, "subpath query param required", http.StatusBadRequest)
		return "", false
	}
	return sp, true
}

func methodOnly(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func handleAdminUI(w http.ResponseWriter, r *http.Request)  { http.ServeFile(w, r, adminHtmlPath) }
func handleAdminCSS(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, adminCssPath) }
func handleAdminJS(w http.ResponseWriter, r *http.Request)  { http.ServeFile(w, r, adminJsPath) }

func handleAdminShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		config, ok := configOrErr(w)
		if !ok {
			return
		}
		jsonResponse(w, config.Files)

	case http.MethodPost:
		var req struct {
			Subpath string `json:"subpath"`
			shared.FileData
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		if req.Subpath == "" || req.Path == "" {
			http.Error(w, "subpath and path are required", http.StatusBadRequest)
			return
		}
		config, ok := configOrErr(w)
		if !ok {
			return
		}
		if _, exists := config.Files[req.Subpath]; exists {
			http.Error(w, "Subpath already exists", http.StatusConflict)
			return
		}
		req.FileData.UploadTime = time.Now().Unix()
		config.Files[req.Subpath] = req.FileData
		if !saveOrErr(w, config) {
			return
		}
		GoLog.Infof("share created: %s → %s", req.Subpath, req.Path)
		w.WriteHeader(http.StatusCreated)

	case http.MethodPatch:
		subpath, ok := subpathOrErr(w, r)
		if !ok {
			return
		}

		var patch struct {
			Path       *string `json:"path"`
			Uses       *int    `json:"uses"`
			Expiration *int64  `json:"expiration"`
			AllowPost  *bool   `json:"allow_post"`
			Expired    *bool   `json:"expired"`
			Password   *string `json:"password"`
		}

		if !decodeOrErr(w, r, &patch) {
			return
		}

		config, ok := configOrErr(w)
		if !ok {
			return
		}

		entry, exists := config.Files[subpath]
		if !exists {
			http.Error(w, "share not found", http.StatusNotFound)
			return
		}

		var changes []string
		track := func(k, v string) {
			changes = append(changes, k+" -> "+v)
		}

		if patch.Path != nil {
			if *patch.Path == "" {
				http.Error(w, "path cannot be empty", http.StatusBadRequest)
				return
			}
			track("path", *patch.Path)
			entry.Path = *patch.Path
		}

		if patch.Uses != nil {
			track("uses", strconv.Itoa(*patch.Uses))
			entry.Uses = *patch.Uses
		}

		if patch.Expiration != nil {
			track("expiration", strconv.FormatInt(*patch.Expiration, 10))
			entry.Expiration = *patch.Expiration
		}

		if patch.AllowPost != nil {
			track("allow_post", strconv.FormatBool(*patch.AllowPost))
			entry.AllowPost = *patch.AllowPost
		}

		if patch.Expired != nil {
			track("expired", strconv.FormatBool(*patch.Expired))
			entry.Expired = *patch.Expired
		}

		if patch.Password != nil {
			track("password", "***")
			entry.Password = *patch.Password
		}

		if len(changes) == 0 {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		config.Files[subpath] = entry

		if !saveOrErr(w, config) {
			return
		}

		GoLog.Infof("%s updated: %s", subpath, strings.Join(changes, ", "))

	case http.MethodDelete:
		subpath, ok := subpathOrErr(w, r)
		if !ok {
			return
		}
		config, ok := configOrErr(w)
		if !ok {
			return
		}
		entry := config.Files[subpath]
		delete(config.Files, subpath)
		if !saveOrErr(w, config) {
			return
		}
		GoLog.Infof("share deleted: %s (was → %s)", subpath, entry.Path)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && parsed > 0 {
		n = parsed
	}
	entries := shared.Logger.Recent(n)
	if entries == nil {
		entries = []shared.LogEntry{}
	}
	jsonResponse(w, entries)
}

func handleAdminLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := shared.Logger.Subscribe()
	defer shared.Logger.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				GoLog.Errorf("failed to marshal log entry for SSE: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func handleAdminSettingsPassword(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeOrErr(w, r, &req) {
		return
	}
	if req.NewPassword == "" {
		http.Error(w, "Password cannot be empty", http.StatusBadRequest)
		return
	}
	config, ok := configOrErr(w)
	if !ok {
		return
	}
	if !shared.CheckPassword(req.CurrentPassword, config.AdminPassword) {
		GoLog.Warnf("admin password change rejected: wrong current password")
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}
	hashed, err := shared.HashPassword(req.NewPassword)
	if err != nil {
		GoLog.Errorf("failed to hash new password: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.AdminPassword = hashed
	if !saveOrErr(w, config) {
		return
	}
	GoLog.Infof("admin password changed successfully")
}

func handleAdminFunctionPruneExpired(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	config, ok := configOrErr(w)
	if !ok {
		return
	}
	pruned := 0
	for subpath, fd := range config.Files {
		if shared.IsExpired(fd) {
			delete(config.Files, subpath)
			pruned++
		}
	}
	if !saveOrErr(w, config) {
		return
	}
	GoLog.Infof("pruned %d expired share(s)", pruned)
}
