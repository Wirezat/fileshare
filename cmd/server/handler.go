package main

import (
	"errors"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ttl"
)

// handleRequest is the main entry point for public share routes.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/log" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		handleLogEvent(w, r)
		return
	}

	if r.Method == http.MethodPost && r.URL.Query().Get("callback") == "1" {
		handleOfficeCallback(w, r)
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

	cfg := cfgStore.Current()

	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	relativePath := strings.TrimPrefix(r.URL.Path, "/"+subpath)

	fileData, exists := cfg.Share(subpath)
	if !exists {
		serveNotFoundPage(w)
		return nil, false
	}

	diskPath := filepath.Join(fileData.Path, relativePath)

	if diskPath != fileData.Path && !strings.HasPrefix(diskPath, fileData.Path+"/") {
		GoLog.Warnf("path traversal attempt: %s", diskPath)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil, false
	}

	if fileData.Expired {
		if isPreviewBot(r) {
			serveExpiredPreview(w, r, subpath)
		} else {
			serveExpiredPage(w, subpath)
		}
		return nil, false
	}

	if fileData.Password != "" && !hasPasswordCookie(r, subpath) && !office.TokenAllows(r.Header.Get("Authorization"), r.URL.RequestURI(), cfg.OfficeSecret()) {
		serveGatePage(w, gateData{
			Subpath:    subpath,
			FormAction: "/" + subpath + "/unlock",
		})
		return nil, false
	}

	fileInfo, err := os.Stat(diskPath)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			GoLog.Warnf("symlink loop detected: %s", diskPath)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return nil, false
		}
		GoLog.Errorf("failed to stat %s: %v", diskPath, err)
		serveNotFoundPage(w)
		return nil, false
	}

	return &requestContext{
		config:   cfg,
		fileData: fileData,
		subpath:  subpath,
		diskPath: diskPath,
		fileInfo: fileInfo,
	}, true
}

// handleGet serves a file or directory, enforcing expiration.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	fd := ctx.fileData

	if config.IsExpired(fd) {
		if isPreviewBot(r) {
			serveExpiredPreview(w, r, ctx.subpath)
		} else {
			serveExpiredPage(w, ctx.subpath)
		}
		return
	}

	switch {
	case ctx.fileInfo.IsDir():
		serveDirectory(w, r, ctx)
	case wantsPreviewImage(r, ctx):
		servePreviewImage(w, r, ctx)
	case previewWanted(r, ctx):
		servePreview(w, r, ctx)
	case officeWanted(r, ctx):
		serveOfficeViewer(w, r, ctx)
	case textWanted(r, ctx):
		serveTextView(w, r, ctx)
	default:
		serveShareFile(w, r, ctx.diskPath)
	}
}

// serveShareFile hands out the shared file inside a CSP sandbox.
func serveShareFile(w http.ResponseWriter, r *http.Request, diskPath string) {
	if office.Ext(diskPath) != "pdf" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	http.ServeFile(w, r, diskPath)
}

// resolveUploadTarget loads the FileData for the subpath in the request URL
// and verifies that uploads are permitted.
func resolveUploadTarget(w http.ResponseWriter, r *http.Request) (config.FileData, bool) {
	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	fd, exists := cfgStore.Current().Share(subpath)
	if !exists {
		http.NotFound(w, r)
		return config.FileData{}, false
	}
	if !fd.AllowPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return config.FileData{}, false
	}
	if config.IsExpired(fd) {
		http.Error(w, "File share expired", http.StatusGone)
		return config.FileData{}, false
	}
	return fd, true
}

var (
	gateTemplate     *template.Template
	gateTemplateErr  error
	gateTemplateOnce sync.Once
)

// gateData is the template context for the share password gate.
type gateData struct {
	Subpath          string
	FormAction       string
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

var (
	notFoundTemplate     *template.Template
	notFoundTemplateErr  error
	notFoundTemplateOnce sync.Once
)

func loadNotFoundTemplate() (*template.Template, error) {
	notFoundTemplateOnce.Do(func() {
		notFoundTemplate, notFoundTemplateErr = template.New("notfound").ParseFiles(notFoundHtmlPath)
	})
	return notFoundTemplate, notFoundTemplateErr
}

// serveNotFoundPage answers with the styled 404 page for a real browser
// navigation. API-style endpoints keep using http.NotFound.
func serveNotFoundPage(w http.ResponseWriter) {
	tmpl, err := loadNotFoundTemplate()
	if err != nil {
		GoLog.Errorf("failed to load notfound template: %v", err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := tmpl.ExecuteTemplate(w, "notfound", nil); err != nil {
		GoLog.Errorf("failed to render notfound template: %v", err)
	}
}

var (
	expiredTemplate     *template.Template
	expiredTemplateErr  error
	expiredTemplateOnce sync.Once
)

// expiredData is the template context for the expired-share page.
type expiredData struct {
	Subpath string
}

func loadExpiredTemplate() (*template.Template, error) {
	expiredTemplateOnce.Do(func() {
		expiredTemplate, expiredTemplateErr = template.New("expired").ParseFiles(expiredHtmlPath)
	})
	return expiredTemplate, expiredTemplateErr
}

// serveExpiredPage answers with the styled expired-share page for a real
// browser navigation. Preview bots get serveExpiredPreview instead.
func serveExpiredPage(w http.ResponseWriter, subpath string) {
	tmpl, err := loadExpiredTemplate()
	if err != nil {
		GoLog.Errorf("failed to load expired template: %v", err)
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	if err := tmpl.ExecuteTemplate(w, "expired", expiredData{Subpath: subpath}); err != nil {
		GoLog.Errorf("failed to render expired template: %v", err)
	}
}

// handleUnlock handles POST /{subpath}/unlock — verifies the share password,
// issues a token cookie on success, and redirects to the share.
func handleUnlock(w http.ResponseWriter, r *http.Request) {
	subpath := r.PathValue("subpath")

	fd, exists := cfgStore.Current().Share(subpath)
	if !exists {
		serveNotFoundPage(w)
		return
	}

	if fd.Password == "" {
		// Share has no password — redirect directly.
		http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
		return
	}

	limitKey := clientIP(r) + "|" + subpath
	if !allow(unlockLimiter, w, limitKey, "share unlock") {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	pw := r.FormValue("password")
	if !password.Check(pw, fd.Password) {
		unlockLimiter.Failure(limitKey)
		GoLog.Warnf("failed unlock attempt for share /%s from %s", subpath, clientIP(r))
		serveGatePage(w, gateData{
			Subpath:          subpath,
			FormAction:       "/" + subpath + "/unlock",
			WrongCredentials: true,
		})
		return
	}
	unlockLimiter.Success(limitKey)

	// Upgrade an outdated hash.
	if password.NeedsRehash(fd.Password) {
		if rehashed, err := password.Hash(pw); err == nil {
			err := cfgStore.Update(func(c *config.Config) error {
				if cur, ok := c.Files[subpath]; ok && cur.Password == fd.Password {
					cur.Password = rehashed
					c.Files[subpath] = cur
				}
				return nil
			})
			if err != nil {
				GoLog.Errorf("handleUnlock: failed to store upgraded hash: %v", err)
			} else {
				GoLog.Infof("share password hash upgraded to argon2id: /%s", subpath)
			}
		}
	}

	token, err := ttl.Token()
	if err != nil {
		GoLog.Errorf("handleUnlock: generate token: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	storeShareToken(token, subpath)
	setPasswordCookie(w, r, subpath, token)
	http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
}
