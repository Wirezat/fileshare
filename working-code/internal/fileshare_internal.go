package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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
	AllowPost  bool
}

// JsonData contains the list of shared files and the port used by the program
type JsonData struct {
	Port  int                 `json:"port"`  // Port used by the program
	Files map[string]FileData `json:"files"` // Details for the sharing configurations per file
}

// for multithreaded zipping.
type fileJob struct {
	path    string
	relPath string
}

// #endregion

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

func logToFile(level, message string) {
	logDir := "./logs"
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.Mkdir(logDir, 0755)
	}

	day := time.Now().Format("20060102")
	logFile := filepath.Join(logDir, day+".log")

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening log file:", err)
		return
	}
	defer file.Close()

	logData := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     level,
		"message":   message,
	}
	logJSON, _ := json.Marshal(logData)
	file.WriteString(string(logJSON) + "\n")
}

func logError(w http.ResponseWriter, r *http.Request, err error) {
	logData := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     "ERROR",
		"request": map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
		},
		"error": err.Error(),
		"response": map[string]interface{}{
			"status_code": http.StatusInternalServerError,
			"message":     "An error occurred while processing your request",
		},
	}
	logJSON, _ := json.Marshal(logData)
	logToFile("ERROR", string(logJSON))

	http.Error(w, "An error occurred while processing your request", http.StatusInternalServerError)
}

func logInfo(message string) {
	logData := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     "INFO",
		"message":   message,
	}
	logJSON, _ := json.Marshal(logData)
	logToFile("INFO", string(logJSON))
}

// logRequest logs basic information about an incoming HTTP request,
// including timestamp, method, requested URL, and client IP address.
// It prefers proxy headers (X-Forwarded-For, Cf-Connecting-Ip) to determine the IP.
// The log is saved in JSON format via logToFile.
func logRequest(r *http.Request) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	decodedURL, _ := url.QueryUnescape(r.URL.RequestURI())

	// Bestimme Client-IP: Priorität XFF > CF > RemoteAddr (IPv4/IPv6)
	getIP := func() string {
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			return ip
		}
		if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
			return ip
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			if ip := net.ParseIP(host); ip != nil {
				return ip.String()
			}
			return host
		}
		return r.RemoteAddr
	}

	logData := map[string]interface{}{
		"timestamp": timestamp,
		"method":    r.Method,
		"url":       decodedURL,
		"client_ip": getIP(),
	}

	// Datei-Metadaten bei POST (multipart/form-data)
	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err == nil {
			var fileInfos []map[string]interface{}
			for field, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					fileInfos = append(fileInfos, map[string]interface{}{
						"field":       field,
						"filename":    fh.Filename,
						"size_bytes":  fh.Size,
						"contenttype": fh.Header.Get("Content-Type"),
					})
				}
			}
			if len(fileInfos) > 0 {
				logData["uploaded_files"] = fileInfos
			}
		}
	}

	if logJSON, err := json.Marshal(logData); err == nil {
		logToFile("REQUEST", string(logJSON))
	}
}

// #endregion

// handleRequest processes requests for files and directories.
// The first path segment is treated as a subpath to fetch the corresponding file metadata.
// If the requested file or directory is found, it is either served or an appropriate error is returned.
// handleRequest processes requests for files and directories.
// It logs the request and checks if it's a GET, HEAD or POST request.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	// exit clause for wrong http method
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		// Respond with allowed methods in case of unsupported methods
		w.Header().Set("Allow", "GET, POST, HEAD")
		logError(w, r, fmt.Errorf("unsupported method: %s", r.Method))
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load the configuration containing the file metadata
	config, err := loadConfig()
	if err != nil {
		logError(w, r, fmt.Errorf("failed to load config: %v", err))
		return
	}

	// Split the URL path and extract the subpath for file selection
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(pathParts) == 0 {
		http.NotFound(w, r)
		return
	}

	// Retrieve the file metadata from the config based on the subpath
	subpath := pathParts[0]
	FileData, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return
	}

	// Create the full path to the requested file or directory (Absolute path on drive)
	remainingPath := filepath.Join(FileData.Path, filepath.Join(pathParts[1:]...))
	fileInfo, err := os.Stat(remainingPath)
	if err != nil {
		logError(w, r, fmt.Errorf("error accessing file: %v", err))
		return
	}

	handleGet := func() {
		// Check if the file is expired or has been used up, and delete it if necessary
		if FileData.Uses == 0 || (FileData.Expiration != 0 && FileData.Expiration < FileData.UploadTime) {
			delete(config.Files, subpath)
			http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
		} else if FileData.Uses > 0 {
			// Decrease the use count if the file is still available
			fd := config.Files[subpath]
			fd.Uses--
			config.Files[subpath] = fd
		}
		if err := saveConfig(config); err != nil {
			logError(w, r, fmt.Errorf("failed to save config: %v", err))
			return
		}
		if fileInfo.IsDir() {
			serveDirectory(w, remainingPath, subpath, FileData.Path, FileData.UploadTime, FileData.Expiration, FileData.Uses, FileData.AllowPost, r)
		} else {
			http.ServeFile(w, r, remainingPath)
		}
	}

	handlePost := func(w http.ResponseWriter, r *http.Request, uploadDir string) {
		err := r.ParseMultipartForm(100 << 30)

		if err != nil {
			logError(w, r, fmt.Errorf("could not parse multipart form: %v", err))
			return
		}

		files := r.MultipartForm.File["files"]
		for _, fileHeader := range files {

			// open file
			src, err := fileHeader.Open()
			if err != nil {
				logError(w, r, fmt.Errorf("error opening uploaded file: %v", err))
				return
			}
			defer src.Close()

			// path for the file to be on the server. add unix timestamp if filename already exist
			dstPath := filepath.Join(uploadDir, filepath.Base(fileHeader.Filename))
			if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
				timestamp := time.Now().Unix()
				dstPath = filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s", strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename)), timestamp, filepath.Ext(fileHeader.Filename)))
			}

			// create file
			dst, err := os.Create(dstPath)
			if err != nil {
				logError(w, r, fmt.Errorf("error creating file on server: %v", err))
				return
			}
			defer dst.Close()

			// copy file to destination
			_, err = io.Copy(dst, src)
			if err != nil {
				logError(w, r, fmt.Errorf("error saving file: %v", err))
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
		if !FileData.AllowPost {
			http.Error(w, "POST Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePost(w, r, remainingPath)
	default:
		//handleGet()
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
		logError(w, r, fmt.Errorf("error parsing template: %v", err))
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
		logError(w, r, fmt.Errorf("error rendering template: %v", err))
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
	logInfo(fmt.Sprintf("Server running at http://localhost:%d", port))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		logError(nil, nil, fmt.Errorf("failed to start server: %v", err))
	}
}

func main() {
	// load configuration and Start Server
	config, err := loadConfig()
	if err != nil {
		logError(nil, nil, fmt.Errorf("error: %v", err))
		return
	}
	startServer(config.Port)
}
