#!/bin/bash
# ╔══════════════════════════════════════════════════════╗
# ║         Fileshare - Full Migration Script            ║
# ║  Führe aus dem Repository-Root aus (wo LICENSE liegt)║
# ╚══════════════════════════════════════════════════════╝
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }
section() { echo -e "\n${CYAN}══ $1 ══${NC}"; }

# ── Sicherheitscheck ──────────────────────────────────
if [ ! -f "README.md" ] && [ ! -d "working-code" ]; then
    error "Bitte aus dem Repository-Root ausführen!"
fi

section "Ordnerstruktur erstellen"
mkdir -p cmd/server
mkdir -p cmd/cli
mkdir -p assets
mkdir -p configs
mkdir -p scripts
log "Ordner erstellt"

# ══════════════════════════════════════════════════════
section "cmd/server/config.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/config.go << 'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// #region constants

const (
	configFilePath   = "./data.json"
	templateFilePath = "./template.html"
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
	Path       string `json:"path"`
	UploadTime int64  `json:"upload_time"`
	Uses       int    `json:"uses"`
	Expiration int64  `json:"expiration"`
	Expired    bool   `json:"expired"`
	AllowPost  bool   `json:"allow_post"`
	Password   string `json:"password"`
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

// #endregion

// #region config I/O

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
EOF
log "cmd/server/config.go"

# ══════════════════════════════════════════════════════
section "cmd/server/logging.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/logging.go << 'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// requestToJSON serializes an HTTP request to a JSON string for logging.
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

	// Sensitive Header vor dem Loggen entfernen
	headers := make(map[string]string)
	sensitiveHeaders := map[string]bool{
		"Authorization": true,
		"Cookie":        true,
		"X-Auth-Token":  true,
	}
	for name, values := range r.Header {
		if sensitiveHeaders[name] {
			continue
		}
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
EOF
log "cmd/server/logging.go"

# ══════════════════════════════════════════════════════
section "cmd/server/upload.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/upload.go << 'EOF'
package main

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
EOF
log "cmd/server/upload.go"

# ══════════════════════════════════════════════════════
section "cmd/server/zip.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/zip.go << 'EOF'
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

type fileJob struct {
	path    string
	relPath string
}

// zipAndServe creates a ZIP archive of dirPath and streams it to the client.
// Uses multiple worker goroutines for parallel compression.
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
EOF
log "cmd/server/zip.go"

# ══════════════════════════════════════════════════════
section "cmd/server/handler.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/handler.go << 'EOF'
package main

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
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
		if err := saveConfig(ctx.config); err != nil {
			GoLog.Errorf("failed to save config: %v", err)
		}
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	// Uses dekrementieren wenn nicht unendlich
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
func serveDirectory(w http.ResponseWriter, dirPath, subpath, basePath string, uploadTime int64, expiration int64, uses int, allowPost bool, r *http.Request) {
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string {
			return strings.ToLower(filepath.Ext(filename))
		},
	}).ParseFiles(templateFilePath)
	if err != nil {
		GoLog.Errorf("error parsing template: %v", err)
		return
	}

	if err := t.Execute(w, PageData{
		Subpath:    subpath,
		UploadTime: uploadTime,
		DirPath:    filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:      func() []FileInfo { infos, _ := getFileInfos(dirPath, basePath); return infos }(),
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
func getFileInfos(dirPath, basePath string) ([]FileInfo, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
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
EOF
log "cmd/server/handler.go"

# ══════════════════════════════════════════════════════
section "cmd/server/main.go"
# ══════════════════════════════════════════════════════
cat > cmd/server/main.go << 'EOF'
package main

import (
	"fmt"
	"net/http"

	"github.com/Wirezat/GoLog"
)

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
		GoLog.Errorf("error initializing logger: %v", err)
		return
	}

	config, err := loadConfig()
	if err != nil {
		GoLog.Errorf("error loading config: %v", err)
		return
	}

	startServer(config.Port)
}
EOF
log "cmd/server/main.go"

# ══════════════════════════════════════════════════════
section "cmd/cli/main.go"
# ══════════════════════════════════════════════════════
cat > cmd/cli/main.go << 'EOF'
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// #region structs

type Sharedata struct {
	Path       string `json:"path"`
	UploadTime int64  `json:"upload_time"`
	Uses       int    `json:"uses"`
	Expiration int64  `json:"expiration"`
	Expired    bool   `json:"expired"`
	AllowPost  bool   `json:"allow_post"`
	Password   string `json:"password"`
}

type JsonData struct {
	Port          int                 `json:"port"`
	AdminPassword string              `json:"admin_password"`
	AdminSalt     string              `json:"admin_salt"`
	Files         map[string]Sharedata `json:"files"`
}

var instructionPath string = "/opt/fileshare/data.json"
var randomSubpathLength int = 12

// #endregion

func generateRandomSubpath(length int) string {
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-"
	subpath := ""
	for i := 0; i < length; i++ {
		subpath += string(validChars[rand.Intn(len(validChars))])
	}
	return subpath
}

func loadJsonData(filepath string, target interface{}) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("error opening file '%s': %v", filepath, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("error decoding JSON data from '%s': %v", filepath, err)
	}

	if jsonData, ok := target.(*JsonData); ok {
		if jsonData.Files == nil {
			jsonData.Files = make(map[string]Sharedata)
		}
	}
	return nil
}

func writeJsonData(filepath string, target interface{}) error {
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "    ")
	err = encoder.Encode(target)
	if err != nil {
		return fmt.Errorf("error encoding JSON: %v", err)
	}
	return nil
}

func list(path string) {
	const (
		green     = "\033[32m"
		red       = "\033[31m"
		yellow    = "\033[33m"
		blue      = "\033[34m"
		cyan      = "\033[36m"
		reset     = "\033[0m"
		underline = "\033[4m"
	)

	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("%s%sLIST OF SHARED FILES%s\n%s----------------------%s\n", cyan, underline, reset, cyan, reset)

	for name, shareData := range jsonData.Files {
		if _, err := os.Stat(shareData.Path); os.IsNotExist(err) {
			fmt.Printf("%sSubpath:%s %s\n", yellow, reset, name)
			fmt.Printf("%sFilepath:%s %s\n", red, reset, shareData.Path)
			fmt.Printf("%sWarning: File does not exist at path %s%s\n", red, shareData.Path, reset)
		} else {
			status := ""
			if shareData.Expired {
				status = red + " [EXPIRED]" + reset
			}
			fmt.Printf("%sSubpath:%s %s%s\n", green, reset, name, status)
			fmt.Printf("%sFilepath:%s %s\n", blue, reset, shareData.Path)
		}
		fmt.Printf("%s  UploadTime:%s %d\n", cyan, reset, shareData.UploadTime)
		fmt.Printf("%s  Uses:%s %d\n", cyan, reset, shareData.Uses)
		fmt.Printf("%s  Expiration:%s %d\n", cyan, reset, shareData.Expiration)
		fmt.Println()
	}
}

func del(path string, subpath string) {
	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Deleting link %s to file: %s\n", subpath, jsonData.Files[subpath].Path)
		delete(jsonData.Files, subpath)
		err := writeJsonData(path, &jsonData)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Subpath deleted successfully.")
	} else {
		fmt.Printf("Subpath %s not found.\n", subpath)
	}
}

func add(path string, subpath string, filePath string, uses int, expiration int64, allowPost bool) {
	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Subpath %s already exists.\n", subpath)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Warning: File path %s does not exist.\n", filePath)
		return
	}

	jsonData.Files[subpath] = Sharedata{
		Path:       filePath,
		UploadTime: time.Now().Unix(),
		Uses:       uses,
		Expiration: expiration,
		AllowPost:  allowPost,
	}

	err = writeJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Added subpath %s with file path %s successfully.\n", subpath, filePath)
}

func edit(subpath, newSubpath string) {
	var jsonData JsonData
	if err := loadJsonData(instructionPath, &jsonData); err != nil {
		fmt.Println(err)
		return
	}
	if shareData, ok := jsonData.Files[subpath]; ok {
		fmt.Printf("Changing subpath %s → %s\n", subpath, newSubpath)
		add(instructionPath, newSubpath, shareData.Path, shareData.Uses, shareData.Expiration, shareData.AllowPost)
		del(instructionPath, subpath)
		fmt.Println("Successfully changed subpath.")
	} else {
		fmt.Printf("No match found for subpath: %s\n", subpath)
	}
}

func calculateExpirationTime(duration string) (int64, error) {
	if duration == "" {
		return 0, nil
	}

	num, err := strconv.Atoi(strings.TrimRight(duration, "hdwmy"))
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %v", err)
	}

	unit := duration[len(duration)-1:]
	now := time.Now()

	switch unit {
	case "h":
		return now.Add(time.Duration(num) * time.Hour).Unix(), nil
	case "d":
		return now.AddDate(0, 0, num).Unix(), nil
	case "w":
		return now.AddDate(0, 0, num*7).Unix(), nil
	case "m":
		return now.AddDate(0, num, 0).Unix(), nil
	case "y":
		return now.AddDate(num, 0, 0).Unix(), nil
	default:
		return 0, fmt.Errorf("invalid unit in duration: %s", unit)
	}
}

func printTooltips(command string) {
	helpText := map[string]string{
		"list": `NAME
       list - Displays all shared files and their assigned subpaths

SYNOPSIS
       list, l`,

		"add": `NAME
       add - Creates a new share for the specified file under the given subpath

SYNOPSIS
       add -subpath=<subpath> -file=<file>
           [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]
           [-allow-post]`,

		"addrandom": `NAME
       addrandom - Creates a new share with a randomly generated subpath

SYNOPSIS
       addrandom -file=<file>
           [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]
           [-allow-post]`,

		"delete": `NAME
       delete - Removes an existing share

SYNOPSIS
       delete -subpath=<subpath>`,

		"edit": `NAME
       edit - Changes the subpath of an existing share

SYNOPSIS
       edit -old_subpath=<old> -new_subpath=<new>`,
	}

	if command == "" || helpText[command] == "" {
		fmt.Println("╔════════════════════════════╗")
		fmt.Println("║     AVAILABLE COMMANDS     ║")
		fmt.Println("╚════════════════════════════╝")
		for cmd, text := range helpText {
			fmt.Printf("\n• %s\n  %s\n", cmd, text)
		}
		return
	}

	fmt.Println("╔════════════════════════════")
	fmt.Printf("║   COMMAND: %s\n", command)
	fmt.Println("╚════════════════════════════")
	fmt.Println(helpText[command])
}

func main() {
	if len(os.Args) < 2 {
		printTooltips("")
		return
	}

	oldSubpathFlag := flag.String("old_subpath", "", "")
	flag.StringVar(oldSubpathFlag, "old", "", "")
	flag.StringVar(oldSubpathFlag, "o", "", "")

	newSubpathFlag := flag.String("new_subpath", "", "")
	flag.StringVar(newSubpathFlag, "new", "", "")
	flag.StringVar(newSubpathFlag, "n", "", "")

	subpathFlag := flag.String("subpath", "", "")
	flag.StringVar(subpathFlag, "s", "", "")

	filePathFlag := flag.String("file", "", "")
	flag.StringVar(filePathFlag, "f", "", "")

	usageLimitFlag := flag.Int("use-expiration", -1, "")
	flag.IntVar(usageLimitFlag, "uses", -1, "")
	flag.IntVar(usageLimitFlag, "u", -1, "")

	expirationTimeFlag := flag.String("time-expiration", "", "")
	flag.StringVar(expirationTimeFlag, "time", "", "")
	flag.StringVar(expirationTimeFlag, "t", "", "")

	allowPostFlag := flag.Bool("allow-post", false, "")
	flag.BoolVar(allowPostFlag, "post", false, "")
	flag.BoolVar(allowPostFlag, "p", false, "")

	flag.CommandLine.Parse(os.Args[2:])

	expirationTime, err := calculateExpirationTime(*expirationTimeFlag)
	if err != nil {
		fmt.Println("Error calculating expiration:", err)
		return
	}

	switch os.Args[1] {
	case "list", "l":
		list(instructionPath)

	case "delete", "del", "remove", "rm":
		if *subpathFlag == "" {
			printTooltips("delete")
			return
		}
		del(instructionPath, *subpathFlag)

	case "add":
		if *subpathFlag == "" || *filePathFlag == "" {
			printTooltips("add")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		add(instructionPath, *subpathFlag, absPath, *usageLimitFlag, expirationTime, *allowPostFlag)

	case "addrandom", "random", "add_random", "addr":
		if *filePathFlag == "" {
			printTooltips("addrandom")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		randomSubpath := generateRandomSubpath(randomSubpathLength)
		fmt.Printf("Random subpath: %s\n", randomSubpath)
		add(instructionPath, randomSubpath, absPath, *usageLimitFlag, expirationTime, *allowPostFlag)

	case "edit":
		if *oldSubpathFlag == "" || *newSubpathFlag == "" {
			printTooltips("edit")
			return
		}
		edit(*oldSubpathFlag, *newSubpathFlag)

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printTooltips("")
	}
}
EOF
log "cmd/cli/main.go"

# ══════════════════════════════════════════════════════
section "go.mod"
# ══════════════════════════════════════════════════════
cat > go.mod << 'EOF'
module github.com/Wirezat/fileshare

go 1.23.6

toolchain go1.24.4

require (
	github.com/Wirezat/GoLog v0.0.0-20250731031935-25a19f441e3f
	github.com/chzyer/readline v1.5.1
)

require golang.org/x/sys v0.0.0-20220310020820-b874c991c1a5 // indirect
EOF
log "go.mod"

# ══════════════════════════════════════════════════════
section "assets/template.html"
# ══════════════════════════════════════════════════════
if [ -f "working-code/internal/template.html" ]; then
    cp working-code/internal/template.html assets/template.html
    log "assets/template.html (aus working-code kopiert)"
else
    warn "template.html nicht gefunden - bitte manuell nach assets/ kopieren"
fi

# ══════════════════════════════════════════════════════
section "configs/data.example.json"
# ══════════════════════════════════════════════════════
cat > configs/data.example.json << 'EOF'
{
  "port": 27182,
  "admin_password": "",
  "admin_salt": "",
  "files": {
    "example": {
      "path": "/path/to/your/file/or/folder",
      "upload_time": 0,
      "uses": -1,
      "expiration": 0,
      "expired": false,
      "allow_post": false,
      "password": ""
    }
  }
}
EOF
log "configs/data.example.json"

# ══════════════════════════════════════════════════════
section "scripts/fileshare.service"
# ══════════════════════════════════════════════════════
cat > scripts/fileshare.service << 'EOF'
[Unit]
Description=Fileshare Service
After=network.target

[Service]
Type=simple
ExecStart=/opt/fileshare/fileshare-backend
WorkingDirectory=/opt/fileshare
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
log "scripts/fileshare.service"

# ══════════════════════════════════════════════════════
section "scripts/install.sh"
# ══════════════════════════════════════════════════════
cat > scripts/install.sh << 'EOF'
#!/bin/bash
# Fileshare Installer
# Voraussetzung: scripts/update.sh wurde vorher ausgeführt
# Usage: sudo bash scripts/install.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

[ "$EUID" -ne 0 ] && error "Bitte als root ausführen: sudo bash scripts/install.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="/opt/fileshare"

# Prüfen ob Binaries existieren
[ ! -f "$REPO_ROOT/fileshare-backend" ]   && error "fileshare-backend nicht gefunden. Bitte zuerst 'bash scripts/update.sh' ausführen."
[ ! -f "$REPO_ROOT/fileshare-interface" ] && error "fileshare-interface nicht gefunden. Bitte zuerst 'bash scripts/update.sh' ausführen."

log "Erstelle $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
chown -R ${SUDO_USER:-$USER}:${SUDO_USER:-$USER} "$INSTALL_DIR"

log "Kopiere Binaries..."
cp "$REPO_ROOT/fileshare-backend"   "$INSTALL_DIR/fileshare-backend"
cp "$REPO_ROOT/fileshare-interface" "$INSTALL_DIR/fileshare-interface"
chmod +x "$INSTALL_DIR/fileshare-backend"
chmod +x "$INSTALL_DIR/fileshare-interface"

log "Kopiere Assets..."
cp "$REPO_ROOT/assets/template.html" "$INSTALL_DIR/template.html"

# data.json nur anlegen wenn noch nicht vorhanden
if [ ! -f "$INSTALL_DIR/data.json" ]; then
    log "Erstelle initiale data.json aus Beispiel-Config..."
    cp "$REPO_ROOT/configs/data.example.json" "$INSTALL_DIR/data.json"
    warn "Bitte $INSTALL_DIR/data.json anpassen!"
else
    log "data.json bereits vorhanden – wird nicht überschrieben."
fi

log "Installiere systemd Service..."
cp "$REPO_ROOT/scripts/fileshare.service" /etc/systemd/system/fileshare.service
/sbin/restorecon -v /etc/systemd/system/fileshare.service 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now fileshare.service

log "Erstelle CLI-Symlink..."
LINK="/usr/local/bin/fileshare"
[ -L "$LINK" ] && rm "$LINK"
ln -s "$INSTALL_DIR/fileshare-interface" "$LINK"
log "  $LINK → $INSTALL_DIR/fileshare-interface"

echo ""
log "Installation abgeschlossen!"
warn "Vergiss nicht, $INSTALL_DIR/data.json zu konfigurieren."
EOF
chmod +x scripts/install.sh
log "scripts/install.sh"

# ══════════════════════════════════════════════════════
section "scripts/uninstall.sh"
# ══════════════════════════════════════════════════════
cat > scripts/uninstall.sh << 'EOF'
#!/bin/bash
# Fileshare Uninstaller
# Usage: sudo bash scripts/uninstall.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

[ "$EUID" -ne 0 ] && error "Bitte als root ausführen: sudo bash scripts/uninstall.sh"

log "Stoppe Service..."
systemctl stop    fileshare.service 2>/dev/null || warn "Service war nicht aktiv"
systemctl disable fileshare.service 2>/dev/null || warn "Service war nicht aktiviert"
systemctl daemon-reload

log "Entferne Service-Datei..."
rm -f /etc/systemd/system/fileshare.service

log "Entferne CLI-Symlink..."
rm -f /usr/local/bin/fileshare

log "Entferne /opt/fileshare..."
rm -rf /opt/fileshare

echo ""
log "Deinstallation abgeschlossen."
EOF
chmod +x scripts/uninstall.sh
log "scripts/uninstall.sh"

# ══════════════════════════════════════════════════════
section "scripts/update.sh"
# ══════════════════════════════════════════════════════
cat > scripts/update.sh << 'EOF'
#!/bin/bash
# Fileshare Updater – baut Binaries und deployt sie
# Usage: bash scripts/update.sh
# Hinweis: Deployment benötigt sudo (wird intern aufgerufen)

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$REPO_ROOT"

GO=/usr/local/go/bin/go
[ ! -f "$GO" ] && error "Go nicht gefunden unter $GO"

# ── Build ──────────────────────────────────────────────
log "Baue fileshare-backend..."
GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-backend ./cmd/server/

log "Baue fileshare-interface..."
GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-interface ./cmd/cli/

log "Build abgeschlossen."

# ── Deploy (nur wenn bereits installiert) ─────────────
if [ -d "/opt/fileshare" ]; then
    log "Deploye nach /opt/fileshare..."
    sudo cp fileshare-backend    /opt/fileshare/fileshare-backend
    sudo cp fileshare-interface  /opt/fileshare/fileshare-interface
    sudo cp assets/template.html /opt/fileshare/template.html

    sudo restorecon -v /opt/fileshare/fileshare-backend  2>/dev/null || true
    sudo restorecon -v /opt/fileshare/fileshare-interface 2>/dev/null || true

    log "Starte Service neu..."
    sudo systemctl restart fileshare.service
    log "Update abgeschlossen."
else
    log "Build abgeschlossen. /opt/fileshare nicht gefunden – für Erstinstallation 'sudo bash scripts/install.sh' ausführen."
fi
EOF
chmod +x scripts/update.sh
log "scripts/update.sh"

# ══════════════════════════════════════════════════════
section "scripts/logview.sh"
# ══════════════════════════════════════════════════════
if [ -f "working-code/logview.sh" ]; then
    cp working-code/logview.sh scripts/logview.sh
    chmod +x scripts/logview.sh
    log "scripts/logview.sh (aus working-code kopiert)"
else
    warn "logview.sh nicht gefunden"
fi

# ══════════════════════════════════════════════════════
section ".gitignore"
# ══════════════════════════════════════════════════════
cat > .gitignore << 'EOF'
# Binaries
fileshare-backend
fileshare-interface

# Go
*.test
go.sum

# Laufzeit-Konfiguration (bleibt auf dem Server)
data.json

# Logs
*.log

# IDE
.idea/
.vscode/
EOF
log ".gitignore"

# ══════════════════════════════════════════════════════
section "Zusammenfassung"
# ══════════════════════════════════════════════════════
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║        Migration abgeschlossen!          ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
echo ""
echo "Neue Struktur:"
find cmd assets configs scripts go.mod .gitignore -type f 2>/dev/null | sort | sed 's/^/  /'
echo ""
echo -e "${YELLOW}Nächste Schritte:${NC}"
echo "  1. go.sum generieren:        /usr/local/go/bin/go mod tidy"
echo "  2. Build testen:             bash scripts/update.sh"
echo "  3. Erstinstallation:         sudo bash scripts/install.sh"
echo "  4. Altes working-code/ prüfen und ggf. löschen"
echo ""
echo -e "${YELLOW}Hinweis:${NC} working-code/ wurde nicht angefasst."