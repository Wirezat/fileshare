package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

// #region constants

const (
	configFilePath   = "./data.json"
	templateFilePath = "template.html"
)

// #endregion

// #region structs

// FileInfo contains the name, path and type of a file or directory
type FileInfo struct {
	Name  string
	Path  string
	IsDir bool
}

// PageData contains all data needed to render the directory view template
type PageData struct {
	Subpath      string
	UploadTime   int64
	DirPath      string
	Files        []FileInfo
	ParentDir    string
	HasParentDir bool
	Uses         int
	Expiration   int64
	AllowPost    bool
}

// FileData contains the sharing configuration for a single share
type FileData struct {
	Path       string
	UploadTime int64
	Uses       int
	Expiration int64
	Expired    bool
	AllowPost  bool
	Password   string
}

// JsonData is the top-level configuration structure
type JsonData struct {
	Port          int                 `json:"port"`
	AdminPassword string              `json:"admin_password"`
	AdminSalt     string              `json:"admin_salt"`
	Files         map[string]FileData `json:"files"`
}

// requestContext holds all resolved data for an incoming request,
// computed once by prepareRequest and passed to the individual handlers.
type requestContext struct {
	config   JsonData
	fileData FileData
	subpath  string
	diskPath string
	fileInfo os.FileInfo
}

type fileJob struct {
	path    string
	relPath string
}

// #endregion

// #region config

func loadConfig() (JsonData, error) {
	file, err := os.Open(configFilePath)
	if err != nil {
		return JsonData{}, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config JsonData
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return JsonData{}, fmt.Errorf("failed to decode config: %w", err)
	}
	return config, nil
}

func saveConfig(config JsonData) error {
	file, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to open config file for writing: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}

// #endregion

// #region logging

func requestToJSON(r *http.Request) (string, error) {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		return "", err
	}

	getIP := func() string {
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			return ip
		}
		if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
			return ip
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}

	headers := make(map[string]string)
	for name, values := range r.Header {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}

	var bodyContent interface{}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				var jsonBody interface{}
				if json.Unmarshal(bodyBytes, &jsonBody) == nil {
					bodyContent = jsonBody
				} else {
					bodyContent = string(bodyBytes)
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
			_ = r.ParseForm()
			bodyContent = r.Form
		}
	}

	var files []map[string]interface{}
	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
			for field, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					files = append(files, map[string]interface{}{
						"field":       field,
						"filename":    fh.Filename,
						"size_bytes":  fh.Size,
						"contenttype": fh.Header.Get("Content-Type"),
					})
				}
			}
		}
	}

	logData := map[string]interface{}{
		"method":    r.Method,
		"url":       decodedURL,
		"client_ip": getIP(),
		"headers":   headers,
	}
	if bodyContent != nil {
		logData["body"] = bodyContent
	}
	if len(files) > 0 {
		logData["uploaded_files"] = files
	}

	jsonBytes, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// #endregion

// #region request handling

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
	config, err := loadConfig()
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
	if !strings.HasPrefix(diskPath, fileData.Path) {
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

// handleGet serves a file or directory for GET and HEAD requests.
func handleGet(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	fd := ctx.fileData

	// Expired oder Uses aufgebraucht
	if fd.Uses == 0 || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix()) {
		fd.Expired = true
		ctx.config.Files[ctx.subpath] = fd
		if err := saveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config: %v", err)
		}
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	// Uses dekrementieren
	if fd.Uses > 0 {
		fd.Uses--
		ctx.config.Files[ctx.subpath] = fd
	}
	if err := saveConfig(ctx.config); err != nil {
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

	err := r.ParseMultipartForm(100 << 30) // 100 GB - intentionally high for private use
	if err != nil {
		GoLog.Errorf("could not parse multipart form: %v", err)
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
// and then dispatches to the appropriate handler based on the HTTP method.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	GoLog.Infof(requestToJSON(r))

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

// serveDirectory renders the directory listing HTML or streams a ZIP archive.
func serveDirectory(w http.ResponseWriter, dirPath, subpath, basePath string, UploadTime int64, Expiration int64, Uses int, AllowPost bool, r *http.Request) {
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string { return strings.ToLower(filepath.Ext(filename)) },
	}).ParseFiles(templateFilePath)
	if err != nil {
		GoLog.Errorf("error parsing template: %v", err)
		return
	}

	if err := t.Execute(w, PageData{
		Subpath:    subpath,
		UploadTime: UploadTime,
		DirPath:    filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:      func() []FileInfo { infos, _ := getFileInfos(dirPath, basePath); return infos }(),
		ParentDir: func() string {
			if dirPath == basePath {
				return "/"
			}
			return filepath.Join("/", strings.TrimPrefix(filepath.Dir(dirPath), basePath))
		}(),
		HasParentDir: dirPath != basePath,
		Uses:         Uses,
		Expiration:   Expiration,
		AllowPost:    AllowPost,
	}); err != nil {
		GoLog.Errorf("error rendering template: %v", err)
	}
}

// getFileInfos reads a directory and returns a list of FileInfo structs.
func getFileInfos(dirPath, basePath string) ([]FileInfo, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var fileInfos []FileInfo
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".") {
			continue
		}
		fileInfos = append(fileInfos, FileInfo{
			Name:  file.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), file.Name()),
			IsDir: file.IsDir(),
		})
	}
	return fileInfos, nil
}

// #endregion

// #region zip

// zipAndServe creates a ZIP archive of dirPath and streams it to the client.
func zipAndServe(w http.ResponseWriter, dirPath string) {
	numWorkers := runtime.NumCPU() - 1
	if numWorkers < 1 {
		numWorkers = 1
	}

	_, folderName := filepath.Split(filepath.Clean(dirPath))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", folderName))

	pr, pw := io.Pipe()
	zipWriter := zip.NewWriter(pw)
	var wg sync.WaitGroup
	jobs := make(chan fileJob)
	var zipMutex sync.Mutex

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				zipMutex.Lock()
				file, err := os.Open(job.path)
				if err == nil {
					zipFile, err := zipWriter.Create(job.relPath)
					if err == nil {
						io.Copy(zipFile, file)
					}
					file.Close()
				}
				zipMutex.Unlock()
			}
		}()
	}

	var walkWG sync.WaitGroup
	walkWG.Add(1)
	go func() {
		defer walkWG.Done()
		filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err == nil && path != dirPath {
				relPath, err := filepath.Rel(dirPath, path)
				if err == nil {
					jobs <- fileJob{path, relPath}
				}
			}
			return nil
		})
		close(jobs)
	}()

	go func() {
		walkWG.Wait()
		wg.Wait()
		zipWriter.Close()
		pw.Close()
	}()

	io.Copy(w, pr)
}

// #endregion

// #region upload

// saveUploadedFile saves a single uploaded file to uploadDir.
// If a file with the same name already exists, a unix timestamp is appended.
func saveUploadedFile(fileHeader *multipart.FileHeader, uploadDir string) error {
	src, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("error opening uploaded file: %w", err)
	}
	defer src.Close()

	dstPath := filepath.Join(uploadDir, filepath.Base(fileHeader.Filename))
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		timestamp := time.Now().Unix()
		dstPath = filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s",
			strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename)),
			timestamp,
			filepath.Ext(fileHeader.Filename)))
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("error creating file on server: %w", err)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		return fmt.Errorf("error saving file: %w", err)
	}
	return nil
}

// #endregion

// #region server

func startServer(port int) {
	http.HandleFunc("/", handleRequest)
	GoLog.Infof(fmt.Sprintf("Server running at http://localhost:%d", port))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		GoLog.Errorf("failed to start server: %v", err)
	}
}

func main() {
	err := GoLog.ToFile()
	if err != nil {
		GoLog.Errorf("error: %v", err)
		return
	}
	config, err := loadConfig()
	if err != nil {
		GoLog.Errorf("error loading config: %v", err)
		return
	}
	startServer(config.Port)
}

// #endregion
