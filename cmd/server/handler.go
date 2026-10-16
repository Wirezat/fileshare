package main

import (
	"errors"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/media"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/share"
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

	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	res, err := shares.Resolve(subpath, strings.TrimPrefix(r.URL.Path, "/"+subpath), share.Access{
		Token:      shareCookie(r, subpath),
		AuthHeader: r.Header.Get("Authorization"),
		RequestURI: r.URL.RequestURI(),
	})
	if err != nil {
		resolveErr(w, r, subpath, res, err)
		return nil, false
	}
	diskPath := res.DiskPath

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
		config:   res.Config,
		fileData: res.Share,
		subpath:  subpath,
		diskPath: diskPath,
		fileInfo: fileInfo,
	}, true
}

func resolveErr(w http.ResponseWriter, r *http.Request, subpath string, res share.Resolved, err error) {
	switch {
	case errors.Is(err, share.ErrNotFound):
		serveNotFoundPage(w)
	case errors.Is(err, share.ErrOutside):
		GoLog.Warnf("path traversal attempt: %s", res.DiskPath)
		http.Error(w, "Forbidden", http.StatusForbidden)
	case errors.Is(err, share.ErrExpired):
		if media.IsPreviewBot(r.UserAgent()) {
			serveExpiredPreview(w, r, subpath)
		} else {
			serveExpiredPage(w, subpath)
		}
	case errors.Is(err, share.ErrLocked):
		serveGatePage(w, gateData{
			Subpath:    subpath,
			FormAction: "/" + subpath + "/unlock",
		})
	default:
		GoLog.Errorf("share lookup /%s: %v", subpath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleGet serves a file or directory.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
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

// resolveUploadTarget returns the share in the request URL if it accepts uploads, or writes the HTTP error.
func resolveUploadTarget(w http.ResponseWriter, r *http.Request) (config.FileData, bool) {
	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	fd, err := shares.UploadTarget(subpath)
	if err != nil {
		uploadErr(w, r, err)
		return config.FileData{}, false
	}
	return fd, true
}

func uploadErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, share.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, share.ErrUploadsOff):
		http.Error(w, "Forbidden", http.StatusForbidden)
	case errors.Is(err, share.ErrExpired):
		http.Error(w, "File share expired", http.StatusGone)
	default:
		GoLog.Errorf("upload target: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	token, err := shares.Unlock(subpath, r.FormValue("password"), clientIP(r))
	var limited share.RateLimitedError
	switch {
	case err == nil:
		setPasswordCookie(w, r, subpath, token)
		http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
	case errors.Is(err, share.ErrNotFound):
		serveNotFoundPage(w)
	case errors.Is(err, share.ErrNoPassword):
		http.Redirect(w, r, "/"+subpath, http.StatusSeeOther)
	case errors.As(err, &limited):
		tooManyAttempts(w, limited.RetryAfter, clientIP(r)+"|"+subpath, "share unlock")
	case errors.Is(err, share.ErrWrongPassword):
		GoLog.Warnf("failed unlock attempt for share /%s from %s", subpath, clientIP(r))
		serveGatePage(w, gateData{
			Subpath:          subpath,
			FormAction:       "/" + subpath + "/unlock",
			WrongCredentials: true,
		})
	default:
		GoLog.Errorf("handleUnlock: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
