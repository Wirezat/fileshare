package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

// Neue Struktur für die Datei mit 'path' (keine 'data' mehr)
type Sharedata struct {
	Path string `json:"path"`
}

// Die JsonData Struktur mit einer neuen Files Map
type JsonData struct {
	Port  int                  `json:"port"`
	Files map[string]Sharedata `json:"files"` // Map von Subdomain zu Sharedata
}

var instructionPath string = "/opt/fileshare/data.json"
var random_subdomain_length int = 12

func generateRandomChar() rune {
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-"
	return rune(validChars[rand.Intn(len(validChars))])
}

func generateRandomSubdomain(length int) string {
	subdomain := ""
	for i := 0; i < length; i++ {
		subdomain += string(generateRandomChar())
	}
	return subdomain
}

func loadJsonData(filepath string, target interface{}) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("error opening file '%s': %v", filepath, err)
	}
	defer file.Close()

	// Decode the JSON data
	decoder := json.NewDecoder(file)
	err = decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("error decoding JSON data from '%s': %v", filepath, err)
	}

	// Ensure the Files map is initialized
	if jsonData, ok := target.(*JsonData); ok {
		if jsonData.Files == nil {
			jsonData.Files = make(map[string]Sharedata)
		}
	}

	return nil
}

func writeJsonData(filepath string, target interface{}) error {
	// Open the file for writing, creating it if it doesn't exist
	file, err := os.Create(filepath)
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

	for name, shareData := range jsonData.Files {
		if _, err := os.Stat(shareData.Path); os.IsNotExist(err) {
			fmt.Printf("Filepath: %s; Subdomain: %s\n", shareData.Path, name)
			fmt.Printf("Warning: File %s does not exist at path %s\n", name, shareData.Path)
		} else {
			fmt.Printf("Filepath: %s; Subdomain: %s\n", shareData.Path, name)
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
		fmt.Printf("Deleting link %s to file: %s\n", subdomain, jsonData.Files[subdomain].Path)
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
	jsonData.Files[subdomain] = Sharedata{
		Path: filePath,
	}

	// Write the updated data back to the file
	err = writeJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Added subdomain %s with file path %s successfully.\n", subdomain, filePath)
}

func edit(subdomain string, newSubdomain string) {
	oldSubdomain, shareData, err := getTuple(subdomain)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Printf("Match found: Subdomain: %s, Path: %s\n", oldSubdomain, shareData.Path)
		fmt.Printf("Now changing: Old Subdomain %s to new subdomain %s\n", oldSubdomain, newSubdomain)
	}
	del(instructionPath, oldSubdomain)
	add(instructionPath, newSubdomain, shareData.Path)
}

func getTuple(searchTerm string) (string, Sharedata, error) {
	var jsonData JsonData
	err := loadJsonData(instructionPath, &jsonData)
	if err != nil {
		return "", Sharedata{}, err
	}

	for name, shareData := range jsonData.Files {
		if name == searchTerm || shareData.Path == searchTerm {
			return name, shareData, nil
		}
	}
	return "", Sharedata{}, fmt.Errorf("no match found for search term: %s", searchTerm)
}

func isParamGiven(param string) {
	if param == "" {
		fmt.Println("Error: Missing parameters for command.")
		fmt.Println("Usage: <command> [<name>] [<filepath>]")
		os.Exit(1)
	}
}

func printTooltips() {
	fmt.Println("Usage: <command> [<name>] [<filepath>]")
	fmt.Println("Commands:\n" +
		" list                   -> lists all shared files with their path and domains\n" +
		" 	alternative inputs: l\n" +
		" add <subdomain> <filepath> -> shares a file under given subdomain\n" +
		"\n" +
		" addrandom <filepath>   -> shares a file under a random automatically generated subdomain\n" +
		" 	alternative inputs:, random, add_random, addr\n" +
		" delete <subdomain>     -> stops sharing the file connected to the subdomain\n" +
		" 	alternative inputs: del, remove, rm\n" +
		"\n" +
		" edit <old subdomain> <new subdomain> -> changes the subdomain of an already shared file")
}

func main() {
	// Überprüfen, ob der Befehl angegeben ist
	if len(os.Args) < 2 {
		printTooltips()
		return
	}

	var tool string
	var subdomain string
	var givenPath string
	// Der erste Parameter ist der Befehl
	// Zweiter Parameter als Name der Subdomain
	// Dritter Parameter als Dateipfad der Datei
	args := append(os.Args[1:], "", "")
	tool, subdomain, givenPath = args[0], args[1], args[2]

	switch tool {
	case "list", "l":
		fmt.Println("Listing all file paths and their domains...")
		list(instructionPath)
		fmt.Println("Done.")

	case "delete", "del", "remove", "rm":
		isParamGiven(subdomain)
		fmt.Printf("Deleting %s...\n", subdomain)
		del(instructionPath, subdomain)
		fmt.Println("Done.")

	case "add":
		// Der zu Teilende Pfad wird im Falle eines Relativen Pfades in einen Absoluten konvertiert.
		filepath, _ := filepath.Abs(givenPath)

		isParamGiven(subdomain)
		isParamGiven(filepath)
		fmt.Printf("Adding %s with file path %s...\n", subdomain, filepath)
		add(instructionPath, subdomain, filepath)
		fmt.Println("Done.")

	case "addrandom", "random", "add_random", "addr":
		//subdomain hier als Dateipfad, um Argumente besser zu nutzen
		// Der zu Teilende Pfad wird im Falle eines Relativen Pfades in einen Absoluten konvertiert.
		filepath, _ := filepath.Abs(subdomain)

		var rSubdomain string = ""
		rSubdomain = generateRandomSubdomain(random_subdomain_length)
		fmt.Printf("Random subdomain: %s\n", rSubdomain)
		fmt.Printf("Adding %s with file path %s...\n", rSubdomain, filepath)
		add(instructionPath, rSubdomain, filepath)
		fmt.Println("Done.")

	case "edit":
		//filepath hier als neue subdomain um Argumente besser zu machen
		var newSubdomain string = givenPath
		isParamGiven(subdomain)
		isParamGiven(givenPath)
		edit(subdomain, newSubdomain)

	default:
		fmt.Printf("Unknown command: %s\n", tool)
		printTooltips()
	}
}
