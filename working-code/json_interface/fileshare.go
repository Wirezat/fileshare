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

type Sharedata struct {
	Path       string
	UploadTime int64
	Uses       int
	Expiration int64
}

// Die JsonData Struktur mit einer neuen Files Map
type JsonData struct {
	Port  int                  `json:"port"`
	Files map[string]Sharedata `json:"files"` // Map von Subpath zu Sharedata
}

var instructionPath string = "/opt/fileshare/data.json"
var random_subpath_length int = 12

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

func add(path string, subpath string, filePath string, uses int, expiration int64) {
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
		add(instructionPath, newSubpath, shareData.Path, shareData.Uses, shareData.Expiration)
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
	const (
		green     = "\033[32m"
		yellow    = "\033[33m"
		cyan      = "\033[36m"
		reset     = "\033[0m"
		magenta   = "\033[35m"
		underline = "\033[4m"
	)

	helpText := map[string]string{
		"list": fmt.Sprintf("%s%sLIST COMMAND%s\n%s----------------------%s\n%slist, l%s\n    → %sDisplays all shared files along with their assigned subpaths.%s",
			cyan, underline, reset, cyan, reset, green, reset, reset, reset),

		"add": fmt.Sprintf("%s%sADD COMMAND%s\n%s----------------------%s\n%sadd%s -subpath=<subpath> -file=<file> [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]%s\n    → %sCreates a new share for the specified file under the given subpath.%s\n      (%ssubpath%s = desired share name, %sfile%s = path to the file on the system, %suse-expiration%s = max uses, %stime-expiration%s = time limit)",
			cyan, underline, reset, cyan, reset, green, yellow, reset, reset, reset, yellow, reset, yellow, reset, yellow, reset, yellow, reset),

		"addrandom": fmt.Sprintf("%s%sADDRANDOM COMMAND%s\n%s----------------------------%s\n%saddrandom, random, add_random, addr%s -file=<file> [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]%s\n    → %sCreates a new share for the specified file with a randomly generated subpath.%s\n      (%sfile%s = path to the file on the system, %suse-expiration%s = max uses, %stime-expiration%s = time limit)",
			cyan, underline, reset, cyan, reset, green, yellow, reset, reset, reset, yellow, reset, yellow, reset, yellow, reset),

		"delete": fmt.Sprintf("%s%sDELETE COMMAND%s\n%s----------------------%s\n%sdelete, del, remove, rm%s -subpath=<subpath>%s\n    → %sRemoves an existing share.%s\n      (%ssubpath%s = existing share name)",
			cyan, underline, reset, cyan, reset, green, yellow, reset, reset, reset, yellow, reset),

		"edit": fmt.Sprintf("%s%sEDIT COMMAND%s\n%s----------------------%s\n%sedit%s -subpath=<old_subpath> -file=<new_subpath>%s\n    → %sChanges the subpath of an existing share.%s\n      (%sold_subpath%s = current share name, %snew_subpath%s = new share name)",
			cyan, underline, reset, cyan, reset, green, yellow, reset, reset, reset, yellow, reset, yellow, reset),
	}

	if command == "" || helpText[command] == "" {
		fmt.Printf("%s╔══════════════════════════════╗%s\n", magenta, reset)
		fmt.Printf("%s║    %sAVAILABLE COMMANDS%s        ║%s\n", magenta, green, magenta, reset)
		fmt.Printf("%s╚══════════════════════════════╝%s\n", magenta, reset)
		for _, text := range helpText {
			fmt.Printf("\n%s\n", text)
		}
		return
	}

	fmt.Println(helpText[command])
}

func main() {
	if len(os.Args) < 2 {
		printTooltips("")
		return
	}
	old_subpath := flag.String("old_subpath", "", "The subpath (share name), that was used before edit")
	new_subpath := flag.String("new_subpath", "", "The subpath (share name) to be used after edit")
	subpath := flag.String("subpath", "", "The subpath (share name) to be used")
	file := flag.String("file", "", "The path to the file to be used")
	uses := flag.Int("use-expiration", -1, "Optional: amount of times a link can be used (note: not useful for sharing folders, since they make a request for every file opened)")
	duration := flag.String("time-expiration", "", "Optional: amount of time, a link can be used <xx><h/d/w/m/y>")
	flag.CommandLine.Parse(os.Args[2:])
	expiration, err := calculateExpirationTime(*duration)
	if err != nil {
		fmt.Println("Error calculating expiration:", err)
		return
	}

	switch os.Args[1] {
	case "list", "l":
		fmt.Println("Listing all file paths and their domains...")
		list(instructionPath)
		fmt.Println("Done.")

	case "delete", "del", "remove", "rm":
		if *subpath == "" {
			printTooltips("delete")
			return
		}
		fmt.Printf("Deleting share %s...\n", *subpath)
		del(instructionPath, *subpath)
		fmt.Println("Done.")

	case "add":
		if *subpath == "" || *file == "" {
			printTooltips("add")
			return
		}
		absPath, _ := filepath.Abs(*file)
		fmt.Printf("Adding share %s with file path %s...\n", *subpath, absPath)
		add(instructionPath, *subpath, absPath, *uses, expiration)
		fmt.Println("Done.")

	case "addrandom", "random", "add_random", "addr":
		if *file == "" {
			printTooltips("addrandom")
			return
		}
		absPath, _ := filepath.Abs(*file)
		randomSubpath := generateRandomSubpath(random_subpath_length)
		fmt.Printf("Random subpath: %s\n", randomSubpath)
		fmt.Printf("Adding share %s with file path %s...\n", randomSubpath, absPath)
		add(instructionPath, randomSubpath, absPath, *uses, expiration)
		fmt.Println("Done.")

	case "edit":
		if *old_subpath == "" || *new_subpath == "" {
			printTooltips("edit")
			return
		}
		fmt.Printf("Editing: changing %s to %s...\n", *old_subpath, *new_subpath)
		edit(*old_subpath, *new_subpath)

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printTooltips("")
	}
}
