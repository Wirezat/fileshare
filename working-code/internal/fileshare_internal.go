package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var instructionpath string = "./data.json"

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

// Zentrale, kompakte Fehlerbehandlung
func handleError(response http.ResponseWriter, err error, statusCode int) {
	if err != nil {
		http.Error(response, http.StatusText(statusCode), statusCode)
	}
}

func loadConfig() (JsonData, error) {
	file, err := os.Open(instructionpath)
	if err != nil {
		return JsonData{}, err
	}
	defer file.Close()

	var jsonData JsonData
	if err := json.NewDecoder(file).Decode(&jsonData); err != nil {
		return jsonData, err
	}

	for name, path := range jsonData.Files {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(jsonData.Files, name)
		}
	}
	return jsonData, nil
}

func fileShareHandler(response http.ResponseWriter, request *http.Request) {
	// Konfiguration laden
	jsonData, err := loadConfig()
	if err != nil {
		handleError(response, err, http.StatusInternalServerError)
		return
	}

	// Extrahiere den Pfad und entferne führenden Slash
	subdomains := strings.Split(strings.TrimPrefix(request.URL.Path, "/"), "/")

	// Holen der Datei/Verzeichnis-Info aus der fileMap
	if filePath, ok := jsonData.Files[subdomains[0]]; ok {
		remainingPath := filepath.Join(filePath, filepath.Join(subdomains[1:]...))

		// Verarbeite Datei oder Verzeichnis
		if fileInfo, err := os.Stat(remainingPath); err == nil {
			if fileInfo.IsDir() {
				serveDirectory(response, remainingPath, subdomains[0], filePath)
			} else {
				http.ServeFile(response, request, remainingPath)
			}
		} else {
			handleError(response, err, http.StatusInternalServerError)
		}
	} else {
		http.NotFound(response, request)
	}
}

func serveDirectory(response http.ResponseWriter, dirPath, subdomain, basePath string) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		handleError(response, err, http.StatusInternalServerError)
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
	}).ParseFiles("template.html")
	if err != nil {
		handleError(response, err, http.StatusInternalServerError)
		return
	}

	if err := t.Execute(response, PageData{
		Subdomain:    subdomain,
		DirPath:      filepath.Join("/", strings.TrimPrefix(dirPath, basePath)),
		Files:        fileInfos,
		ParentDir:    parentDir,
		HasParentDir: dirPath != basePath,
	}); err != nil {
		handleError(response, err, http.StatusInternalServerError)
	}
}

func starthttp(port int) {
	http.HandleFunc("/", fileShareHandler)
	fmt.Printf("Serving at http://localhost:%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func main() {
	if jsonData, err := loadConfig(); err != nil {
		fmt.Println(err)
		return
	} else {
		starthttp(jsonData.Port)
	}
}
