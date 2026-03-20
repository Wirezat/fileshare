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

	"github.com/Wirezat/fileshare/pkg/shared"
)

// #region structs

type JsonData struct {
	Port          int                        `json:"port"`
	AdminPassword string                     `json:"admin_password"`
	Files         map[string]shared.FileData `json:"files"`
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

	decoder := json.NewDecoder(file)
	err = decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("error decoding JSON data from '%s': %v", filepath, err)
	}

	if jsonData, ok := target.(*JsonData); ok {
		if jsonData.Files == nil {
			jsonData.Files = make(map[string]shared.FileData)
		}
	}
	return nil
}

func writeJsonData(filepath string, target interface{}) error {
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

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
		reset     = "\033[0m"
		underline = "\033[4m"
	)

	var jsonData JsonData
	err := loadJsonData(path, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("%s%sLIST OF SHARED FILES%s\n%s----------------------%s\n", cyan, underline, reset, cyan, reset)

	for name, shareData := range jsonData.Files {
		if _, err := os.Stat(shareData.Path); os.IsNotExist(err) {
			fmt.Printf("%sSubpath:%s %s\n", yellow, reset, name)
			fmt.Printf("%sFilepath:%s %s\n", red, reset, shareData.Path)
			fmt.Printf("%sWarning: File does not exist at path %s%s\n", red, shareData.Path, reset)
		} else {
			status := ""
			if shareData.Expired {
				status = red + " [EXPIRED]" + reset
			}
			fmt.Printf("%sSubpath:%s %s%s\n", green, reset, name, status)
			fmt.Printf("%sFilepath:%s %s\n", blue, reset, shareData.Path)
		}
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

	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Deleting link %s to file: %s\n", subpath, jsonData.Files[subpath].Path)
		delete(jsonData.Files, subpath)
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

	if _, exists := jsonData.Files[subpath]; exists {
		fmt.Printf("Subpath %s already exists.\n", subpath)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Warning: File path %s does not exist.\n", filePath)
		return
	}

	jsonData.Files[subpath] = shared.FileData{
		Path:       filePath,
		UploadTime: time.Now().Unix(),
		Uses:       uses,
		Expiration: expiration,
		AllowPost:  allowPost,
	}

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
		fmt.Printf("Changing subpath %s → %s\n", subpath, newSubpath)
		add(instructionPath, newSubpath, shareData.Path, shareData.Uses, shareData.Expiration, shareData.AllowPost)
		del(instructionPath, subpath)
		fmt.Println("Successfully changed subpath.")
	} else {
		fmt.Printf("No match found for subpath: %s\n", subpath)
	}
}

func setPassword(password string) {
	hashedPassword, err := shared.HashPassword(password)
	if err != nil {
		fmt.Println("Error hashing password:", err)
		return
	}
	var jsonData JsonData
	err = loadJsonData(instructionPath, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	jsonData.AdminPassword = hashedPassword
	err = writeJsonData(instructionPath, &jsonData)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Admin password set successfully.")
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
	helpText := map[string]string{
		"list": `NAME
       list - Displays all shared files and their assigned subpaths

SYNOPSIS
       list, l`,

		"add": `NAME
       add - Creates a new share for the specified file under the given subpath

SYNOPSIS
       add -subpath=<subpath> -file=<file>
           [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]
           [-allow-post]`,

		"addrandom": `NAME
       addrandom - Creates a new share with a randomly generated subpath

SYNOPSIS
       addrandom -file=<file>
           [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]
           [-allow-post]`,

		"delete": `NAME
       delete - Removes an existing share

SYNOPSIS
       delete -subpath=<subpath>`,

		"edit": `NAME
       edit - Changes the subpath of an existing share

SYNOPSIS
       edit -old_subpath=<old> -new_subpath=<new>`,

		"setpassword": `NAME
       setpassword - Sets the admin password

SYNOPSIS
       setpassword -password=<password>`,
	}

	if command == "" || helpText[command] == "" {
		fmt.Println("╔════════════════════════════╗")
		fmt.Println("║     AVAILABLE COMMANDS     ║")
		fmt.Println("╚════════════════════════════╝")
		for cmd, text := range helpText {
			fmt.Printf("\n• %s\n  %s\n", cmd, text)
		}
		return
	}

	fmt.Println("╔════════════════════════════")
	fmt.Printf("║   COMMAND: %s\n", command)
	fmt.Println("╚════════════════════════════")
	fmt.Println(helpText[command])
}

func main() {
	if len(os.Args) < 2 {
		printTooltips("")
		return
	}

	oldSubpathFlag := flag.String("old_subpath", "", "")
	flag.StringVar(oldSubpathFlag, "old", "", "")
	flag.StringVar(oldSubpathFlag, "o", "", "")

	newSubpathFlag := flag.String("new_subpath", "", "")
	flag.StringVar(newSubpathFlag, "new", "", "")
	flag.StringVar(newSubpathFlag, "n", "", "")

	subpathFlag := flag.String("subpath", "", "")
	flag.StringVar(subpathFlag, "s", "", "")

	filePathFlag := flag.String("file", "", "")
	flag.StringVar(filePathFlag, "f", "", "")

	usageLimitFlag := flag.Int("use-expiration", -1, "")
	flag.IntVar(usageLimitFlag, "uses", -1, "")
	flag.IntVar(usageLimitFlag, "u", -1, "")

	expirationTimeFlag := flag.String("time-expiration", "", "")
	flag.StringVar(expirationTimeFlag, "time", "", "")
	flag.StringVar(expirationTimeFlag, "t", "", "")

	allowPostFlag := flag.Bool("allow-post", false, "")
	flag.BoolVar(allowPostFlag, "post", false, "")
	flag.BoolVar(allowPostFlag, "p", false, "")

	passwordFlag := flag.String("password", "", "")
	flag.StringVar(passwordFlag, "pass", "", "")
	flag.StringVar(passwordFlag, "pwd", "", "")

	flag.CommandLine.Parse(os.Args[2:])

	expirationTime, err := calculateExpirationTime(*expirationTimeFlag)
	if err != nil {
		fmt.Println("Error calculating expiration:", err)
		return
	}

	switch os.Args[1] {
	case "list", "l":
		list(instructionPath)

	case "delete", "del", "remove", "rm":
		if *subpathFlag == "" {
			printTooltips("delete")
			return
		}
		del(instructionPath, *subpathFlag)

	case "add":
		if *subpathFlag == "" || *filePathFlag == "" {
			printTooltips("add")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		add(instructionPath, *subpathFlag, absPath, *usageLimitFlag, expirationTime, *allowPostFlag)

	case "addrandom", "random", "add_random", "addr":
		if *filePathFlag == "" {
			printTooltips("addrandom")
			return
		}
		absPath, _ := filepath.Abs(*filePathFlag)
		randomSubpath := generateRandomSubpath(randomSubpathLength)
		fmt.Printf("Random subpath: %s\n", randomSubpath)
		add(instructionPath, randomSubpath, absPath, *usageLimitFlag, expirationTime, *allowPostFlag)

	case "edit":
		if *oldSubpathFlag == "" || *newSubpathFlag == "" {
			printTooltips("edit")
			return
		}
		edit(*oldSubpathFlag, *newSubpathFlag)

	case "setpassword", "setpass", "password":
		if *passwordFlag == "" {
			printTooltips("setpassword")
			return
		}
		setPassword(*passwordFlag)

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printTooltips("")
	}
}
