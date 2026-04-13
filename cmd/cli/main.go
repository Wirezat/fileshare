package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// ── ANSI colors ──────────────────────────────────────
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
var (
	dataPath            = "/opt/fileshare/data.json"
	randomSubpathLength = 12
)

// ── Data I/O ──────────────────────────────────────────

// mustLoad loads the config or exits with an error log.
func mustLoad() *shared.Config {
	d, err := shared.LoadConfigFrom(dataPath)
	if err != nil {
		GoLog.Errorf("Failed to load data: %v", err)
		os.Exit(1)
	}
	return d
}

// mustSave saves the config or exits with an error log.
func mustSave(d *shared.Config) {
	if err := shared.SaveConfigTo(dataPath, d); err != nil {
		GoLog.Errorf("Failed to save data: %v", err)
		os.Exit(1)
	}
}

// ── Formatting helpers ────────────────────────────────

func fmtExpiration(ts int64) string {
	if ts == 0 {
		return colorGray + "never" + colorReset
	}
	t := time.Unix(ts, 0)
	if t.Before(time.Now()) {
		return colorRed + "expired" + colorReset
	}
	diff := time.Until(t)
	return fmt.Sprintf("%dd %dh", int(diff.Hours())/24, int(diff.Hours())%24)
}

func fmtUses(u int) string {
	switch u {
	case -1:
		return "∞"
	case 0:
		return colorRed + "0" + colorReset
	default:
		return strconv.Itoa(u)
	}
}

func fmtUpload(on bool) string {
	if on {
		return colorGreen + "on" + colorReset
	}
	return colorGray + "off" + colorReset
}

func truncatePath(p string, max int) string {
	if len(p) > max {
		return "…" + p[len(p)-(max-1):]
	}
	return p
}

func confirmPrompt(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		return ans == "y" || ans == "yes"
	}
	return false
}

func sortedKeys(m map[string]shared.FileData) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var tableDivider = colorGray + strings.Repeat("─", 76) + colorReset

// ── Commands ──────────────────────────────────────────

func cmdList(asJSON bool) {
	d := mustLoad()

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(d.Files)
		return
	}

	keys := sortedKeys(d.Files)
	total := len(keys)
	var active, expired, withUpload int
	for _, k := range keys {
		s := d.Files[k]
		if shared.IsExpired(s) {
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
	fmt.Println(tableDivider)

	if total == 0 {
		fmt.Println(colorGray + "  No shares yet. Add one with:  fileshare add -f <path>" + colorReset)
		fmt.Println()
		return
	}

	fmt.Printf("%-22s %-28s %6s  %-14s %-8s %s\n",
		colorBold+"SUBPATH"+colorReset,
		colorBold+"PATH"+colorReset,
		colorBold+"USES"+colorReset,
		colorBold+"EXPIRES"+colorReset,
		colorBold+"UPLOAD"+colorReset,
		colorBold+"STATUS"+colorReset,
	)
	fmt.Println(tableDivider)

	for _, sub := range keys {
		s := d.Files[sub]
		exp := shared.IsExpired(s)
		_, statErr := os.Stat(s.Path)
		pathMissing := os.IsNotExist(statErr)

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

		fmt.Printf("%s%-22s%s %-28s %6s  %-14s %-8s %s\n",
			subColor, "/"+sub, colorReset,
			truncatePath(s.Path, 27),
			fmtUses(s.Uses),
			fmtExpiration(s.Expiration),
			fmtUpload(s.AllowPost),
			status,
		)
	}
	fmt.Println()
}

func cmdAdd(subpath, filePath string, uses int, expiration int64, allowPost bool) {
	if filePath == "" {
		helpAdd()
		os.Exit(1)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		GoLog.Errorf("Cannot resolve path: %v", err)
		os.Exit(1)
	}

	if subpath == "" {
		subpath = shared.GenerateRandomSubpath(randomSubpathLength)
		GoLog.Infof("No subpath given — using random: %s", subpath)
	}

	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		GoLog.Warnf("%q does not exist on disk — share will be created anyway", absPath)
	}

	d := mustLoad()

	if _, exists := d.Files[subpath]; exists {
		GoLog.Errorf("Subpath /%s already exists", subpath)
		os.Exit(1)
	}

	d.Files[subpath] = shared.FileData{
		Path:       absPath,
		UploadTime: time.Now().Unix(),
		Uses:       uses,
		Expiration: expiration,
		AllowPost:  allowPost,
	}
	mustSave(d)

	fmt.Printf("%s✓%s Share added:\n", colorGreen, colorReset)
	fmt.Printf("  Subpath : %s/%s%s\n", colorBold, subpath, colorReset)
	fmt.Printf("  Path    : %s\n", absPath)
	fmt.Printf("  Uses    : %s\n", fmtUses(uses))
	fmt.Printf("  Expires : %s\n", fmtExpiration(expiration))
	fmt.Printf("  Upload  : %s\n", fmtUpload(allowPost))
	GoLog.Infof("Share added: /%s → %s", subpath, absPath)
}

func cmdDelete(subpath string, yes bool) {
	if subpath == "" {
		helpDelete()
		os.Exit(1)
	}

	d := mustLoad()

	s, exists := d.Files[subpath]
	if !exists {
		GoLog.Errorf("Share /%s not found", subpath)
		os.Exit(1)
	}

	if !yes {
		fmt.Printf("Delete share %s/%s%s → %s\n", colorBold, subpath, colorReset, s.Path)
		if !confirmPrompt("Confirm:") {
			fmt.Println("Aborted.")
			return
		}
	}

	delete(d.Files, subpath)
	mustSave(d)

	fmt.Printf("%s✓%s Share /%s deleted.\n", colorGreen, colorReset, subpath)
	GoLog.Infof("Share deleted: /%s", subpath)
}

func cmdEdit(subpath, newSubpath, newFile, newUsesStr, newExpiresStr, newAllowPostStr string) {
	if subpath == "" {
		helpEdit()
		os.Exit(1)
	}

	d := mustLoad()

	s, exists := d.Files[subpath]
	if !exists {
		GoLog.Errorf("Share /%s not found", subpath)
		os.Exit(1)
	}

	changed := false
	targetSubpath := subpath

	if newSubpath != "" && newSubpath != subpath {
		if _, exists := d.Files[newSubpath]; exists {
			GoLog.Errorf("Subpath /%s already exists", newSubpath)
			os.Exit(1)
		}
		fmt.Printf("  Subpath  : /%s → /%s\n", subpath, newSubpath)
		targetSubpath = newSubpath
		changed = true
	}

	if newFile != "" {
		abs, err := filepath.Abs(newFile)
		if err != nil {
			GoLog.Errorf("Cannot resolve path: %v", err)
			os.Exit(1)
		}
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			GoLog.Warnf("%q does not exist on disk", abs)
		}
		fmt.Printf("  Path     : %s → %s\n", s.Path, abs)
		s.Path = abs
		changed = true
	}

	if newUsesStr != "" {
		newUses, err := strconv.Atoi(newUsesStr)
		if err != nil {
			GoLog.Errorf("Invalid uses value %q: must be an integer (-1 = unlimited)", newUsesStr)
			os.Exit(1)
		}
		if newUses != s.Uses {
			fmt.Printf("  Uses     : %s → %s\n", fmtUses(s.Uses), fmtUses(newUses))
			s.Uses = newUses
			changed = true
		}
	}

	if newExpiresStr != "" {
		ts, err := shared.ParseExpiration(newExpiresStr)
		if err != nil {
			GoLog.Errorf("Invalid expiration: %v", err)
			os.Exit(1)
		}
		if ts != s.Expiration {
			fmt.Printf("  Expires  : %s → %s\n", fmtExpiration(s.Expiration), fmtExpiration(ts))
			s.Expiration = ts
			if ts == 0 || ts > time.Now().Unix() {
				s.Expired = false
			}
			changed = true
		}
	}

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
	mustSave(d)

	fmt.Printf("%s✓%s Share updated.\n", colorGreen, colorReset)
	GoLog.Infof("Share edited: /%s", targetSubpath)
}

func cmdPrune(yes bool) {
	d := mustLoad()

	var toDelete []string
	for k, s := range d.Files {
		if shared.IsExpired(s) {
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
	mustSave(d)

	fmt.Printf("%s✓%s Deleted %d expired share(s).\n", colorGreen, colorReset, len(toDelete))
	GoLog.Infof("Pruned %d expired share(s)", len(toDelete))
}

func cmdSetPassword(password string) {
	if password == "" {
		fmt.Print("New admin password: ")
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			password = strings.TrimSpace(sc.Text())
		}
	}
	if password == "" {
		GoLog.Error("Password cannot be empty")
		os.Exit(1)
	}

	hash, err := shared.HashPassword(password)
	if err != nil {
		GoLog.Errorf("Hashing failed: %v", err)
		os.Exit(1)
	}

	d := mustLoad()
	d.AdminPassword = hash
	mustSave(d)

	fmt.Printf("%s✓%s Admin password updated.\n", colorGreen, colorReset)
	GoLog.Info("Admin password updated")
}

func cmdSetUsername(username string) {
	if username == "" {
		fmt.Print("New admin username: ")
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			username = strings.TrimSpace(sc.Text())
		}
	}
	if username == "" {
		GoLog.Error("Username cannot be empty")
		os.Exit(1)
	}

	d := mustLoad()
	d.AdminUsername = username
	mustSave(d)

	fmt.Printf("%s✓%s Admin username updated.\n", colorGreen, colorReset)
	GoLog.Info("Admin username updated")
}

// ── Help texts ────────────────────────────────────────

func helpAdd() {
	fmt.Print(`
USAGE
  fileshare add [options]

OPTIONS
  -subpath, -s    URL subpath (omit for random)
  -file,    -f    File or folder path on the server  [required]
  -uses,    -u    Max downloads; -1 = unlimited  (default: -1)
  -expires, -e    Expiration: 24h, 7d, 2w, 3m, 1y, unix timestamp, or 0/never
  -allow-post, -p Allow uploads to this share

EXAMPLES
  fileshare add -s music -f /home/user/music
  fileshare add -f /tmp/report.pdf -e 7d -u 10
  fileshare add -f /srv/uploads -allow-post
  fileshare add -f /tmp/secret.zip   # random subpath

`)
}

func helpDelete() {
	fmt.Print(`
USAGE
  fileshare delete -subpath=<subpath> [-y]

OPTIONS
  -subpath, -s   Subpath of the share to delete  [required]
  -y             Skip confirmation

`)
}

func helpEdit() {
	fmt.Print(`
USAGE
  fileshare edit -subpath=<subpath> [options]

OPTIONS
  -subpath,     -s  Share to edit  [required]
  -new-subpath, -n  Rename to a different subpath
  -file,        -f  Change the server file/folder path
  -uses,        -u  Change max uses (-1 = unlimited)
  -expires,     -e  Change expiration (duration, unix timestamp, or 0/never)
  -allow-post       Change upload permission (true/false)

EXAMPLES
  fileshare edit -s music -n music2024
  fileshare edit -s docs  -e 30d -u 50
  fileshare edit -s temp  -allow-post=false

`)
}

func helpAdminUsername() {
	fmt.Print(`
USAGE
  fileshare setusername [options]

OPTIONS
  -username, -user, -u   New admin username (prompted if omitted)

EXAMPLES
  fileshare setusername -u myname
  fileshare setusername

`)
}

func helpAdminPassword() {
	fmt.Print(`
USAGE
  fileshare setpassword [options]

OPTIONS
  -password, -pass, -pwd, -p   New admin password

EXAMPLES
  fileshare setpassword -p mypassword
  fileshare setpassword

`)
}

func helpPrune() {
	fmt.Print(`
USAGE
  fileshare prune [-y]

Deletes all expired shares. Mirrors "Delete all expired shares" in Admin UI.

OPTIONS
  -y   Skip confirmation

`)
}

func printHelp() {
	fmt.Print(`
Fileshare CLI — manage shares from the command line

USAGE
  fileshare <command> [options]

COMMANDS
  list         Show all shares with status summary
  add          Create a new share
  delete       Delete a share
  edit         Edit an existing share
  prune        Delete all expired shares
  setpassword  Set the admin password
  setusername  Set the admin username
  help         Show this help or help for a specific command

GLOBAL FLAGS
  -data <path>   Path to data.json  (default: /opt/fileshare/data.json)

EXAMPLES
  fileshare list
  fileshare list --json
  fileshare add -f /home/user/music -s music -e 7d
  fileshare add -f /tmp/file.zip
  fileshare delete -s music
  fileshare edit -s music -e 30d -u 100
  fileshare prune -y
  fileshare setpassword
  fileshare setusername -u myname
  fileshare help add

`)
}

// ── Main ──────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	// Strip global -data flag before sub-command parsing.
	remaining := os.Args[1:]
	for i := 0; i < len(remaining); i++ {
		arg := remaining[i]
		if strings.HasPrefix(arg, "-data=") {
			dataPath = strings.TrimPrefix(arg, "-data=")
			remaining = append(remaining[:i], remaining[i+1:]...)
			break
		}
		if (arg == "-data" || arg == "--data") && i+1 < len(remaining) {
			dataPath = remaining[i+1]
			remaining = append(remaining[:i], remaining[i+2:]...)
			break
		}
	}

	if len(remaining) == 0 {
		printHelp()
		return
	}

	cmd, args := remaining[0], remaining[1:]

	switch cmd {

	case "list", "l", "ls":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output raw JSON")
		_ = fs.Parse(args)
		cmdList(*jsonOut)

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

		if cmd != "add" {
			*subpath = "" // legacy addrandom → force random subpath
		}
		exp, err := shared.ParseExpiration(*expires)
		if err != nil {
			GoLog.Errorf("Invalid expiration: %v", err)
			os.Exit(1)
		}
		cmdAdd(*subpath, *filePath, *uses, exp, *allowPost)

	case "delete", "del", "remove", "rm":
		fs := flag.NewFlagSet("delete", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdDelete(*subpath, *yes)

	case "edit":
		fs := flag.NewFlagSet("edit", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		oldSubpath := fs.String("old_subpath", "", "") // legacy
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

		if *subpath == "" && *oldSubpath != "" {
			*subpath = *oldSubpath
		}
		cmdEdit(*subpath, *newSubpath, *newFile, *newUses, *newExpires, *newAllowPost)

	case "prune", "cleanup", "clean":
		fs := flag.NewFlagSet("prune", flag.ExitOnError)
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdPrune(*yes)

	case "setpassword", "setpass", "password":
		fs := flag.NewFlagSet("setpassword", flag.ExitOnError)
		password := fs.String("password", "", "")
		fs.StringVar(password, "pass", "", "")
		fs.StringVar(password, "pwd", "", "")
		fs.StringVar(password, "p", "", "")
		_ = fs.Parse(args)
		cmdSetPassword(*password)

	case "setusername", "setuser", "username":
		fs := flag.NewFlagSet("setusername", flag.ExitOnError)
		username := fs.String("username", "", "")
		fs.StringVar(username, "user", "", "")
		fs.StringVar(username, "u", "", "")
		_ = fs.Parse(args)
		cmdSetUsername(*username)

	case "help", "--help", "-h":
		if len(args) > 0 {
			switch args[0] {
			case "add", "addrandom":
				helpAdd()
			case "delete", "del", "remove", "rm":
				helpDelete()
			case "edit":
				helpEdit()
			case "setusername", "setuser", "username":
				helpAdminUsername()
			case "setpassword", "setpass", "password":
				helpAdminPassword()
			case "prune", "cleanup":
				helpPrune()
			default:
				printHelp()
			}
		} else {
			printHelp()
		}

	default:
		GoLog.Errorf("Unknown command %q", cmd)
		printHelp()
		os.Exit(1)
	}
}
