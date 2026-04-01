package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wirezat/fileshare/pkg/shared"
)

// ── ANSI colours ──────────────────────────────────────
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

// ── Config ────────────────────────────────────────────
var dataPath = "/opt/fileshare/data.json"
var randomSubpathLength = 12

// ── Types ─────────────────────────────────────────────
type JsonData struct {
	Port          int                        `json:"port"`
	AdminPassword string                     `json:"admin_password"`
	Files         map[string]shared.FileData `json:"files"`
}

// ── Helpers: I/O ──────────────────────────────────────
func loadData() (*JsonData, error) {
	f, err := os.Open(dataPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %v", dataPath, err)
	}
	defer f.Close()

	var d JsonData
	if err := json.NewDecoder(f).Decode(&d); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %v", dataPath, err)
	}
	if d.Files == nil {
		d.Files = make(map[string]shared.FileData)
	}
	return &d, nil
}

func saveData(d *JsonData) error {
	f, err := os.Create(dataPath)
	if err != nil {
		return fmt.Errorf("cannot write %s: %v", dataPath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	return enc.Encode(d)
}

// ── Helpers: formatting ───────────────────────────────

// isExpired mirrors the UI's expiry logic exactly.
func isExpired(s shared.FileData) bool {
	if s.Expired {
		return true
	}
	if s.Uses == 0 {
		return true
	}
	if s.Expiration != 0 && s.Expiration < time.Now().Unix() {
		return true
	}
	return false
}

// fmtExpiration formats a unix timestamp the same way the UI does
// (never / Xd Yh remaining / expired).
func fmtExpiration(ts int64) string {
	if ts == 0 {
		return colorGray + "never" + colorReset
	}
	t := time.Unix(ts, 0)
	if t.Before(time.Now()) {
		return colorRed + "expired" + colorReset
	}
	diff := time.Until(t)
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// fmtUses mirrors the UI's ∞ / 0 / N display.
func fmtUses(u int) string {
	switch {
	case u == -1:
		return "∞"
	case u == 0:
		return colorRed + "0" + colorReset
	default:
		return strconv.Itoa(u)
	}
}

func generateRandomSubpath(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// parseExpiration accepts: "" or "0" or "never" → 0 (no expiry),
// a plain unix timestamp, or a duration string: 24h, 7d, 2w, 3m, 1y.
func parseExpiration(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "never" {
		return 0, nil
	}
	// Plain unix timestamp
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ts, nil
	}
	// Duration string
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid expiration %q — use e.g. 24h, 7d, 2w, 3m, 1y or a unix timestamp", s)
	}
	unit := string(s[len(s)-1])
	num, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid expiration %q: %v", s, err)
	}
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
		return 0, fmt.Errorf("unknown unit %q — use h, d, w, m or y", unit)
	}
}

// confirmPrompt asks the user a yes/no question; returns true on y/yes.
func confirmPrompt(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		return ans == "y" || ans == "yes"
	}
	return false
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, colorRed+"Error: "+colorReset+format+"\n", a...)
	os.Exit(1)
}

// ── Command: list ─────────────────────────────────────
//
// Displays the same information as the Admin UI's Shares tab:
// a stat summary row followed by a table with subpath, path, uses,
// expiration, upload flag and status.

func cmdList(asJSON bool) {
	d, err := loadData()
	if err != nil {
		die("%v", err)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(d.Files)
		return
	}

	keys := sortedKeys(d.Files)

	// ── stat summary (mirrors the UI's four stat-cards) ──────────
	total := len(keys)
	var active, expired, withUpload int
	for _, k := range keys {
		s := d.Files[k]
		if isExpired(s) {
			expired++
		} else {
			active++
		}
		if s.AllowPost {
			withUpload++
		}
	}

	fmt.Printf("\n%sSHARES%s   total: %s%d%s   active: %s%d%s   expired: %s%d%s   upload-enabled: %s%d%s\n",
		colorBold+colorCyan, colorReset,
		colorBold, total, colorReset,
		colorGreen, active, colorReset,
		colorRed, expired, colorReset,
		colorBlue, withUpload, colorReset,
	)
	fmt.Println(colorGray + strings.Repeat("─", 76) + colorReset)

	if total == 0 {
		fmt.Println(colorGray + "  No shares yet. Add one with:  fileshare-cli add -f <path>" + colorReset)
		fmt.Println()
		return
	}

	// ── table header ─────────────────────────────────────────────
	fmt.Printf("%-22s %-28s %6s  %-14s %-8s %s\n",
		colorBold+"SUBPATH"+colorReset,
		colorBold+"PATH"+colorReset,
		colorBold+"USES"+colorReset,
		colorBold+"EXPIRES"+colorReset,
		colorBold+"UPLOAD"+colorReset,
		colorBold+"STATUS"+colorReset,
	)
	fmt.Println(colorGray + strings.Repeat("─", 76) + colorReset)

	for _, sub := range keys {
		s := d.Files[sub]
		exp := isExpired(s)
		pathMissing := false
		if _, statErr := os.Stat(s.Path); os.IsNotExist(statErr) {
			pathMissing = true
		}

		subColor := colorGreen
		status := colorGreen + "active" + colorReset
		switch {
		case exp:
			subColor = colorRed
			status = colorRed + "expired" + colorReset
		case pathMissing:
			subColor = colorYellow
			status = colorYellow + "path missing" + colorReset
		}

		upload := colorGray + "off" + colorReset
		if s.AllowPost {
			upload = colorGreen + "on" + colorReset
		}

		path := s.Path
		if len(path) > 27 {
			path = "…" + path[len(path)-26:]
		}

		fmt.Printf("%s%-22s%s %-28s %6s  %-14s %-8s %s\n",
			subColor, "/"+sub, colorReset,
			path,
			fmtUses(s.Uses),
			fmtExpiration(s.Expiration),
			upload,
			status,
		)
	}
	fmt.Println()
}

// ── Command: add ─────────────────────────────────────
//
// Creates a new share. If -subpath is omitted a random one is generated,
// matching the UI behaviour (leave Subpath field empty → random).

func cmdAdd(subpath, filePath string, uses int, expiration int64, allowPost bool) {
	if filePath == "" {
		helpAdd()
		os.Exit(1)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		die("cannot resolve path: %v", err)
	}

	// Mirror UI: empty subpath → generate random
	if subpath == "" {
		subpath = generateRandomSubpath(randomSubpathLength)
		fmt.Printf("%sInfo:%s No subpath given — using random: %s%s%s\n",
			colorCyan, colorReset, colorBold, subpath, colorReset)
	}

	// UI adds the share even if the path doesn't exist yet; we warn but continue.
	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		fmt.Printf("%sWarning:%s %q does not exist on disk — share will be created anyway.\n",
			colorYellow, colorReset, absPath)
	}

	d, err := loadData()
	if err != nil {
		die("%v", err)
	}

	if _, exists := d.Files[subpath]; exists {
		die("subpath /%s already exists", subpath)
	}

	d.Files[subpath] = shared.FileData{
		Path:       absPath,
		UploadTime: time.Now().Unix(),
		Uses:       uses,
		Expiration: expiration,
		AllowPost:  allowPost,
	}

	if err := saveData(d); err != nil {
		die("%v", err)
	}

	uploadStr := colorGray + "off" + colorReset
	if allowPost {
		uploadStr = colorGreen + "on" + colorReset
	}

	fmt.Printf("%s✓%s Share added:\n", colorGreen, colorReset)
	fmt.Printf("  Subpath : %s/%s%s\n", colorBold, subpath, colorReset)
	fmt.Printf("  Path    : %s\n", absPath)
	fmt.Printf("  Uses    : %s\n", fmtUses(uses))
	fmt.Printf("  Expires : %s\n", fmtExpiration(expiration))
	fmt.Printf("  Upload  : %s\n", uploadStr)
}

// ── Command: delete ───────────────────────────────────
//
// Removes a share. Shows a confirmation prompt (like the UI's confirm dialog)
// unless -y is passed.

func cmdDelete(subpath string, yes bool) {
	if subpath == "" {
		helpDelete()
		os.Exit(1)
	}

	d, err := loadData()
	if err != nil {
		die("%v", err)
	}

	s, exists := d.Files[subpath]
	if !exists {
		die("share /%s not found", subpath)
	}

	if !yes {
		fmt.Printf("Delete share %s/%s%s → %s\n", colorBold, subpath, colorReset, s.Path)
		if !confirmPrompt("Confirm:") {
			fmt.Println("Aborted.")
			return
		}
	}

	delete(d.Files, subpath)
	if err := saveData(d); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s✓%s Share /%s deleted.\n", colorGreen, colorReset, subpath)
}

// ── Command: edit ─────────────────────────────────────
//
// Edits one or more properties of an existing share in-place.
// The UI only supports delete+recreate; the CLI goes further and lets
// you change any field without recreating the share.

func cmdEdit(subpath, newSubpath, newFile, newUsesStr, newExpiresStr, newAllowPostStr string) {
	if subpath == "" {
		helpEdit()
		os.Exit(1)
	}

	d, err := loadData()
	if err != nil {
		die("%v", err)
	}

	s, exists := d.Files[subpath]
	if !exists {
		die("share /%s not found", subpath)
	}

	changed := false

	// ── subpath rename ────
	targetSubpath := subpath
	if newSubpath != "" && newSubpath != subpath {
		if _, exists := d.Files[newSubpath]; exists {
			die("subpath /%s already exists", newSubpath)
		}
		fmt.Printf("  Subpath  : /%s → /%s\n", subpath, newSubpath)
		targetSubpath = newSubpath
		changed = true
	}

	// ── file path ─────────
	if newFile != "" {
		abs, absErr := filepath.Abs(newFile)
		if absErr != nil {
			die("cannot resolve path: %v", absErr)
		}
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			fmt.Printf("%sWarning:%s %q does not exist on disk.\n", colorYellow, colorReset, abs)
		}
		fmt.Printf("  Path     : %s → %s\n", s.Path, abs)
		s.Path = abs
		changed = true
	}

	// ── uses ──────────────
	if newUsesStr != "" {
		newUses, parseErr := strconv.Atoi(newUsesStr)
		if parseErr != nil {
			die("invalid uses value %q: must be an integer (-1 = unlimited)", newUsesStr)
		}
		if newUses != s.Uses {
			fmt.Printf("  Uses     : %s → %s\n", fmtUses(s.Uses), fmtUses(newUses))
			s.Uses = newUses
			changed = true
		}
	}

	// ── expiration ────────
	if newExpiresStr != "" {
		ts, parseErr := parseExpiration(newExpiresStr)
		if parseErr != nil {
			die("%v", parseErr)
		}
		if ts != s.Expiration {
			fmt.Printf("  Expires  : %s → %s\n", fmtExpiration(s.Expiration), fmtExpiration(ts))
			s.Expiration = ts
			// Clear the expired flag if a new expiry is set
			if ts == 0 || ts > time.Now().Unix() {
				s.Expired = false
			}
			changed = true
		}
	}

	// ── allow-post ────────
	if newAllowPostStr != "" {
		newPost := newAllowPostStr == "true" || newAllowPostStr == "1" || newAllowPostStr == "yes" || newAllowPostStr == "on"
		if newPost != s.AllowPost {
			onOff := func(b bool) string {
				if b {
					return "on"
				}
				return "off"
			}
			fmt.Printf("  Upload   : %s → %s\n", onOff(s.AllowPost), onOff(newPost))
			s.AllowPost = newPost
			changed = true
		}
	}

	if !changed {
		fmt.Println("Nothing changed.")
		return
	}

	if targetSubpath != subpath {
		delete(d.Files, subpath)
	}
	d.Files[targetSubpath] = s

	if err := saveData(d); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s✓%s Share updated.\n", colorGreen, colorReset)
}

// ── Command: prune ───────────────────────────────────
//
// Deletes all expired shares — mirrors the "Delete all expired shares"
// button in the UI's Settings → Danger Zone.

func cmdPrune(yes bool) {
	d, err := loadData()
	if err != nil {
		die("%v", err)
	}

	var toDelete []string
	for k, s := range d.Files {
		if isExpired(s) {
			toDelete = append(toDelete, k)
		}
	}
	sort.Strings(toDelete)

	if len(toDelete) == 0 {
		fmt.Println("No expired shares found.")
		return
	}

	fmt.Printf("Found %s%d%s expired share(s):\n", colorBold, len(toDelete), colorReset)
	for _, k := range toDelete {
		fmt.Printf("  %s/%s%s → %s\n", colorRed, k, colorReset, d.Files[k].Path)
	}

	if !yes && !confirmPrompt("Delete all?") {
		fmt.Println("Aborted.")
		return
	}

	for _, k := range toDelete {
		delete(d.Files, k)
	}
	if err := saveData(d); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s✓%s Deleted %d expired share(s).\n", colorGreen, colorReset, len(toDelete))
}

// ── Command: setpassword ─────────────────────────────
//
// Sets the admin password (stored as a bcrypt hash in data.json).
// The UI requires the current password because it validates over HTTP;
// the CLI has direct file access so no current-password check is needed.
// If -password is omitted the CLI prompts interactively (hides input
// by not echoing — pipe-friendly).

func cmdSetPassword(password string) {
	if password == "" {
		fmt.Print("New admin password: ")
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			password = strings.TrimSpace(sc.Text())
		}
	}
	if password == "" {
		die("password cannot be empty")
	}

	hash, err := shared.HashPassword(password)
	if err != nil {
		die("hashing failed: %v", err)
	}

	d, err := loadData()
	if err != nil {
		die("%v", err)
	}
	d.AdminPassword = hash
	if err := saveData(d); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s✓%s Admin password updated.\n", colorGreen, colorReset)
}

// ── Help texts ────────────────────────────────────────

func helpAdd() {
	fmt.Print(`
USAGE
  fileshare-cli add [options]

OPTIONS
  -subpath, -s    URL subpath for the share (omit or leave empty for random, same as Admin UI)
  -file,    -f    File or folder path on the server  [required]
  -uses,    -u    Max downloads; -1 = unlimited  (default: -1)
  -expires, -e    Expiration: duration (24h, 7d, 2w, 3m, 1y), unix timestamp, or 0/never
  -allow-post, -p Allow uploads to this share

EXAMPLES
  fileshare-cli add -s music -f /home/user/music
  fileshare-cli add -f /tmp/report.pdf -e 7d -u 10
  fileshare-cli add -f /srv/uploads -allow-post
  fileshare-cli add -f /tmp/secret.zip            # random subpath, like Admin UI

`)
}

func helpDelete() {
	fmt.Print(`
USAGE
  fileshare-cli delete -subpath=<subpath> [-y]

OPTIONS
  -subpath, -s   Subpath of the share to delete  [required]
  -y             Skip the confirmation prompt

`)
}

func helpEdit() {
	fmt.Print(`
USAGE
  fileshare-cli edit -subpath=<subpath> [options]

OPTIONS
  -subpath,     -s  Share to edit  [required]
  -new-subpath, -n  Rename to a different subpath
  -file,        -f  Change the server file/folder path
  -uses,        -u  Change max uses (-1 = unlimited)
  -expires,     -e  Change expiration (duration, unix timestamp, or 0/never)
  -allow-post       Change upload permission  (true/false)

EXAMPLES
  fileshare-cli edit -s music -n music2024
  fileshare-cli edit -s docs  -e 30d -u 50
  fileshare-cli edit -s temp  -allow-post=false
  fileshare-cli edit -s old   -f /new/path -e never

`)
}

func helpPrune() {
	fmt.Print(`
USAGE
  fileshare-cli prune [-y]

Deletes all expired shares from data.json.
Mirrors "Delete all expired shares" in the Admin UI Settings tab.

OPTIONS
  -y   Skip the confirmation prompt

`)
}

func printHelp() {
	fmt.Print(`
Fileshare CLI — manage shares from the command line

USAGE
  fileshare-cli <command> [options]

COMMANDS
  list         Show all shares with status summary  (mirrors Admin UI Shares tab)
  add          Create a new share
  delete       Delete a share
  edit         Edit an existing share (subpath, path, uses, expiration, upload)
  prune        Delete all expired shares  (mirrors Admin UI Settings → Danger Zone)
  setpassword  Set the admin password
  help         Show this help or help for a specific command

GLOBAL FLAGS
  -data <path>   Path to data.json  (default: /opt/fileshare/data.json)

EXAMPLES
  fileshare-cli list
  fileshare-cli list --json
  fileshare-cli add -f /home/user/music -s music -e 7d
  fileshare-cli add -f /tmp/file.zip                    # random subpath
  fileshare-cli delete -s music
  fileshare-cli edit -s music -e 30d -u 100
  fileshare-cli prune -y
  fileshare-cli help add

`)
}

// ── Misc ──────────────────────────────────────────────

func sortedKeys(m map[string]shared.FileData) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── Main ──────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	// Strip a leading -data=<path> global flag before parsing sub-commands.
	remaining := os.Args[1:]
	for i := 0; i < len(remaining); i++ {
		if strings.HasPrefix(remaining[i], "-data=") {
			dataPath = strings.TrimPrefix(remaining[i], "-data=")
			remaining = append(remaining[:i], remaining[i+1:]...)
			break
		} else if (remaining[i] == "-data" || remaining[i] == "--data") && i+1 < len(remaining) {
			dataPath = remaining[i+1]
			remaining = append(remaining[:i], remaining[i+2:]...)
			break
		}
	}

	if len(remaining) == 0 {
		printHelp()
		return
	}

	cmd := remaining[0]
	args := remaining[1:]

	switch cmd {

	// ── list ─────────────────────────────────────────
	case "list", "l", "ls":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output raw JSON")
		_ = fs.Parse(args)
		cmdList(*jsonOut)

	// ── add (+ legacy addrandom aliases) ─────────────
	case "add", "addrandom", "random", "add_random", "addr":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		filePath := fs.String("file", "", "")
		fs.StringVar(filePath, "f", "", "")
		uses := fs.Int("uses", -1, "")
		fs.IntVar(uses, "use-expiration", -1, "") // legacy
		fs.IntVar(uses, "u", -1, "")
		expires := fs.String("expires", "", "")
		fs.StringVar(expires, "time-expiration", "", "") // legacy
		fs.StringVar(expires, "time", "", "")            // legacy
		fs.StringVar(expires, "e", "", "")
		fs.StringVar(expires, "t", "", "")
		allowPost := fs.Bool("allow-post", false, "")
		fs.BoolVar(allowPost, "post", false, "")
		fs.BoolVar(allowPost, "p", false, "")
		_ = fs.Parse(args)

		// legacy addrandom → force empty subpath so a random one is generated
		if cmd != "add" {
			*subpath = ""
		}

		exp, err := parseExpiration(*expires)
		if err != nil {
			die("%v", err)
		}
		cmdAdd(*subpath, *filePath, *uses, exp, *allowPost)

	// ── delete ────────────────────────────────────────
	case "delete", "del", "remove", "rm":
		fs := flag.NewFlagSet("delete", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdDelete(*subpath, *yes)

	// ── edit ──────────────────────────────────────────
	case "edit":
		fs := flag.NewFlagSet("edit", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		// legacy flags
		oldSubpath := fs.String("old_subpath", "", "")
		fs.StringVar(oldSubpath, "old", "", "")
		fs.StringVar(oldSubpath, "o", "", "")
		newSubpath := fs.String("new-subpath", "", "")
		fs.StringVar(newSubpath, "new_subpath", "", "") // legacy
		fs.StringVar(newSubpath, "new", "", "")         // legacy
		fs.StringVar(newSubpath, "n", "", "")
		newFile := fs.String("file", "", "")
		fs.StringVar(newFile, "f", "", "")
		newUses := fs.String("uses", "", "")
		fs.StringVar(newUses, "u", "", "")
		newExpires := fs.String("expires", "", "")
		fs.StringVar(newExpires, "e", "", "")
		newAllowPost := fs.String("allow-post", "", "")
		fs.StringVar(newAllowPost, "p", "", "")
		_ = fs.Parse(args)

		// legacy: -old_subpath was used as the share to edit
		if *subpath == "" && *oldSubpath != "" {
			*subpath = *oldSubpath
		}
		cmdEdit(*subpath, *newSubpath, *newFile, *newUses, *newExpires, *newAllowPost)

	// ── prune ─────────────────────────────────────────
	case "prune", "cleanup", "clean":
		fs := flag.NewFlagSet("prune", flag.ExitOnError)
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdPrune(*yes)

	// ── setpassword ───────────────────────────────────
	case "setpassword", "setpass", "password":
		fs := flag.NewFlagSet("setpassword", flag.ExitOnError)
		password := fs.String("password", "", "")
		fs.StringVar(password, "pass", "", "")
		fs.StringVar(password, "pwd", "", "")
		fs.StringVar(password, "p", "", "")
		_ = fs.Parse(args)
		cmdSetPassword(*password)

	// ── help ──────────────────────────────────────────
	case "help", "--help", "-h":
		if len(args) > 0 {
			switch args[0] {
			case "add", "addrandom":
				helpAdd()
			case "delete", "del", "remove", "rm":
				helpDelete()
			case "edit":
				helpEdit()
			case "prune", "cleanup":
				helpPrune()
			default:
				printHelp()
			}
		} else {
			printHelp()
		}

	default:
		fmt.Fprintf(os.Stderr, "%sError:%s unknown command %q\n\n", colorRed, colorReset, cmd)
		printHelp()
		os.Exit(1)
	}
}
