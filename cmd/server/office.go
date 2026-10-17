package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/share"
)

// officeWanted reports whether this request should get the viewer page instead of the file.
func officeWanted(r *http.Request, ctx *requestContext) bool {
	return !ctx.fileInfo.IsDir() &&
		ctx.fileData.Office != config.OfficeOff &&
		ctx.config.OfficeReady() &&
		office.DocType(ctx.fileInfo.Name()) != "" &&
		r.URL.Query().Get("dl") == ""
}

func requestLang(r *http.Request) string {
	al := strings.ToLower(r.Header.Get("Accept-Language"))
	if strings.HasPrefix(al, "de") {
		return "de"
	}
	return "en"
}

type officePage struct {
	Title       string
	Subpath     string
	ParentURL   string
	DownloadURL string
	APIScript   string
	Config      template.JS
}

// serveOfficeViewer renders the page that embeds the document server editor.
func (s *server) serveOfficeViewer(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	tmpl := s.tpl.office

	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	downloadPath := r.URL.Path + "?dl=1"
	name := ctx.fileInfo.Name()

	cfg := office.EditorConfig(office.Doc{
		Name:     name,
		Key:      office.DocKey(ctx.diskPath, ctx.fileInfo),
		URL:      scheme + "://" + r.Host + downloadPath,
		Edit:     ctx.fileData.Office == config.OfficeEdit,
		Callback: scheme + "://" + r.Host + r.URL.Path + "?callback=1",
	}, requestLang(r), ctx.config.OfficeSecret())
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	parent := ""
	if ctx.diskPath != ctx.fileData.Path {
		parent = filepath.Dir(r.URL.Path)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "office", officePage{
		Title:       name,
		Subpath:     ctx.subpath,
		ParentURL:   parent,
		DownloadURL: downloadPath,
		APIScript:   ctx.config.OfficeURL() + "/web-apps/apps/api/documents/api.js",
		Config:      template.JS(cfgJSON),
	}); err != nil {
		GoLog.Errorf("failed to render office template: %v", err)
	}
}

func dsReply(w http.ResponseWriter, code int, dsErr int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":%d}`, dsErr)
}

// handleOfficeCallback replaces the file when the document server announces a saved version.
func (s *server) handleOfficeCallback(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Store.Current()
	subpath := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)[0]
	fd, exists := cfg.Share(subpath)
	if !exists {
		dsReply(w, http.StatusNotFound, 1)
		return
	}
	diskPath, inside := share.Join(fd.Path, strings.TrimPrefix(r.URL.Path, "/"+subpath))
	if !inside {
		dsReply(w, http.StatusForbidden, 1)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		dsReply(w, http.StatusUnauthorized, 1)
		return
	}
	cb, ok := office.ParseCallback(r.Header.Get("Authorization"), body, cfg.OfficeSecret())
	if !ok {
		dsReply(w, http.StatusUnauthorized, 1)
		return
	}
	if config.IsExpired(fd) {
		dsReply(w, http.StatusGone, 1)
		return
	}
	if fd.Office != config.OfficeEdit || office.DocType(diskPath) == "" {
		dsReply(w, http.StatusForbidden, 1)
		return
	}

	switch cb.Status {
	case 2, 6:
		if !strings.HasPrefix(cb.URL, cfg.OfficeURL()+"/") {
			GoLog.Warnf("office callback for /%s named a download outside the document server: %s", subpath, cb.URL)
			dsReply(w, http.StatusForbidden, 1)
			return
		}
		if err := office.ReplaceFromURL(diskPath, cb.URL, int64(cfg.MaxPostSize())); err != nil {
			GoLog.Errorf("office save failed for %s: %v", diskPath, err)
			dsReply(w, http.StatusInternalServerError, 1)
			return
		}
		GoLog.Infof("office saved %s", diskPath)
	case 3, 7:
		GoLog.Warnf("document server reported a save error for %s (status %d)", diskPath, cb.Status)
	}
	dsReply(w, http.StatusOK, 0)
}
