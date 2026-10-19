package web

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
	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/share"
	qrcode "github.com/skip2/go-qrcode"
)

// updateOrErr applies fn through the config store; on failure it writes the HTTP error and returns false.
func (s *Server) updateOrErr(w http.ResponseWriter, fn func(*config.Config) error) bool {
	err := s.deps.Store.Update(fn)
	if err == nil {
		return true
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

func (s *Server) handleAdminUI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("html/admin.html"))
}
func (s *Server) handleAdminLogsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("html/admin-logs.html"))
}
func (s *Server) handleAdminSettingsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("html/admin-settings.html"))
}
func (s *Server) handleThemeCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.asset("css/theme.css"))
}

// handleAdminMe reports the logged-in admin for the wui user menu.
func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	jsonResponse(w, map[string]any{
		"username":       s.deps.Store.Current().AdminUsername(),
		"is_admin":       true,
		"config_problem": s.deps.Store.Refused(),
	})
}

func (s *Server) handleAdminUptime(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	jsonResponse(w, map[string]any{
		"uptime_seconds": int(time.Since(s.deps.Started).Seconds()),
	})
}

func shareErr(w http.ResponseWriter, err error) {
	var invalid share.InvalidError
	switch {
	case errors.As(err, &invalid):
		http.Error(w, string(invalid), http.StatusBadRequest)
	case errors.Is(err, share.ErrNotFound):
		http.Error(w, "share not found", http.StatusNotFound)
	case errors.Is(err, share.ErrExists):
		http.Error(w, "Subpath already exists", http.StatusConflict)
	default:
		GoLog.Errorf("share update failed: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		jsonResponse(w, maps.Collect(s.deps.Store.Current().Shares()))

	case http.MethodPost:
		var req struct {
			Subpath string `json:"subpath"`
			config.FileData
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		if err := s.deps.Shares.Create(req.Subpath, req.FileData); err != nil {
			shareErr(w, err)
			return
		}
		GoLog.Infof("share created: %s → %s", req.Subpath, filepath.Clean(req.Path))
		w.WriteHeader(http.StatusCreated)

	case http.MethodPatch:
		subpath, ok := subpathOrErr(w, r)
		if !ok {
			return
		}
		var patch share.Patch
		if !decodeOrErr(w, r, &patch) {
			return
		}
		changes, err := s.deps.Shares.Patch(subpath, patch)
		if err != nil {
			shareErr(w, err)
			return
		}
		GoLog.Infof("%s updated: %s", subpath, strings.Join(changes, ", "))

	case http.MethodDelete:
		subpath, ok := subpathOrErr(w, r)
		if !ok {
			return
		}
		removed, err := s.deps.Shares.Delete(subpath)
		if err != nil {
			shareErr(w, err)
			return
		}
		GoLog.Infof("share deleted: %s (was → %s)", subpath, removed.Path)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

const shareQRSize = 512

// handleAdminShareQR answers with a PNG QR code encoding the share's public URL.
func (s *Server) handleAdminShareQR(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	subpath, ok := subpathOrErr(w, r)
	if !ok {
		return
	}
	if _, exists := s.deps.Store.Current().Share(subpath); !exists {
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

func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && parsed > 0 {
		n = parsed
	}
	entries := s.deps.Logs.Recent(n)
	if entries == nil {
		entries = []logstore.Entry{}
	}
	jsonResponse(w, entries)
}

func (s *Server) handleAdminLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.deps.Logs.Subscribe()
	defer s.deps.Logs.Unsubscribe(ch)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.streamsDone:
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

func credentialErr(w http.ResponseWriter, r *http.Request, err error) {
	var limited auth.RateLimitedError
	switch {
	case errors.As(err, &limited):
		tooManyAttempts(w, limited.RetryAfter, clientIP(r), "credential change")
	case errors.Is(err, auth.ErrEmptyUsername):
		http.Error(w, "Username cannot be empty", http.StatusBadRequest)
	case errors.Is(err, auth.ErrEmptyPassword):
		http.Error(w, "Password cannot be empty", http.StatusBadRequest)
	case errors.Is(err, auth.ErrWrongPassword):
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
	default:
		GoLog.Errorf("credential change failed: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminSettingsUsername(w http.ResponseWriter, r *http.Request) {
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
	dropped, err := s.deps.Auth.ChangeUsername(req.CurrentPassword, req.NewUsername, currentAdminToken(r))
	if errors.Is(err, auth.ErrWrongPassword) {
		GoLog.Warnf("admin username change rejected: wrong current password")
	}
	if err != nil {
		credentialErr(w, r, err)
		return
	}
	GoLog.Infof("admin username changed successfully (%d other session(s) ended)", dropped)
}

func (s *Server) handleAdminSettingsPassword(w http.ResponseWriter, r *http.Request) {
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
	dropped, err := s.deps.Auth.ChangePassword(req.CurrentPassword, req.NewPassword, currentAdminToken(r))
	if errors.Is(err, auth.ErrWrongPassword) {
		GoLog.Warnf("admin password change rejected: wrong current password")
	}
	if err != nil {
		credentialErr(w, r, err)
		return
	}
	GoLog.Infof("admin password changed successfully (%d other session(s) ended)", dropped)
}

func (s *Server) handleAdminFunctionPruneExpired(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	pruned, err := s.deps.Shares.Prune()
	if err != nil {
		shareErr(w, err)
		return
	}
	GoLog.Infof("pruned %d expired share(s)", pruned)
}
