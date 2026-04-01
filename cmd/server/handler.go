package main

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// #region request preparation

// prepareRequest validates and resolves all data needed to serve a request.
// It writes the appropriate HTTP error to w and returns false if anything is invalid.
// On success it returns a populated requestContext and true.
func prepareRequest(w http.ResponseWriter, r *http.Request) (*requestContext, bool) {
	// Methode prüfen
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, POST, HEAD")
		GoLog.Errorf("unsupported method: %s", r.Method)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return nil, false
	}

	// Config laden
	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}

	// URL aufteilen
	subpath := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
	relativePath := strings.TrimPrefix(r.URL.Path, "/"+subpath)

	// Share aus Config holen
	fileData, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return nil, false
	}

	// Disk-Pfad berechnen
	diskPath := filepath.Join(fileData.Path, relativePath)

	// Security-Check: Path Traversal verhindern
	if !strings.HasPrefix(diskPath, fileData.Path+"/") && diskPath != fileData.Path {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil, false
	}

	// Expired prüfen
	if fileData.Expired {
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return nil, false
	}

	// Datei/Ordner prüfen
	fileInfo, err := os.Stat(diskPath)
	if err != nil {
		GoLog.Errorf("error accessing file: %v", err)
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

// #endregion

// #region handlers

// handleGet serves a file or directory for GET and HEAD requests.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	fd := ctx.fileData

	// Expired oder Uses aufgebraucht
	if fd.Uses == 0 || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix()) {
		fd.Expired = true
		ctx.config.Files[ctx.subpath] = fd
		if err := shared.SaveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config: %v", err)
		}
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	// Uses dekrementieren wenn nicht unendlich
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
		serveDirectory(w, ctx.diskPath, ctx.subpath, fd.Path, fd.UploadTime, fd.Expiration, fd.Uses, fd.AllowPost, r)
	} else {
		http.ServeFile(w, r, ctx.diskPath)
	}
}

// handlePost processes file uploads for POST requests.
func handlePost(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if !ctx.fileData.AllowPost {
		http.Error(w, "POST Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 100 GB limit - intentionally high for private use
	err := r.ParseMultipartForm(100 << 30)
	if err != nil {
		GoLog.Errorf("could not parse multipart form: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	for _, fileHeader := range r.MultipartForm.File["files"] {
		if err := saveUploadedFile(fileHeader, ctx.diskPath); err != nil {
			GoLog.Errorf("error saving uploaded file: %v", err)
			http.Error(w, "Upload failed", http.StatusInternalServerError)
			return
		}
	}

	w.Write([]byte("Files uploaded successfully"))
}

// handleRequest is the main HTTP handler. It validates the request via prepareRequest
// and dispatches to the appropriate handler based on the HTTP method.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	requestJson, _ := requestToJSON(r)
	GoLog.Infof(requestJson)

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

// #endregion

// #region directory serving
func handleShareCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareCssPath)
}

func handleShareJS(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, shareJsPath)
}

// serveDirectory renders the directory listing HTML or streams a ZIP archive.
func serveDirectory(w http.ResponseWriter, dirPath, subpath, basePath string, uploadTime int64, expiration int64, uses int, allowPost bool, r *http.Request) {
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string {
			return strings.ToLower(filepath.Ext(filename))
		},
	}).ParseFiles(shareHtmlPath)
	if err != nil {
		GoLog.Errorf("error parsing template: %v", err)
		return
	}

	if err := t.Execute(w, PageData{
		Subpath:    subpath,
		UploadTime: uploadTime,
		DirPath:    filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:      func() []shared.FileInfo { infos, _ := getFileInfos(dirPath, basePath); return infos }(),
		ParentDir: func() string {
			if dirPath == basePath {
				return "/"
			}
			return filepath.Join("/", strings.TrimPrefix(filepath.Dir(dirPath), basePath))
		}(),
		HasParentDir: dirPath != basePath,
		Uses:         uses,
		Expiration:   expiration,
		AllowPost:    allowPost,
	}); err != nil {
		GoLog.Errorf("error rendering template: %v", err)
	}
}

// getFileInfos reads a directory and returns a list of FileInfo structs.
// Hidden files (starting with ".") are skipped.
func getFileInfos(dirPath, basePath string) ([]shared.FileInfo, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var fileInfos []shared.FileInfo
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".") {
			continue
		}
		fileInfos = append(fileInfos, shared.FileInfo{
			Name:  file.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), file.Name()),
			IsDir: file.IsDir(),
		})
	}
	return fileInfos, nil
}

// #endregion
