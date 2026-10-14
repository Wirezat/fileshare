package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/password"
	qrcode "github.com/skip2/go-qrcode"
)

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

// updateOrErr applies fn through the config store; on failure it writes the HTTP error and returns false.
func updateOrErr(w http.ResponseWriter, fn func(*config.Config) error) bool {
	err := cfgStore.Update(fn)
	if err == nil {
		return true
	}
	var he httpError
	if errors.As(err, &he) {
		http.Error(w, he.msg, he.code)
		return false
	}
	GoLog.Errorf("failed to update config: %v", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	return false
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

// The three admin pages are static shells rendered by their modules under /admin/static/js/.
func handleAdminUI(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, adminHtmlPath) }
func handleAdminLogsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, adminLogsHtmlPath)
}
func handleAdminSettingsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, adminSettingsHtmlPath)
}
func handleThemeCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, themeCssPath)
}

// handleAdminMe reports the logged-in admin for the wui user menu.
func handleAdminMe(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	jsonResponse(w, map[string]any{
		"username": cfgStore.Current().AdminUsername(),
		"is_admin": true,
	})
}

func handleAdminUptime(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	jsonResponse(w, map[string]any{
		"uptime_seconds": int(time.Since(startTime).Seconds()),
	})
}

func handleAdminShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		jsonResponse(w, maps.Collect(cfgStore.Current().Shares()))

	case http.MethodPost:
		var req struct {
			Subpath string `json:"subpath"`
			config.FileData
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		if req.Subpath == "" || req.Path == "" {
			http.Error(w, "subpath and path are required", http.StatusBadRequest)
			return
		}
		if !config.ValidOffice(req.Office) {
			http.Error(w, "office must be empty, view or edit", http.StatusBadRequest)
			return
		}
		if req.FileData.Password != "" {
			hashed, err := password.Hash(req.FileData.Password)
			if err != nil {
				GoLog.Errorf("failed to hash share password: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			req.FileData.Password = hashed
		}
		req.Path = filepath.Clean(req.Path)
		req.FileData.UploadTime = time.Now().Unix()
		if !updateOrErr(w, func(c *config.Config) error {
			if _, exists := c.Files[req.Subpath]; exists {
				return httpError{http.StatusConflict, "Subpath already exists"}
			}
			c.Files[req.Subpath] = req.FileData
			return nil
		}) {
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
			Expiration *int64  `json:"expiration"`
			AllowPost  *bool   `json:"allow_post"`
			NoZip      *bool   `json:"no_zip"`
			Office     *string `json:"office"`
			Expired    *bool   `json:"expired"`
			Password   *string `json:"password"`
		}

		if !decodeOrErr(w, r, &patch) {
			return
		}

		entry, exists := cfgStore.Current().Share(subpath)
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
			cleaned := filepath.Clean(*patch.Path)
			track("path", cleaned)
			entry.Path = cleaned
		}

		if patch.Expiration != nil {
			track("expiration", strconv.FormatInt(*patch.Expiration, 10))
			entry.Expiration = *patch.Expiration
		}

		if patch.AllowPost != nil {
			track("allow_post", strconv.FormatBool(*patch.AllowPost))
			entry.AllowPost = *patch.AllowPost
		}

		if patch.NoZip != nil {
			track("no_zip", strconv.FormatBool(*patch.NoZip))
			entry.NoZip = *patch.NoZip
		}

		if patch.Office != nil {
			if !config.ValidOffice(*patch.Office) {
				http.Error(w, "office must be empty, view or edit", http.StatusBadRequest)
				return
			}
			track("office", *patch.Office)
			entry.Office = *patch.Office
		}

		if patch.Expired != nil {
			track("expired", strconv.FormatBool(*patch.Expired))
			entry.Expired = *patch.Expired
		}

		if patch.Password != nil {
			if *patch.Password == "" {
				// Empty string = remove password protection.
				track("password", "<removed>")
				entry.Password = ""
			} else {
				hashed, err := password.Hash(*patch.Password)
				if err != nil {
					GoLog.Errorf("failed to hash share password: %v", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				track("password", "***")
				entry.Password = hashed
			}
		}

		if len(changes) == 0 {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		if !updateOrErr(w, func(c *config.Config) error {
			if _, exists := c.Files[subpath]; !exists {
				return httpError{http.StatusNotFound, "share not found"}
			}
			c.Files[subpath] = entry
			return nil
		}) {
			return
		}

		GoLog.Infof("%s updated: %s", subpath, strings.Join(changes, ", "))

	case http.MethodDelete:
		subpath, ok := subpathOrErr(w, r)
		if !ok {
			return
		}
		var entry config.FileData
		if !updateOrErr(w, func(c *config.Config) error {
			entry = c.Files[subpath]
			delete(c.Files, subpath)
			return nil
		}) {
			return
		}
		GoLog.Infof("share deleted: %s (was → %s)", subpath, entry.Path)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

const shareQRSize = 512

// handleAdminShareQR answers with a PNG QR code encoding the share's public URL.
func handleAdminShareQR(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	subpath, ok := subpathOrErr(w, r)
	if !ok {
		return
	}
	if _, exists := cfgStore.Current().Share(subpath); !exists {
		http.Error(w, "share not found", http.StatusNotFound)
		return
	}

	png, err := qrcode.Encode(absoluteURL(r, "/"+subpath), qrcode.Medium, shareQRSize)
	if err != nil {
		GoLog.Errorf("failed to generate QR code for %s: %v", subpath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && parsed > 0 {
		n = parsed
	}
	entries := logStore.Recent(n)
	if entries == nil {
		entries = []logstore.Entry{}
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

	ch := logStore.Subscribe()
	defer logStore.Unsubscribe(ch)

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

func handleAdminSettingsUsername(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewUsername     string `json:"new_username"`
	}
	if !decodeOrErr(w, r, &req) {
		return
	}
	if req.NewUsername == "" {
		http.Error(w, "Username cannot be empty", http.StatusBadRequest)
		return
	}
	if !password.Check(req.CurrentPassword, cfgStore.Current().AdminPassword()) {
		GoLog.Warnf("admin username change rejected: wrong current password")
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}
	if !updateOrErr(w, func(c *config.Config) error {
		c.AdminUsername = req.NewUsername
		return nil
	}) {
		return
	}
	dropped := deleteAdminTokensExcept(currentAdminToken(r))
	GoLog.Infof("admin username changed successfully (%d other session(s) ended)", dropped)
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
	if !password.Check(req.CurrentPassword, cfgStore.Current().AdminPassword()) {
		GoLog.Warnf("admin password change rejected: wrong current password")
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}
	hashed, err := password.Hash(req.NewPassword)
	if err != nil {
		GoLog.Errorf("failed to hash new password: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if !updateOrErr(w, func(c *config.Config) error {
		c.AdminPassword = hashed
		return nil
	}) {
		return
	}
	// Revoke every other session opened with the old password.
	dropped := deleteAdminTokensExcept(currentAdminToken(r))
	GoLog.Infof("admin password changed successfully (%d other session(s) ended)", dropped)
}

func handleAdminFunctionPruneExpired(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	pruned := 0
	if !updateOrErr(w, func(c *config.Config) error {
		for subpath, fd := range c.Files {
			if config.IsExpired(fd) {
				delete(c.Files, subpath)
				pruned++
			}
		}
		return nil
	}) {
		return
	}
	GoLog.Infof("pruned %d expired share(s)", pruned)
}
