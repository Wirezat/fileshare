package main

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

const maxUploadSize = 100 << 30 // 100 GB – intentionally high for private use

// dirTemplate is parsed once on first use and reused for all directory listings.
var (
	dirTemplate     *template.Template
	dirTemplateErr  error
	dirTemplateOnce sync.Once
)

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

// prepareRequest validates the request and resolves all data needed to serve it.
// Writes an appropriate HTTP error and returns false if anything is invalid.
func prepareRequest(w http.ResponseWriter, r *http.Request) (*requestContext, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, POST, HEAD")
		GoLog.Warnf("unsupported method: %s", r.Method)
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

	// Reject path traversal attempts.
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

// handleRequest is the main entry point. Logs the request, then delegates to
// prepareRequest and the appropriate method handler.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	if data, err := requestToJSON(r); err == nil {
		GoLog.Info(string(data))
	} else {
		GoLog.Warnf("failed to serialize request: %v", err)
	}

	ctx, ok := prepareRequest(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handleGet(w, r, ctx)
	case http.MethodPost:
		handlePost(w, r, ctx)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleGet serves a file or directory, enforcing expiration and use limits.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	fd := ctx.fileData

	if fd.Uses == 0 || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix()) {
		fd.Expired = true
		ctx.config.Files[ctx.subpath] = fd
		if err := shared.SaveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config after expiry: %v", err)
		}
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	// Decrement uses; 0 means expired, negative means unlimited.
	if fd.Uses > 0 {
		fd.Uses--
		if fd.Uses == 0 {
			fd.Expired = true
		}
		ctx.config.Files[ctx.subpath] = fd
	}
	if err := shared.SaveConfig(ctx.config); err != nil {
		GoLog.Errorf("failed to save config: %v", err)
		return
	}

	if ctx.fileInfo.IsDir() {
		serveDirectory(w, r, ctx)
	} else {
		http.ServeFile(w, r, ctx.diskPath)
	}
}

// handlePost processes file uploads.
func handlePost(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if !ctx.fileData.AllowPost {
		http.Error(w, "POST Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		GoLog.Errorf("failed to parse multipart form: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	for _, fh := range r.MultipartForm.File["files"] {
		if err := saveUploadedFile(fh, ctx.diskPath); err != nil {
			GoLog.Errorf("failed to save uploaded file %q: %v", fh.Filename, err)
			http.Error(w, "Upload failed", http.StatusInternalServerError)
			return
		}
	}

	w.Write([]byte("Files uploaded successfully"))
}

func handleShareCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareCssPath)
}

func handleShareJS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareJsPath)
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
