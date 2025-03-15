package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var mu sync.RWMutex
var instructionpath string = "./data.json"
var fileMap map[string]string

type JsonData struct {
	Port  int               `json:"port"`
	Files map[string]string `json:"files"`
}
type FileInfo struct {
	Name  string
	Path  string
	IsDir bool
}

type PageData struct {
	Subdomain    string
	DirPath      string
	Files        []FileInfo
	ParentDir    string
	HasParentDir bool
}

func loadConfig() (JsonData, error) {
	var jsonData JsonData
	file, err := os.Open(instructionpath)
	if err != nil {
		return jsonData, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&jsonData)
	if err != nil {
		return jsonData, fmt.Errorf("error decoding JSON: %v", err)
	}

	for name, path := range jsonData.Files {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("Warning: File or Directory %s does not exist at path: %s\n", name, path)
			delete(jsonData.Files, name)
		}
	}
	return jsonData, nil
}

func fileShareHandler(response http.ResponseWriter, request *http.Request) {
	// Konfiguration bei jedem Aufruf neu laden
	jsonData, err := loadConfig()
	if err != nil {
		fmt.Println("Error loading config:", err)
		http.Error(response, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Aktualisiere die fileMap
	mu.Lock()
	fileMap = jsonData.Files
	mu.Unlock()

	// Extrahiere den Pfad der Anfrage
	path := request.URL.Path

	// Entferne den führenden Slash, falls vorhanden
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	// Teile den Pfad in Teile (Subdomains)
	subdomains := strings.Split(path, "/")

	// Suche nach dem ersten Element in der fileMap
	mu.RLock()
	filePath, ok := fileMap[subdomains[0]]
	mu.RUnlock()

	// Wenn der erste Subdomain-Schlüssel nicht gefunden wurde
	if !ok {
		fmt.Printf("Subdomain %s nicht gefunden\n", subdomains[0])
		http.NotFound(response, request)
		return
	}

	// Der erste Subdomain-Schlüssel wurde gefunden
	fmt.Println("File or directory found:", filePath)

	// Entferne das erste Element der Subdomain-Liste und baue den Rest des Pfads
	remainingPath := filepath.Join(filePath, filepath.Join(subdomains[1:]...))

	// Verarbeite den verbleibenden Pfad
	fileInfo, err := os.Stat(remainingPath)
	if err != nil {
		fmt.Println("Error accessing file or directory:", err)
		http.Error(response, "Error accessing file or directory", http.StatusInternalServerError)
		return
	}

	// Wenn es sich um ein Verzeichnis handelt, den Inhalt des Verzeichnisses auflisten
	if fileInfo.IsDir() {
		fmt.Println("Serving directory:", remainingPath)
		serveDirectory(response, remainingPath, subdomains[0], filePath)
	} else {
		fmt.Println("Serving file:", remainingPath)
		// Wenn es sich um eine Datei handelt, die Datei an den Client senden
		http.ServeFile(response, request, remainingPath)
	}
}

func serveDirectory(response http.ResponseWriter, dirPath, subdomain, basePath string) {
	// Verzeichnisinhalt abrufen
	files, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(response, "Fehler beim Lesen des Verzeichnisses", http.StatusInternalServerError)
		return
	}

	var filteredFiles []os.DirEntry
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") { // Ignoriere Dateien/Ordner, die mit einem Punkt beginnen
			filteredFiles = append(filteredFiles, file)
		}
	}

	// Vorbereiten der Dateiinformationen
	var fileInfos []FileInfo
	for _, file := range filteredFiles {
		fileInfos = append(fileInfos, FileInfo{
			Name:  file.Name(),
			Path:  strings.TrimPrefix(filepath.Join(dirPath, file.Name()), basePath),
			IsDir: file.IsDir(),
		})
	}

	// Berechnung des übergeordneten Verzeichnisses für die Rückwärtsnavigation
	var parentDir string
	var hasParentDir bool
	if dirPath != basePath {
		parentDir = filepath.Dir(dirPath)
		parentDir = strings.TrimPrefix(parentDir, basePath)
		if parentDir == "" {
			parentDir = "/"
		}
		hasParentDir = true
	}

	// PageData für Template füllen
	pageData := PageData{
		Subdomain:    subdomain,
		DirPath:      strings.TrimPrefix(dirPath, basePath), // Nur den relativen Pfad ab basePath anzeigen
		Files:        fileInfos,
		ParentDir:    parentDir,
		HasParentDir: hasParentDir,
	}

	// Template-Funktionen registrieren
	funcMap := template.FuncMap{
		"getFileExtension": func(filename string) string {
			return strings.ToLower(filepath.Ext(filename)) // Dateiendung in Kleinbuchstaben zurückgeben
		},
	}

	// Template parsen
	t, err := template.New("directory").Funcs(funcMap).ParseFiles("template.html")
	if err != nil {
		http.Error(response, "Fehler beim Laden des Templates", http.StatusInternalServerError)
		return
	}

	// Template ausführen und an ResponseWriter senden
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = t.Execute(response, pageData)
	if err != nil {
		fmt.Println("Render-Fehler:", err)
		http.Error(response, "Fehler beim Rendern der Seite", http.StatusInternalServerError)
	}
}

func starthttp(port int) {
	http.HandleFunc("/", fileShareHandler)
	fmt.Printf("Serving at http://localhost:%d\n", port)
	address := fmt.Sprintf(":%d", port)
	if err := http.ListenAndServe(address, nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func main() {
	jsonData, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}

	mu.Lock()
	fileMap = jsonData.Files
	mu.Unlock()

	starthttp(jsonData.Port)
}
