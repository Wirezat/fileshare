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

const (
	configFilePath   = "./data.json"
	templateFilePath = "template.html"
)

// #region various structs
type FileInfo struct {
	Name  string // Name of file or directory
	Path  string // path of the requested item, relative to the path defined in the JSON
	IsDir bool
}

// PageData contains all necessary files for loading the folder share html
type PageData struct {
	Subpath      string // requested subpath
	UploadTime   int64
	DirPath      string // current requested path
	Files        []FileInfo
	ParentDir    string
	HasParentDir bool
	Uses         int
	Expiration   int64
	AllowPost    bool
}

// FileData contains the details for the sharing configurations per file
type FileData struct {
	Path       string
	UploadTime int64
	Uses       int
	Expiration int64
	Expired    bool
	AllowPost  bool
	Password   string
}

// JsonData contains the list of shared files and the port used by the program
type JsonData struct {
	Port          int                 `json:"port"`           // Port used by the program
	AdminPassword string              `json:"admin_password"` // Password for admin access (not implemented yet)
	AdminSalt     string              `json:"admin_salt"`     // Salt for admin password hashing (not implemented yet)
	Files         map[string]FileData `json:"files"`          // Details for the sharing configurations per file
}

type fileJob struct {
	path    string
	relPath string
}

func requestToJSON(r *http.Request) (string, error) {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		return "", err
	}

	// Client-IP ermitteln
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

	// Header sammeln (optional)
	headers := make(map[string]string)
	for name, values := range r.Header {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}

	// Body lesen (nur bei nicht-multipart)
	var bodyContent interface{}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			// JSON Body
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				var jsonBody interface{}
				if json.Unmarshal(bodyBytes, &jsonBody) == nil {
					bodyContent = jsonBody
				} else {
					bodyContent = string(bodyBytes)
				}
			}
			// Body muss zurückgesetzt werden, falls weitere Handler es brauchen
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
			_ = r.ParseForm()
			bodyContent = r.Form
		}
	}

	// Multipart-Dateien
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

	// Zusammensetzen der JSON-Daten
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

// #region log and config management functions
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
	// formatting for JSON
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}

// saveUploadedFile saves a single uploaded file to the given directory.
// If a file with the same name already exists, a unix timestamp is appended to the filename.
// Parameters:
//   - fileHeader: The multipart file header containing the file data.
//   - uploadDir: The directory to save the file to.
func saveUploadedFile(fileHeader *multipart.FileHeader, uploadDir string) error {
	src, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("error opening uploaded file: %w", err)
	}
	defer src.Close()

	// path for the file to be on the server. add unix timestamp if filename already exists
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

// handleRequest processes requests for files and directories.
// The first path segment is treated as a subpath to fetch the corresponding file metadata.
// If the requested file or directory is found, it is either served or an appropriate error is returned.
// handleRequest processes requests for files and directories.
// It logs the request and checks if it's a GET, HEAD or POST request.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	GoLog.Infof(requestToJSON(r))

	// Methode prüfen
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, POST, HEAD")
		GoLog.Errorf("unsupported method: %s", r.Method)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Config laden
	config, err := loadConfig()
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		return
	}

	// URL aufteilen
	subpath := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
	relativePath := strings.TrimPrefix(r.URL.Path, "/"+subpath)

	// Share aus Config holen
	fileData, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return
	}

	// Disk-Pfad berechnen
	diskPath := filepath.Join(fileData.Path, relativePath)

	// Security-Check: Path Traversal verhindern
	if !strings.HasPrefix(diskPath, fileData.Path) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Expired prüfen
	if fileData.Expired {
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		return
	}

	// Datei/Ordner prüfen
	fileInfo, err := os.Stat(diskPath)
	if err != nil {
		GoLog.Errorf("error accessing file: %v", err)
		http.NotFound(w, r)
		return
	}

	fd := config.Files[subpath]
	handleGet := func() {
		// Check if the file is expired or has been used up, and delete it if necessary
		if fd.Uses == 0 || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix()) {
			fd.Expired = true
			config.Files[subpath] = fd
			if err := saveConfig(config); err != nil {
				GoLog.Errorf("failed to save config: %v", err)
				return
			}
			http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
			return
		} else if fd.Uses > 0 {
			// Decrease the use count if the file is still available
			fd.Uses--
			config.Files[subpath] = fd
		}
		if err := saveConfig(config); err != nil {
			GoLog.Errorf("failed to save config: %v", err)
			return
		}
		if fileInfo.IsDir() {
			serveDirectory(w, diskPath, subpath, fd.Path, fd.UploadTime, fd.Expiration, fd.Uses, fd.AllowPost, r)
		} else {
			http.ServeFile(w, r, diskPath)
		}
	}

	handlePost := func(w http.ResponseWriter, r *http.Request, uploadDir string) {
		err := r.ParseMultipartForm(100 << 30)
		if err != nil {
			GoLog.Errorf("could not parse multipart form: %v", err)
			return
		}

		for _, fileHeader := range r.MultipartForm.File["files"] {
			if err := saveUploadedFile(fileHeader, uploadDir); err != nil {
				GoLog.Errorf("error saving uploaded file: %v", err)
				http.Error(w, "Upload failed", http.StatusInternalServerError)
				return
			}
		}

		w.Write([]byte("Files uploaded successfully"))
	}

	// Check the request method (GET or POST)
	switch r.Method {
	case http.MethodGet:
		handleGet()
	case http.MethodHead:
		handleGet()
	case http.MethodPost:
		if !fd.AllowPost {
			http.Error(w, "POST Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePost(w, r, diskPath)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// serveDirectory handles requests for directories.
// It either displays an HTML view of the directory contents or offers a ZIP download.
// Parameters:
//   - w: http.ResponseWriter for writing the response.
//   - dirPath: The absolute path of the requested directory.
//   - subpath: The subpath of the domain under which the content is hosted.
//   - basePath: The root directory that the subpath refers to (as defined in the JSON under "Path").
//   - r: *http.Request containing the client request.
func serveDirectory(w http.ResponseWriter, dirPath, subpath, basePath string, UploadTime int64, Expiration int64, Uses int, AllowPost bool, r *http.Request) {
	// Check if a ZIP download has been requested.
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	// load and parse the html template
	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string { return strings.ToLower(filepath.Ext(filename)) },
	}).ParseFiles(templateFilePath)
	if err != nil {
		GoLog.Errorf("error parsing template: %v", err)
		return
	}

	// Render html page
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

// getFileInfos reads the contents of a directory and returns a list of FileInfo.
// Parameters:
//   - dirPath: The absolute path of the directory to be scanned.
//   - basePath: The base path used to compute the returned paths relative to it.
//
// Returns:
//   - []FileInfo: A list of data structures, each containing the name,
//     relative path to the host directory [path listed in json], and IsDir status.
//   - error: An error if the directory cannot be read.
func getFileInfos(dirPath, basePath string) ([]FileInfo, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var fileInfos []FileInfo
	// skip hidden files
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

// zipAndServe creates a ZIP archive of the specified directory and streams it to the client.
// The function uses multiple worker goroutines to compress files in parallel and streams
// the archive in real time to the client in order to save memory.
// Parameters:
//   - w: http.ResponseWriter used to stream the ZIP archive to the client.
//   - dirPath: The absolute path of the directory to be zipped and served.
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

	// Mutex for safe access to zipWriter.
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

// startServer starts the HTTP server on the specified port.
// Parameters:
//   - port: The port number on which the server should listen.
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
