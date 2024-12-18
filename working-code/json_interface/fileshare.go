package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
)

var instructionPath string = "/opt/fileshare/data.json"
var random_subdomain_length int = 12

type Sharedata struct {
	Name string
	Path string
}

type JsonData struct {
	Port  int               `json:"port"`
	Files map[string]string `json:"files"`
}

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
			jsonData.Files = make(map[string]string)
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

	for name, path := range jsonData.Files {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("Filepath: %s; Subdomain: %s\n", path, name)
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

func add(path string, subdomain string, filepath string) {
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

	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		fmt.Printf("Warning: File path %s does not exist.\n", filepath)
		return
	}

	// Add the new subdomain and file path
	jsonData.Files[subdomain] = filepath

	// Write the updated data back to the file
	err = writeJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Added subdomain %s with file path %s successfully.\n", subdomain, filepath)
}

func edit(subdomain string, newSubdomain string) {
	getTuple(subdomain)
	oldSubdomain, path, err := getTuple(subdomain)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Printf("Match found: Name: %s, Path: %s\n", oldSubdomain, path)
		fmt.Printf("now changing: Old Subdomain %s to new subdomain %s\n", oldSubdomain, newSubdomain)
	}
	del(instructionPath, oldSubdomain)
	add(instructionPath, newSubdomain, path)

}
func isParamGiven(param string) {
	if param == "" {
		fmt.Println("Error: Missing parameters for command.")
		fmt.Println("Usage: <command> [<name>] [<filepath>]")
		os.Exit(1)
	}
}
func getTuple(searchTerm string) (string, string, error) {
	var jsonData JsonData
	err := loadJsonData(instructionPath, &jsonData)
	if err != nil {
		return "", "", err
	}

	for name, path := range jsonData.Files {
		if name == searchTerm || path == searchTerm {
			return name, path, nil
		}
	}
	return "", "", fmt.Errorf("no match found for search term: %s", searchTerm)
}

func printTooltips() {
	fmt.Println("Usage: <command> [<name>] [<filepath>]")
	fmt.Println("Commands:\n" +
		" list                   -> lists all shared files with their path and domains\n" +
		" add <subdomain> <filepath> -> shares a file under given subdomain\n" +
		" addrandom <filepath>   -> shares a file under a random automatically generated subdomain\n" +
		" delete <subdomain>     -> stops sharing the file connected to the subdomain\n" +
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
	var filepath string

	// Der erste Parameter ist der Befehl
	// Zweiter Parameter als Name der Subdomain
	// Dritter Parameter als Dateipfad der Datei
	args := append(os.Args[1:], "", "") // Zwei leere Strings anhängen
	tool, subdomain, filepath = args[0], args[1], args[2]

	switch tool {
	case "list":
		fmt.Println("Listing all file paths and their domains...")
		list(instructionPath)
		fmt.Println("Done.")

	case "delete", "del":
		isParamGiven(subdomain)
		fmt.Printf("Deleting %s...\n", subdomain)
		del(instructionPath, subdomain)
		fmt.Println("Done.")

	case "add":
		isParamGiven(subdomain)
		isParamGiven(filepath)
		fmt.Printf("Adding %s with file path %s...\n", subdomain, filepath)
		add(instructionPath, subdomain, filepath)
		fmt.Println("Done.")

	case "addrandom", "random", "add_random":
		//subdomain hier als Dateipfad, um Argumente besser zu nutzen
		var filepathForRandom = subdomain
		var rSubdomain string = ""
		rSubdomain = generateRandomSubdomain(random_subdomain_length)
		fmt.Printf("Random subdomain: %s\n", rSubdomain)
		fmt.Printf("Adding %s with file path %s...\n", rSubdomain, filepathForRandom)
		add(instructionPath, rSubdomain, filepathForRandom)
		fmt.Println("Done.")

	case "edit":
		//filepath hier als neue subdomain um Argumente besser zu machen
		var newSubdomain string = filepath
		isParamGiven(subdomain)
		isParamGiven(filepath)
		edit(subdomain, newSubdomain)

	default:
		fmt.Printf("Unknown command: %s\n", tool)
		printTooltips()
	}
}
