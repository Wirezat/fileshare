package main

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// handleRequest is the main entry point for public share routes.
// Logging middleware is applied upstream.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/log" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		handleLogEvent(w, r)
		return
	}

	ctx, ok := prepareRequest(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handleGet(w, r, ctx)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// prepareRequest validates the request and resolves all data needed to serve it.
// Writes an appropriate HTTP error and returns false if anything is invalid.
func prepareRequest(w http.ResponseWriter, r *http.Request) (*requestContext, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return nil, false
	}

	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}

	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	relativePath := strings.TrimPrefix(r.URL.Path, "/"+subpath)

	fileData, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return nil, false
	}

	diskPath := filepath.Join(fileData.Path, relativePath)

	if diskPath != fileData.Path && !strings.HasPrefix(diskPath, fileData.Path+"/") {
		GoLog.Warnf("path traversal attempt: %s", diskPath)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil, false
	}

	if fileData.Expired {
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return nil, false
	}

	// Password gate — checked after expiry so expired shares still 410 first.
	if fileData.Password != "" && !hasPasswordCookie(r, subpath) {
		serveGatePage(w, gateData{
			Subpath:    subpath,
			FormAction: "/" + subpath + "/unlock",
		})
		return nil, false
	}

	fileInfo, err := os.Stat(diskPath)
	if err != nil {
		GoLog.Errorf("failed to stat %s: %v", diskPath, err)
		http.NotFound(w, r)
		return nil, false
	}

	return &requestContext{
		config:   config,
		fileData: fileData,
		subpath:  subpath,
		diskPath: diskPath,
		fileInfo: fileInfo,
	}, true
}

// handleGet serves a file or directory, enforcing expiration and use limits.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	fd := ctx.fileData

	if shared.IsExpired(fd) {
		fd.Expired = true
		ctx.config.Files[ctx.subpath] = fd
		if err := shared.SaveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config after expiry: %v", err)
		}
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	isFileDownload := !ctx.fileInfo.IsDir()
	isShareRoot := ctx.diskPath == ctx.fileData.Path
	shouldCount := isShareRoot && (isFileDownload || !hasSessionCookie(r, ctx.subpath))

	if shouldCount && fd.Uses > 0 {
		fd.Uses--
		if fd.Uses == 0 {
			fd.Expired = true
		}
		ctx.config.Files[ctx.subpath] = fd
		if err := shared.SaveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config: %v", err)
			return
		}
		if !isFileDownload {
			setSessionCookie(w, ctx.subpath)
		}
	}

	if ctx.fileInfo.IsDir() {
		serveDirectory(w, r, ctx)
	} else {
		http.ServeFile(w, r, ctx.diskPath)
	}
}

// resolveUploadTarget loads the FileData for the subpath in the request URL
// and verifies that uploads are permitted.
func resolveUploadTarget(w http.ResponseWriter, r *http.Request) (shared.FileData, bool) {
	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("resolveUploadTarget: load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return shared.FileData{}, false
	}

	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	fd, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return shared.FileData{}, false
	}
	if !fd.AllowPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return shared.FileData{}, false
	}
	if shared.IsExpired(fd) {
		http.Error(w, "File share expired", http.StatusGone)
		return shared.FileData{}, false
	}
	return fd, true
}

// hasSessionCookie returns true if the browser already has a session cookie for this share.
func hasSessionCookie(r *http.Request, subpath string) bool {
	_, err := r.Cookie("session_" + subpath)
	return err == nil
}

// setSessionCookie sets a session-scoped cookie for this share.
func setSessionCookie(w http.ResponseWriter, subpath string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_" + subpath,
		Value:    "1",
		Path:     "/" + subpath,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

var (
	gateTemplate     *template.Template
	gateTemplateErr  error
	gateTemplateOnce sync.Once
)

// gateData is the template context for both the share gate and the admin login page.
type gateData struct {
	Subpath          string
	FormAction       string
	ShowUsername     bool
	WrongCredentials bool
}

func loadGateTemplate() (*template.Template, error) {
	gateTemplateOnce.Do(func() {
		gateTemplate, gateTemplateErr = template.New("gate").ParseFiles(gateHtmlPath)
	})
	return gateTemplate, gateTemplateErr
}

func serveGatePage(w http.ResponseWriter, data gateData) {
	tmpl, err := loadGateTemplate()
	if err != nil {
		GoLog.Errorf("failed to load gate template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "gate", data); err != nil {
		GoLog.Errorf("failed to render gate template: %v", err)
	}
}

// handleUnlock handles POST /{subpath}/unlock — verifies the share password,
// issues a token cookie on success, and redirects to the share.
// Not wrapped in loggingMiddleware intentionally — form body contains the password.
func handleUnlock(w http.ResponseWriter, r *http.Request) {
	subpath := r.PathValue("subpath")

	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("handleUnlock: load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	fd, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return
	}

	if fd.Password == "" {
		// Share has no password — redirect directly.
		http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if !shared.CheckPassword(r.FormValue("password"), fd.Password) {
		GoLog.Warnf("failed unlock attempt for share /%s", subpath)
		serveGatePage(w, gateData{
			Subpath:          subpath,
			FormAction:       "/" + subpath + "/unlock",
			WrongCredentials: true,
		})
		return
	}

	token, err := generateShareToken()
	if err != nil {
		GoLog.Errorf("handleUnlock: generate token: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	storeShareToken(token, subpath)
	setPasswordCookie(w, subpath, token)
	http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
}
