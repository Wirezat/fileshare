package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	configPath   = "./data.json"
	templatePath = "template.html"
)

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

// loadConfig lädt die Konfiguration aus der JSON-Datei.
func loadConfig() (JsonData, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return JsonData{}, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var jsonData JsonData
	if err := json.NewDecoder(file).Decode(&jsonData); err != nil {
		return JsonData{}, fmt.Errorf("failed to decode config: %w", err)
	}

	// Entferne nicht existierende Pfade
	for name, path := range jsonData.Files {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(jsonData.Files, name)
		}
	}
	return jsonData, nil
}

// handleError behandelt Fehler und sendet eine HTTP-Fehlermeldung.
func handleError(w http.ResponseWriter, err error, statusCode int) {
	log.Printf("Error: %v", err)
	http.Error(w, http.StatusText(statusCode), statusCode)
}

// fileShareHandler verarbeitet Anfragen für Dateien und Verzeichnisse.
func fileShareHandler(w http.ResponseWriter, r *http.Request) {
	jsonData, err := loadConfig()
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
		return
	}

	subdomains := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(subdomains) == 0 {
		http.NotFound(w, r)
		return
	}

	filePath, ok := jsonData.Files[subdomains[0]]
	if !ok {
		http.NotFound(w, r)
		return
	}

	remainingPath := filepath.Join(filePath, filepath.Join(subdomains[1:]...))
	fileInfo, err := os.Stat(remainingPath)
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
		return
	}

	if fileInfo.IsDir() {
		serveDirectory(w, remainingPath, subdomains[0], filePath, r)
	} else {
		http.ServeFile(w, r, remainingPath)
	}
}

// serveDirectory zeigt den Inhalt eines Verzeichnisses an oder startet einen ZIP-Download.
func serveDirectory(w http.ResponseWriter, dirPath, subdomain, basePath string, r *http.Request) {
	if r.URL.Query().Get("download") == "zip" {
		zipAndServe(w, dirPath)
		return
	}

	files, err := os.ReadDir(dirPath)
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
		return
	}

	fileInfos := make([]FileInfo, 0, len(files))
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

	parentDir := "/"
	if dirPath != basePath {
		parentDir = filepath.Join("/", strings.TrimPrefix(filepath.Dir(dirPath), basePath))
	}

	t, err := template.New("directory").Funcs(template.FuncMap{
		"getFileExtension": func(filename string) string {
			return strings.ToLower(filepath.Ext(filename))
		},
	}).ParseFiles(templatePath)
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
		return
	}

	if err := t.Execute(w, PageData{
		Subdomain:    subdomain,
		DirPath:      filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:        fileInfos,
		ParentDir:    parentDir,
		HasParentDir: dirPath != basePath,
	}); err != nil {
		handleError(w, err, http.StatusInternalServerError)
	}
}

// zipAndServe erstellt ein ZIP-Archiv des angegebenen Verzeichnisses und sendet es an den Client.
func zipAndServe(w http.ResponseWriter, dirPath string) {
	_, folderName := filepath.Split(filepath.Clean(dirPath))
	zipFileName := folderName + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFileName))

	zipWriter := zip.NewWriter(w)
	defer func() {
		if err := zipWriter.Close(); err != nil {
			log.Printf("Failed to close zip writer: %v", err)
		}
	}()

	if err := addFilesToZip(zipWriter, dirPath, dirPath); err != nil {
		handleError(w, err, http.StatusInternalServerError)
		return
	}

	log.Println("ZIP-Vorgang abgeschlossen")
}

// addFilesToZip fügt Dateien und Unterverzeichnisse rekursiv zum ZIP-Archiv hinzu.
func addFilesToZip(zipWriter *zip.Writer, basePath, currentPath string) error {
	return filepath.Walk(currentPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking directory: %w", err)
		}

		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		if relPath == "." {
			return nil
		}

		if info.IsDir() {
			_, err := zipWriter.Create(relPath + "/")
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		zipFile, err := zipWriter.Create(relPath)
		if err != nil {
			return fmt.Errorf("failed to create file in zip: %w", err)
		}

		_, err = io.Copy(zipFile, file)
		return err
	})
}

// startServer startet den HTTP-Server.
func startServer(port int) {
	http.HandleFunc("/", fileShareHandler)
	log.Printf("Serving at http://localhost:%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

func main() {
	jsonData, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	startServer(jsonData.Port)
}
