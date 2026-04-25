package main

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

var (
	dirTemplate     *template.Template
	dirTemplateErr  error
	dirTemplateOnce sync.Once
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

// handleChunkInit registers or resumes a chunked upload session.
// POST /{subpath}/chunk-init
// Form: uploadId (client-generated hash), filename, totalChunks
// Response: 200 + {"uploadId":"...", "missingChunks":[0,1,...]}
func handleChunkInit(w http.ResponseWriter, r *http.Request) {
	fd, ok := resolveUploadTarget(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	uploadID := r.FormValue("uploadId")
	if uploadID == "" {
		http.Error(w, "Bad Request: missing uploadId", http.StatusBadRequest)
		return
	}
	filename := r.FormValue("filename")
	if filename == "" {
		http.Error(w, "Bad Request: missing filename", http.StatusBadRequest)
		return
	}
	totalChunks, err := strconv.Atoi(r.FormValue("totalChunks"))
	if err != nil || totalChunks < 1 {
		http.Error(w, "Bad Request: invalid totalChunks", http.StatusBadRequest)
		return
	}

	missing, err := storage.InitChunk(uploadID, filename, totalChunks, fd.Path)
	if err != nil {
		GoLog.Errorf("handleChunkInit: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"uploadId": uploadID, "missingChunks": missing})
}

// handleChunkReceive stores a single chunk. Returns 202 while more chunks
// are expected; 204 when the file has been fully assembled.
// POST /{subpath}/chunk
// Multipart: uploadId, chunkIndex, chunk (file)
func handleChunkReceive(w http.ResponseWriter, r *http.Request) {
	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("handleChunkReceive: load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := r.ParseMultipartForm(int64(config.MaxPostSize)); err != nil {
		GoLog.Errorf("handleChunkReceive: parse multipart: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	uploadID := r.FormValue("uploadId")
	chunkIndex, err := strconv.Atoi(r.FormValue("chunkIndex"))
	if err != nil || chunkIndex < 0 {
		http.Error(w, "Bad Request: invalid chunkIndex", http.StatusBadRequest)
		return
	}

	f, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, "Bad Request: missing chunk", http.StatusBadRequest)
		return
	}
	defer f.Close()

	done, err := storage.ReceiveChunk(uploadID, chunkIndex, f)
	if err != nil {
		GoLog.Errorf("handleChunkReceive: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if done {
		w.WriteHeader(http.StatusNoContent) // 204 — upload complete
	} else {
		w.WriteHeader(http.StatusAccepted) // 202 — more chunks expected
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

// handleAdminSettingsMaxPostSize updates the maximum per-chunk POST size.
// PATCH /admin/api/settings/max_post_size
// Body: {"maxPostSize": 94371840}
func handleAdminSettingsMaxPostSize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxPostSize int `json:"maxPostSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MaxPostSize < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.MaxPostSize = body.MaxPostSize
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminSettingsChunkInactivityTimeout updates the inactivity timeout
// for incomplete chunked upload sessions.
// PATCH /admin/api/settings/chunk_inactivity_timeout
// Body: {"chunkInactivityTimeout": 1800}  (seconds)
func handleAdminSettingsChunkInactivityTimeout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seconds int `json:"chunkInactivityTimeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Seconds < 60 {
		http.Error(w, "Bad Request: minimum 60 seconds", http.StatusBadRequest)
		return
	}

	config, err := shared.LoadConfig()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	config.ChunkInactivityTimeout = body.Seconds
	if err := shared.SaveConfig(config); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Apply immediately — no restart required.
	storage.SetInactivityTimeout(time.Duration(body.Seconds) * time.Second)
	w.WriteHeader(http.StatusNoContent)
}

// handleShareCSS and handleShareJS serve static assets.
func handleShareCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareCssPath)
}

func handleShareJS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareJsPath)
}

// handleLogEvent allows the client to send log messages to the server.
func handleLogEvent(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Message string `json:"message"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	GoLog.Infof("%s: Client: %s", clientIP(r), preventClientLogInjection(payload.Message))
	w.WriteHeader(http.StatusNoContent)
}

// serveDirectory renders the directory listing, or streams a ZIP if ?download=zip.
func serveDirectory(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, ctx.diskPath)
		return
	}

	tmpl, err := loadTemplate()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	fd := ctx.fileData
	relPath := filepath.Join("/", strings.TrimPrefix(ctx.diskPath, fd.Path))

	parentDir := "/"
	if ctx.diskPath != fd.Path {
		parentDir = filepath.Join("/", strings.TrimPrefix(filepath.Dir(ctx.diskPath), fd.Path))
	}

	files, _ := getFileInfos(ctx.diskPath, fd.Path)

	if err := tmpl.Execute(w, PageData{
		Subpath:      ctx.subpath,
		UploadTime:   fd.UploadTime,
		DirPath:      relPath,
		Files:        files,
		ParentDir:    parentDir,
		HasParentDir: ctx.diskPath != fd.Path,
		Uses:         fd.Uses,
		Expiration:   fd.Expiration,
		AllowPost:    fd.AllowPost,
	}); err != nil {
		GoLog.Errorf("failed to render directory template: %v", err)
	}
}

// getFileInfos returns FileInfo entries for a directory, skipping hidden files.
func getFileInfos(dirPath, basePath string) ([]shared.FileInfo, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	infos := make([]shared.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		infos = append(infos, shared.FileInfo{
			Name:  entry.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), entry.Name()),
			IsDir: entry.IsDir(),
		})
	}
	return infos, nil
}

// loadTemplate parses the directory template once and reuses it for all listings.
func loadTemplate() (*template.Template, error) {
	dirTemplateOnce.Do(func() {
		dirTemplate, dirTemplateErr = template.New("directory").
			Funcs(template.FuncMap{
				"getFileExtension": func(name string) string {
					return strings.ToLower(filepath.Ext(name))
				},
			}).
			ParseFiles(shareHtmlPath)
		if dirTemplateErr != nil {
			GoLog.Errorf("failed to parse directory template: %v", dirTemplateErr)
		}
	})
	return dirTemplate, dirTemplateErr
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
