package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
}

// JsonData enthält die Konfigurationseinstellungen aus der JSON-Datei.
type JsonData struct {
	Port  int                 `json:"port"`  // Port, auf dem der Server läuft
	Files map[string]FileData `json:"files"` // Zuordnungen von Subpfaden zu Dateimetadaten
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

// handleError behandelt Fehler, indem eine HTTP-Fehlermeldung gesendet wird.
func handleError(w http.ResponseWriter, err error, statusCode int) {
	http.Error(w, http.StatusText(statusCode), statusCode)
}

// fileShareHandler verarbeitet Anfragen für Dateien und Verzeichnisse.
// Hier wird der erste Pfadabschnitt als Subpfad interpretiert, der zur Auswahl der entsprechenden Dateimetadaten genutzt wird.
func fileShareHandler(w http.ResponseWriter, r *http.Request) {
	config, err := loadConfig()
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
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

	// Erstelle den vollständigen Pfad zur angeforderten Datei oder Verzeichnis
	remainingPath := filepath.Join(FileData.Path, filepath.Join(pathParts[1:]...))
	fileInfo, err := os.Stat(remainingPath)
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
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
		handleError(w, err, http.StatusInternalServerError)
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
		handleError(w, err, http.StatusInternalServerError)
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
		handleError(w, err, http.StatusInternalServerError)
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
func zipAndServe(w http.ResponseWriter, dirPath string) {
	_, folderName := filepath.Split(filepath.Clean(dirPath))
	zipFileName := folderName + ".zip"

	// Setze die Header für den ZIP-Download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFileName))

	// Erstelle einen neuen ZIP-Writer
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	// Durchlaufe das Verzeichnis und füge Dateien und Unterverzeichnisse zum ZIP hinzu
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil || relPath == "." {
			return err
		}

		// Füge Unterverzeichnisse zum ZIP hinzu
		if info.IsDir() {
			_, err := zipWriter.Create(relPath + "/")
			return err
		}

		// Füge Dateien zum ZIP hinzu
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		zipFile, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		_, err = io.Copy(zipFile, file)
		return err
	})
	if err != nil {
		handleError(w, err, http.StatusInternalServerError)
	}
}

// startServer startet den HTTP-Server auf dem angegebenen Port.
func startServer(port int) {
	http.HandleFunc("/", fileShareHandler)
	fmt.Printf("Server läuft unter http://localhost:%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		fmt.Printf("Fehler beim Starten des Servers: %v\n", err)
	}
}

func main() {
	// Lade die Konfiguration und starte den Server.
	config, err := loadConfig()
	if err != nil {
		fmt.Println("Fehler:", err)
		return
	}
	startServer(config.Port)
}
