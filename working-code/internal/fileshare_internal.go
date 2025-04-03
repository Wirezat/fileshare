package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
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
	configFilePath   = "./data.json"   // Pfad zur Konfigurationsdatei
	templateFilePath = "template.html" // Pfad zur HTML-Template-Datei
)

// FileInfo repräsentiert die Metadaten einer Datei oder eines Verzeichnisses.
type FileInfo struct {
	Name  string // Name der Datei oder des Verzeichnisses
	Path  string // Pfad zur Datei relativ zum Basisverzeichnis
	IsDir bool
}

// PageData enthält alle notwendigen Daten, die zum Rendern einer Seite erforderlich sind.
type PageData struct {
	Subpath      string // Angeforderter Subpfad
	UploadTime   int64
	DirPath      string // Aktueller Pfad des Verzeichnisses
	Files        []FileInfo
	ParentDir    string
	HasParentDir bool
}

// FileData Enthält die Daten des Json eintrags zu jedem Subpath
type FileData struct {
	Path       string
	UploadTime int64
	Uses       int
	Expiration int64
}

// JsonData enthält die Konfigurationseinstellungen aus der JSON-Datei.
type JsonData struct {
	Port  int                 `json:"port"`  // Port, auf dem der Server läuft
	Files map[string]FileData `json:"files"` // Zuordnungen von Subpfaden zu Dateimetadaten
}

// Für multithreaded zipping.
type fileJob struct {
	path    string
	relPath string
}

// loadConfig lädt die Konfiguration aus der JSON-Datei und gibt sie als JsonData zurück.
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
	encoder.SetIndent("", "  ") // Schöne Formatierung für die JSON-Ausgabe
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

	hour := time.Now().Format("2006-01-02-15")
	logFile := filepath.Join(logDir, hour+".log")

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
	logToFile("INFO", message)
}

func logRequest(r *http.Request) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	decodedURL, _ := url.QueryUnescape(r.URL.String())
	headers := make(map[string]string)
	for name, values := range r.Header {
		headers[name] = strings.Join(values, ", ")
	}

	// Falls der Header "X-Forwarded-For" existiert, verwende ihn, andernfalls RemoteAddr
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.Header.Get("Cf-Connecting-Ip")
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}

	logData := map[string]interface{}{
		"timestamp": timestamp,
		"level":     "REQUEST",
		"method":    r.Method,
		"url":       decodedURL,
		"headers":   headers,
		"client_ip": clientIP,
	}

	if r.Body != nil {
		bodyBytes, _ := io.ReadAll(r.Body)
		if len(bodyBytes) > 0 {
			var bodyContent interface{}
			if json.Valid(bodyBytes) {
				json.Unmarshal(bodyBytes, &bodyContent)
			} else {
				bodyContent = string(bodyBytes)
			}
			logData["body"] = bodyContent
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	logJSON, _ := json.Marshal(logData)
	logToFile("REQUEST", string(logJSON))
}

// fileShareHandler verarbeitet Anfragen für Dateien und Verzeichnisse.
// Hier wird der erste Pfadabschnitt als Subpfad interpretiert, der zur Auswahl der entsprechenden Dateimetadaten genutzt wird.
func fileShareHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	config, err := loadConfig()
	if err != nil {
		logError(w, r, fmt.Errorf("failed to load config: %v", err))
		return
	}

	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(pathParts) == 0 {
		http.NotFound(w, r)
		return
	}

	subpath := pathParts[0]
	FileData, exists := config.Files[subpath]
	if !exists {
		http.NotFound(w, r)
		return
	}

	if FileData.Uses == 0 || (FileData.Expiration != 0 && FileData.Expiration < FileData.UploadTime) {
		delete(config.Files, subpath)
		http.Error(w, "File share expired. Please ask your host to re-share it", http.StatusGone)
	} else if FileData.Uses > 0 {
		fd := config.Files[subpath] // Extrahiere den Wert
		fd.Uses--                   // Ändere den Wert
		config.Files[subpath] = fd  // Setze den geänderten Wert wieder in die Map
	}
	saveConfig(config)

	// Erstelle den vollständigen Pfad zur angeforderten Datei oder Verzeichnis
	remainingPath := filepath.Join(FileData.Path, filepath.Join(pathParts[1:]...))
	fileInfo, err := os.Stat(remainingPath)
	if err != nil {
		logError(w, r, fmt.Errorf("error accessing file: %v", err))
		return
	}

	if fileInfo.IsDir() {
		serveDirectory(w, remainingPath, subpath, FileData.Path, FileData.UploadTime, r)
	} else {
		http.ServeFile(w, r, remainingPath)
	}
}

// serveDirectory verarbeitet Anfragen für Verzeichnisse.
// Es zeigt entweder eine HTML-Ansicht des Verzeichnisinhalts an oder bietet einen ZIP-Download an.
// Parameter:
//   - w: http.ResponseWriter zur Ausgabe der Antwort.
//   - dirPath: Der absolute Pfad des angeforderten Verzeichnisses.
//   - subpath: der subpath der Domain, unter der gehostet wird.
//   - basePath: Das Wurzelverzeichnis, auf das sich subpath bezieht (inhalt der JSON:Path).
//   - r: *http.Request mit der Client-Anfrage.
func serveDirectory(w http.ResponseWriter, dirPath, subpath, basePath string, UploadTime int64, r *http.Request) {
	// Prüfe, ob ein ZIP-Download angefordert wurde
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	// Lese die Verzeichnisinhalte
	fileInfos, err := getFileInfos(dirPath, basePath)
	if err != nil {
		logError(w, r, fmt.Errorf("error reading directory: %v", err))
		return
	}

	// Bestimme den Pfad zum Elternverzeichnis
	parentDir := "/"
	if dirPath != basePath {
		parentDir = filepath.Join("/", strings.TrimPrefix(filepath.Dir(dirPath), basePath))
	}

	// Lade und parse das HTML-Template
	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string {
			return strings.ToLower(filepath.Ext(filename))
		},
	}).ParseFiles(templateFilePath)
	if err != nil {
		logError(w, r, fmt.Errorf("error parsing template: %v", err))
		return
	}

	// Render die HTML-Seite
	if err := t.Execute(w, PageData{
		Subpath:      subpath,
		UploadTime:   UploadTime,
		DirPath:      filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:        fileInfos,
		ParentDir:    parentDir,
		HasParentDir: dirPath != basePath,
	}); err != nil {
		logError(w, r, fmt.Errorf("error rendering template: %v", err))
	}
}

// getFileInfos liest den Inhalt eines Verzeichnisses und gibt eine Liste von FileInfo zurück.
// Parameter:
//   - dirPath: Der absolute Pfad des zu durchsuchenden Verzeichnisses.
//   - basePath: Der Basis-Pfad, relativ zu dem die zurückgegebenen Pfade berechnet werden.
//
// Rückgabe:
//   - []FileInfo: Eine Liste von Datenstrukturen, welche jeweils Namen, relativen Pfad zum host verzeichnis und IsDir enthalten.
//   - error: Ein Fehler, falls das Verzeichnis nicht gelesen werden kann.
func getFileInfos(dirPath, basePath string) ([]FileInfo, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var fileInfos []FileInfo
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".") { // Versteckte Dateien überspringen
			continue
		}

		// Füge die Dateiinformationen hinzu
		fileInfos = append(fileInfos, FileInfo{
			Name:  file.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), file.Name()),
			IsDir: file.IsDir(),
		})
	}

	return fileInfos, nil
}

// zipAndServe erstellt ein ZIP-Archiv des angegebenen Verzeichnisses und sendet es an den Client.
// Die Funktion nutzt mehrere Worker-Goroutinen, um die Dateien parallel zu komprimieren, und sendet das
// komprimierte Archiv in Echtzeit an den Client, um Speicher zu sparen.
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

	// Mutex für den sicheren Zugriff auf zipWriter
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

func handleRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		fileShareHandler(w, r)
	default:
		w.Header().Set("Allow", "GET")
		logError(w, r, fmt.Errorf("nicht unterstützte Methode: %s", r.Method))
	}
}

// startServer startet den HTTP-Server auf dem angegebenen Port.
func startServer(port int) {
	http.HandleFunc("/", handleRequest)
	logInfo(fmt.Sprintf("Server läuft unter http://localhost:%d", port))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		logError(nil, nil, fmt.Errorf("fehler beim Starten des Servers: %v", err))
	}
}

func main() {
	// Lade die Konfiguration und starte den Server.
	config, err := loadConfig()
	if err != nil {
		logError(nil, nil, fmt.Errorf("fehler: %v", err))
		return
	}
	startServer(config.Port)
}
