package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "sync"
)

var mu sync.RWMutex
var instructionpath string = "./data.json"
var fileMap map[string]string

type JsonData struct {
    Port  int              `json:"port"`
    Files map[string]string `json:"files"`
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
            fmt.Printf("Warning: File %s does not exist at path: %s\n", name, path)
            delete(jsonData.Files, name)
        }
    }
    return jsonData, nil
}

func fileShareHandler(response http.ResponseWriter, request *http.Request) {
    name := request.URL.Path[1:]

    mu.RLock()
    filePath, ok := fileMap[name]
    mu.RUnlock()

    if !ok {
        // Versuche die Konfiguration neu zu laden, wenn die Datei nicht gefunden wird
        newConfig, err := loadConfig()
        if err == nil {
            mu.Lock()
            fileMap = newConfig.Files
            mu.Unlock()

            mu.RLock()
            filePath, ok = fileMap[name]
            mu.RUnlock()
        }

        if !ok {
            http.NotFound(response, request)
            return
        }
    }

    http.ServeFile(response, request, filePath)
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
