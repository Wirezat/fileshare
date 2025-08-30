package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// #region Structs und Vars
type Sharedata struct {
	Path       string
	UploadTime int64
	Uses       int
	Expiration int64
	AllowPost  bool
}

type JsonData struct {
	Port  int                  `json:"port"`
	Files map[string]Sharedata `json:"files"` // Map von Subpath zu Sharedata
}

var instructionPath string = "/opt/fileshare/data.json"
var randomSubpathLength int = 12

// #endregion

func generateRandomSubpath(length int) string {
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-"
	subpath := ""
	for i := 0; i < length; i++ {
		subpath += string(validChars[rand.Intn(len(validChars))])
	}
	return subpath
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
	const (
		green     = "\033[32m"
		red       = "\033[31m"
		yellow    = "\033[33m"
		blue      = "\033[34m"
		cyan      = "\033[36m"
		magenta   = "\033[35m"
		reset     = "\033[0m"
		underline = "\033[4m"
	)

	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Ausgabe der Liste mit Formatierung
	fmt.Printf("%s%sLIST OF SHARED FILES%s\n%s----------------------%s\n", cyan, underline, reset, cyan, reset)

	for name, shareData := range jsonData.Files {
		// Datei existiert?
		if _, err := os.Stat(shareData.Path); os.IsNotExist(err) {
			// Datei nicht gefunden
			fmt.Printf("%sSubpath:%s %s%s\n", yellow, reset, name, reset)
			fmt.Printf("%sFilepath:%s %s%s\n", red, reset, shareData.Path, reset)
			fmt.Printf("%sWarning: File %s does not exist at path %s%s\n", red, name, shareData.Path, reset)
		} else {
			// Datei gefunden
			fmt.Printf("%sSubpath:%s %s%s\n", green, reset, name, reset)
			fmt.Printf("%sFilepath:%s %s%s\n", blue, reset, shareData.Path, reset)
		}

		// Ausgabe der ShareData in strukturiertem Format
		fmt.Printf("%s  UploadTime:%s %d\n", cyan, reset, shareData.UploadTime)
		fmt.Printf("%s  Uses:%s %d\n", cyan, reset, shareData.Uses)
		fmt.Printf("%s  Expiration:%s %d\n", cyan, reset, shareData.Expiration)
		fmt.Println()
	}
}

func del(path string, subpath string) {
	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Check if the subpath exists
	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Deleting link %s to file: %s\n", subpath, jsonData.Files[subpath].Path)
		delete(jsonData.Files, subpath)

		// Write the updated data back to the file
		err := writeJsonData(path, &jsonData)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Subpath deleted successfully.")
	} else {
		fmt.Printf("Subpath %s not found.\n", subpath)
	}
}

func add(path string, subpath string, filePath string, uses int, expiration int64, allowPost bool) {
	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Check if the subpath already exists
	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Subpath %s already exists.\n", subpath)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Warning: File path %s does not exist.\n", filePath)
		return
	}

	// Add the new subpath and file path
	jsonData.Files[subpath] = Sharedata{
		Path:       filePath,
		UploadTime: time.Now().Unix(),
		Uses:       uses,
		Expiration: expiration,
		AllowPost:  allowPost,
	}

	// Write the updated data back to the file
	err = writeJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Added subpath %s with file path %s successfully.\n", subpath, filePath)
}

func edit(subpath, newSubpath string) {
	var jsonData JsonData
	if err := loadJsonData(instructionPath, &jsonData); err != nil {
		fmt.Println(err)
		return
	}
	if shareData, ok := jsonData.Files[subpath]; ok {
		fmt.Printf("Match found: Subpath: %s, Path: %s\nNow changing: Old Subpath %s to new subpath %s\n", subpath, shareData.Path, subpath, newSubpath)
		add(instructionPath, newSubpath, shareData.Path, shareData.Uses, shareData.Expiration, shareData.AllowPost)
		del(instructionPath, subpath)
		fmt.Println("Successfully changed subpath.")
	} else {
		fmt.Printf("No match found for subpath: %s\n", subpath)
	}
}

func calculateExpirationTime(duration string) (int64, error) {
	if duration == "" {
		return 0, nil
	}

	num, err := strconv.Atoi(strings.TrimRight(duration, "hdwmy"))
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %v", err)
	}

	unit := duration[len(duration)-1:]
	now := time.Now()

	switch unit {
	case "h":
		return now.Add(time.Duration(num) * time.Hour).Unix(), nil
	case "d":
		return now.AddDate(0, 0, num).Unix(), nil
	case "w":
		return now.AddDate(0, 0, num*7).Unix(), nil
	case "m":
		return now.AddDate(0, num, 0).Unix(), nil
	case "y":
		return now.AddDate(num, 0, 0).Unix(), nil
	default:
		return 0, fmt.Errorf("invalid unit in duration: %s", unit)
	}
}

func printTooltips(command string) {
	// Define a map for each command's tooltip description
	helpText := map[string]string{
		"list": `NAME
       list - Displays all shared files and their assigned subpaths

SYNOPSIS
       list, l`,

		"add": `NAME
       add - Creates a new share for the specified file under the given subpath

SYNOPSIS
       add -subpath, -s, --subpath=<subpath> -file, -f, --file=<file>
           [-use-expiration, -u=<num>] [-time-expiration, -t=<xxh/d/w/m/y>]
           [-allow-post, -p]

       subpath  The desired share name.
       file     The path to the file on the system.

OPTIONS
       -subpath, -s, --subpath=<subpath>
               Desired share name.
       -file, -f, --file=<file>
               Path to the file on the system.
       -use-expiration, -u=<num>
               Max uses.
       -time-expiration, -t=<xxh/d/w/m/y>
               Time limit.
       -allow-post, -p
               Allow POST requests.`,

		"addrandom": `NAME
       addrandom - Creates a new share for the specified file with a randomly
       generated subpath

SYNOPSIS
       addrandom, random, add_random, addr -file, -f, --file=<file>
           [-use-expiration, -u=<num>] [-time-expiration, -t=<xxh/d/w/m/y>]
           [-allow-post, -p]

       file     The path to the file on the system.

OPTIONS
       -file, -f, --file=<file>
               Path to the file on the system.
       -use-expiration, -u=<num>
               Max uses.
       -time-expiration, -t=<xxh/d/w/m/y>
               Time limit.
       -allow-post, -p
               Allow POST requests.`,

		"delete": `NAME
       delete - Removes an existing share

SYNOPSIS
       delete, del, remove, rm -subpath, -s, --subpath=<subpath>

OPTIONS
       -subpath, -s, --subpath=<subpath>
               The share to be deleted.`,

		"edit": `NAME
       edit - Changes the subpath of an existing share

SYNOPSIS
       edit -old_subpath, -old, -o=<old> -new_subpath, -new, -n=<new>

OPTIONS
       -old_subpath, -o=<old>
               Current share name.
       -new_subpath, -n=<new>
               New share name.`,
	}

	// If no specific command is provided, print all available commands
	if command == "" || helpText[command] == "" {
		fmt.Println("╔════════════════════════════╗")
		fmt.Println("║     AVAILABLE COMMANDS     ║")
		fmt.Println("╚════════════════════════════╝")
		fmt.Println()

		// List all commands with their brief description
		for cmd, text := range helpText {
			fmt.Printf("\n• %s\n  %s\n", cmd, text)
		}
		return
	}

	// Print specific command tooltip in man page format
	fmt.Println("╔════════════════════════════")
	fmt.Printf("║   COMMAND: %s\n", command)
	fmt.Println("╚════════════════════════════")
	fmt.Println(helpText[command])
}

func main() {
	// Check if the number of arguments is valid
	if len(os.Args) < 2 {
		printTooltips("")
		return
	}

	// Flag definitions
	oldSubpathFlag := flag.String("old_subpath", "", "The subpath (share name) that was used before editing")
	flag.StringVar(oldSubpathFlag, "old", "", "Alias for --old_subpath")
	flag.StringVar(oldSubpathFlag, "o", "", "Alias for --old_subpath")

	newSubpathFlag := flag.String("new_subpath", "", "The subpath (share name) to be used after editing")
	flag.StringVar(newSubpathFlag, "new", "", "Alias for --new_subpath")
	flag.StringVar(newSubpathFlag, "n", "", "Alias for --new_subpath")

	subpathFlag := flag.String("subpath", "", "The subpath (share name) to be used")
	flag.StringVar(subpathFlag, "s", "", "Alias for --subpath")

	filePathFlag := flag.String("file", "", "The path to the file to be used")
	flag.StringVar(filePathFlag, "f", "", "Alias for --file")

	usageLimitFlag := flag.Int("use-expiration", -1, "Optional: number of times a link can be used")
	flag.IntVar(usageLimitFlag, "uses", -1, "Alias for --use-expiration")
	flag.IntVar(usageLimitFlag, "u", -1, "Alias for --use-expiration")

	expirationTimeFlag := flag.String("time-expiration", "", "Optional: amount of time a link can be used <xx><h/d/w/m/y>")
	flag.StringVar(expirationTimeFlag, "time", "", "Alias for --time-expiration")
	flag.StringVar(expirationTimeFlag, "t", "", "Alias for --time-expiration")

	// Corrected section: Using flag.BoolVar for boolean flags
	allowPostFlag := flag.Bool("allow-post", false, "Optional: allow POST requests. Set to true to allow.")
	flag.BoolVar(allowPostFlag, "post", false, "Alias for --allow-post")
	flag.BoolVar(allowPostFlag, "p", false, "Alias for --allow-post")

	// Parse the command line flags
	flag.CommandLine.Parse(os.Args[2:])

	// Calculate expiration time based on user input
	expirationTime, err := calculateExpirationTime(*expirationTimeFlag)
	if err != nil {
		fmt.Println("Error calculating expiration:", err)
		return
	}

	// Main command handling based on the first argument
	switch os.Args[1] {
	case "list", "l":
		// List all file paths and their domains
		fmt.Println("Listing all file paths and their domains...")
		list(instructionPath)
		fmt.Println("Done.")

	case "delete", "del", "remove", "rm":
		// Delete a specific share based on the subpath
		if *subpathFlag == "" {
			printTooltips("delete")
			return
		}
		fmt.Printf("Deleting share %s...\n", *subpathFlag)
		del(instructionPath, *subpathFlag)
		fmt.Println("Done.")

	case "add":
		// Add a new share with a specified file
		if *subpathFlag == "" || *filePathFlag == "" {
			printTooltips("add")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		fmt.Printf("Adding share %s with file path %s...\n", *subpathFlag, absPath)
		add(instructionPath, *subpathFlag, absPath, *usageLimitFlag, expirationTime, *allowPostFlag) // Dereference the bool flag here
		fmt.Println("Done.")

	case "addrandom", "random", "add_random", "addr":
		// Add a random share with a specified file
		if *filePathFlag == "" {
			printTooltips("addrandom")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		randomSubpath := generateRandomSubpath(randomSubpathLength)
		fmt.Printf("Random subpath: %s\n", randomSubpath)
		fmt.Printf("Adding share %s with file path %s...\n", randomSubpath, absPath)
		add(instructionPath, randomSubpath, absPath, *usageLimitFlag, expirationTime, *allowPostFlag) // Dereference the bool flag here
		fmt.Println("Done.")

	case "edit":
		// Edit an existing share by changing its subpath
		if *oldSubpathFlag == "" || *newSubpathFlag == "" {
			printTooltips("edit")
			return
		}
		fmt.Printf("Editing: changing %s to %s...\n", *oldSubpathFlag, *newSubpathFlag)
		edit(*oldSubpathFlag, *newSubpathFlag)

	default:
		// Unknown command handling
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printTooltips("")
	}
}
