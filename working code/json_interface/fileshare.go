package main

import (
    "encoding/json"
    "fmt"
    "math/rand"
    "os"
    "time"
)

var instructionPath string = "/opt/fileshare/data.json"
var random_subdomain_length int = 12

type JsonData struct {
    Port  int              `json:"port"`
    Files map[string]string `json:"files"`
}

func getRandomASCII() rune {
    // ASCII-Zeichen von 32 (Space) bis 126 (Tilde)
    return rune(rand.Intn(95) + 32) // 95 = 126 - 32 + 1
}

func generateRandomSubdomain(length int) string {
    subdomain := ""

    for i := 0; i < length; i++ {
        randomChar := getRandomASCII()
        subdomain += string(randomChar)
    }

    return subdomain
}

func loadJsonData(filePath string, target interface{}) error {
    file, err := os.Open(filePath)
    if err != nil {
        return fmt.Errorf("error opening file '%s': %v", filePath, err)
    }
    defer file.Close()

    // Decode the JSON data
    decoder := json.NewDecoder(file)
    err = decoder.Decode(target)
    if err != nil {
        return fmt.Errorf("error decoding JSON data from '%s': %v", filePath, err)
    }

    // Ensure the Files map is initialized
    if jsonData, ok := target.(*JsonData); ok {
        if jsonData.Files == nil {
            jsonData.Files = make(map[string]string)
        }
    }

    return nil
}


func writeJsonData(filePath string, target interface{}) error {
    // Open the file for writing, creating it if it doesn't exist
    file, err := os.Create(filePath)
    if err != nil {
        return fmt.Errorf("error creating file: %v", err)
    }
    defer file.Close()

    // Encode the JSON data and write to the file
    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "    ")
    err = encoder.Encode(target)
    if err != nil {
        return fmt.Errorf("error encoding JSON: %v", err)
    }

    return nil
}

func list(path string) {
    var jsonData JsonData
    err := loadJsonData(path, &jsonData)
    if err != nil {
        fmt.Println(err)
        return
    }

    for name, path := range jsonData.Files {
        if _, err := os.Stat(path); os.IsNotExist(err) {
            fmt.Printf("Warning: File %s does not exist at path %s\n", name, path)
        } else {
            fmt.Printf("Filepath: %s; Subdomain: %s\n", path, name)
        }
    }
}

func del(path string, subdomain string) {
    var jsonData JsonData
    err := loadJsonData(path, &jsonData)
    if err != nil {
        fmt.Println(err)
        return
    }

    // Check if the subdomain exists
    if _, exists := jsonData.Files[subdomain]; exists {
        fmt.Printf("Deleting link %s to file: %s\n", subdomain, jsonData.Files[subdomain])
        delete(jsonData.Files, subdomain)

        // Write the updated data back to the file
        err := writeJsonData(path, &jsonData)
        if err != nil {
            fmt.Println(err)
            return
        }
        fmt.Println("Subdomain deleted successfully.")
    } else {
        fmt.Printf("Subdomain %s not found.\n", subdomain)
    }
}

func add(path string, subdomain string, filePath string) {
    var jsonData JsonData
    err := loadJsonData(path, &jsonData)
    if err != nil {
        fmt.Println(err)
        return
    }

    // Check if the subdomain already exists
    if _, exists := jsonData.Files[subdomain]; exists {
        fmt.Printf("Subdomain %s already exists.\n", subdomain)
        return
    }

    if _, err := os.Stat(filePath); os.IsNotExist(err) {
        fmt.Printf("Warning: File path %s does not exist.\n", filePath)
        return
    }

    // Add the new subdomain and file path
    jsonData.Files[subdomain] = filePath

    // Write the updated data back to the file
    err = writeJsonData(path, &jsonData)
    if err != nil {
        fmt.Println(err)
        return
    }
    fmt.Printf("Added subdomain %s with file path %s successfully.\n", subdomain, filePath)
}

// Entry point: Function to handle command line arguments
func main() {
    rand.Seed(time.Now().UnixNano())

    // Überprüfen, ob der Befehl angegeben ist
    if len(os.Args) < 2 {
        fmt.Println("Usage: <command> [<name>] [<filepath>]")
        return
    }

    var tool string
    var name string
    var filepath string

    // Der erste Parameter ist der Befehl
    tool = os.Args[1]

    // Überprüfen, ob ein Name angegeben wurde
    if len(os.Args) > 2 {
        name = os.Args[2]
    }

    // Überprüfen, ob ein Dateipfad angegeben wurde
    if len(os.Args) > 3 {
        filepath = os.Args[3]
    }

    // Switch-Anweisung für die verschiedenen Befehle
    switch tool {
    case "list":
        fmt.Println("Listing all file paths and their domains...")
        list(instructionPath)
        fmt.Println("Done.")
    case "delete", "del":
        if name == "" {
            fmt.Println("Error: Missing name for delete command.")
            return
        }
        fmt.Printf("Deleting %s...\n", name)
        del(instructionPath, name)
        fmt.Println("Done.")
    case "add":
        if name == "" || filepath == "" {
            fmt.Println("Error: Missing name or filepath for add command.")
            return
        }
        fmt.Printf("Adding %s with file path %s...\n", name, filepath)
        add(instructionPath, name, filepath)
        fmt.Println("Done.")
    case "addrandom", "random", "add_random":
        name = generateRandomSubdomain(random_subdomain_length)
        fmt.Printf("Random subdomain: %s\n", name)
        fmt.Printf("Adding %s with file path %s...\n", name, filepath)
        add(instructionPath, name, filepath)
        fmt.Println("Done.")
    default:
        fmt.Printf("Unknown command: %s\n", tool)
    }
}

