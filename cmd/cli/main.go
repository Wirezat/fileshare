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

// ── ANSI colors ───────────────────────────────────────────────────────────────

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

// ── Config ────────────────────────────────────────────────────────────────────

var (
	dataPath            = "/opt/fileshare/data.json"
	randomSubpathLength = 12
)

// ── Data I/O ──────────────────────────────────────────────────────────────────

func mustLoad() *shared.Config {
	d, err := shared.LoadConfigFrom(dataPath)
	if err != nil {
		GoLog.Errorf("Failed to load data: %v", err)
		os.Exit(1)
	}
	return d
}

func mustSave(d *shared.Config) {
	if err := shared.SaveConfigTo(dataPath, d); err != nil {
		GoLog.Errorf("Failed to save data: %v", err)
		os.Exit(1)
	}
}

// ── Formatting helpers ────────────────────────────────────────────────────────

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
	case shared.UnlimitedUses:
		return "inf"
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

func fmtPassword(hash string) string {
	if hash != "" {
		return colorYellow + "yes" + colorReset
	}
	return colorGray + "no" + colorReset
}

func truncatePath(p string, max int) string {
	if len(p) > max {
		return "..." + p[len(p)-(max-3):]
	}
	return p
}

func sortedKeys(m map[string]shared.FileData) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── Interactive helpers ───────────────────────────────────────────────────────

func confirmPrompt(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		return ans == "y" || ans == "yes"
	}
	return false
}

func promptLine(prompt string) string {
	fmt.Print(prompt)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

func parseBoolValue(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid value %q — use true/false/yes/no/on/off", s)
	}
}

// hashSharePassword hashes a plaintext share password.
// An empty string is returned unchanged (represents no password).
func hashSharePassword(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return shared.HashPassword(plain)
}

// verifyAdminPassword checks the plaintext against the stored admin password
// hash and exits with an error if it does not match.
func verifyAdminPassword(plain string, d *shared.Config) {
	if !shared.CheckPassword(plain, d.AdminPassword) {
		GoLog.Error("Current password is incorrect")
		os.Exit(1)
	}
}

// isManuallyDisabled returns true when the share has been explicitly disabled
// via the Expired flag but has not yet hit a time-based expiry.
func isManuallyDisabled(s shared.FileData) bool {
	if !s.Expired {
		return false
	}
	return s.Expiration == 0 || time.Unix(s.Expiration, 0).After(time.Now())
}

// ── Commands ──────────────────────────────────────────────────────────────────

var tableDivider = colorGray + strings.Repeat("-", 92) + colorReset

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

	var active, deactivated, withUpload, withPassword int
	for _, k := range keys {
		s := d.Files[k]
		if shared.IsExpired(s) {
			deactivated++
		} else {
			active++
		}
		if s.AllowPost {
			withUpload++
		}
		if s.Password != "" {
			withPassword++
		}
	}

	fmt.Printf(
		"\n%sSHARES%s  total: %s%d%s  active: %s%d%s  deactivated: %s%d%s  upload: %s%d%s  password-protected: %s%d%s\n",
		colorBold+colorCyan, colorReset,
		colorBold, total, colorReset,
		colorGreen, active, colorReset,
		colorRed, deactivated, colorReset,
		colorBlue, withUpload, colorReset,
		colorYellow, withPassword, colorReset,
	)
	fmt.Println(tableDivider)

	if total == 0 {
		fmt.Println(colorGray + "  No shares. Create one with: fileshare add -f <path>" + colorReset)
		fmt.Println()
		return
	}

	fmt.Printf("%-22s %-26s %6s  %-14s %-8s %-5s %s\n",
		colorBold+"SUBPATH"+colorReset,
		colorBold+"PATH"+colorReset,
		colorBold+"USES"+colorReset,
		colorBold+"EXPIRES"+colorReset,
		colorBold+"UPLOAD"+colorReset,
		colorBold+"PW"+colorReset,
		colorBold+"STATUS"+colorReset,
	)
	fmt.Println(tableDivider)

	for _, sub := range keys {
		s := d.Files[sub]
		_, statErr := os.Stat(s.Path)
		pathMissing := os.IsNotExist(statErr)

		subColor := colorGreen
		status := colorGreen + "active" + colorReset

		switch {
		case isManuallyDisabled(s):
			subColor = colorGray
			status = colorGray + "disabled" + colorReset
		case shared.IsExpired(s):
			subColor = colorRed
			status = colorRed + "expired" + colorReset
		case pathMissing:
			subColor = colorYellow
			status = colorYellow + "path missing" + colorReset
		}

		fmt.Printf("%s%-22s%s %-26s %6s  %-14s %-8s %-5s %s\n",
			subColor, "/"+sub, colorReset,
			truncatePath(s.Path, 25),
			fmtUses(s.Uses),
			fmtExpiration(s.Expiration),
			fmtUpload(s.AllowPost),
			fmtPassword(s.Password),
			status,
		)
	}
	fmt.Println()
}

func cmdAdd(subpath, filePath string, uses int, expiration int64, allowPost bool, password string) {
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

	hashedPw, err := hashSharePassword(password)
	if err != nil {
		GoLog.Errorf("Failed to hash share password: %v", err)
		os.Exit(1)
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
		Password:   hashedPw,
	}
	mustSave(d)

	fmt.Printf("%s+%s Share added:\n", colorGreen, colorReset)
	fmt.Printf("  Subpath  : /%s\n", subpath)
	fmt.Printf("  Path     : %s\n", absPath)
	fmt.Printf("  Uses     : %s\n", fmtUses(uses))
	fmt.Printf("  Expires  : %s\n", fmtExpiration(expiration))
	fmt.Printf("  Upload   : %s\n", fmtUpload(allowPost))
	fmt.Printf("  Password : %s\n", fmtPassword(hashedPw))
	GoLog.Infof("Share added: /%s -> %s", subpath, absPath)
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
		fmt.Printf("Delete share /%s -> %s\n", subpath, s.Path)
		if !confirmPrompt("Confirm:") {
			fmt.Println("Aborted.")
			return
		}
	}

	delete(d.Files, subpath)
	mustSave(d)

	fmt.Printf("%s-%s Share /%s deleted.\n", colorRed, colorReset, subpath)
	GoLog.Infof("Share deleted: /%s", subpath)
}

func cmdEdit(subpath, newSubpath, newFile, newUsesStr, newExpiresStr, newUploadStr, newActiveStr, newPassword string, clearPassword bool) {
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
		fmt.Printf("  Subpath  : /%s -> /%s\n", subpath, newSubpath)
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
		fmt.Printf("  Path     : %s -> %s\n", s.Path, abs)
		s.Path = abs
		changed = true
	}

	if newUsesStr != "" {
		newUses, err := strconv.Atoi(newUsesStr)
		if err != nil {
			GoLog.Errorf("Invalid uses value %q — must be an integer (-1 = unlimited)", newUsesStr)
			os.Exit(1)
		}
		if newUses != s.Uses {
			fmt.Printf("  Uses     : %s -> %s\n", fmtUses(s.Uses), fmtUses(newUses))
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
			fmt.Printf("  Expires  : %s -> %s\n", fmtExpiration(s.Expiration), fmtExpiration(ts))
			s.Expiration = ts
			// Clear the expired flag when moving the deadline into the future.
			if ts == 0 || ts > time.Now().Unix() {
				s.Expired = false
			}
			changed = true
		}
	}

	if newUploadStr != "" {
		newUpload, err := parseBoolValue(newUploadStr)
		if err != nil {
			GoLog.Errorf("-upload: %v", err)
			os.Exit(1)
		}
		if newUpload != s.AllowPost {
			fmt.Printf("  Upload   : %s -> %s\n", fmtUpload(s.AllowPost), fmtUpload(newUpload))
			s.AllowPost = newUpload
			changed = true
		}
	}

	if newActiveStr != "" {
		newActive, err := parseBoolValue(newActiveStr)
		if err != nil {
			GoLog.Errorf("-active: %v", err)
			os.Exit(1)
		}
		// active=true clears the Expired flag; active=false sets it.
		wantExpired := !newActive
		if wantExpired != s.Expired {
			oldLabel := "active"
			if s.Expired {
				oldLabel = "disabled"
			}
			newLabel := "active"
			if wantExpired {
				newLabel = "disabled"
			}
			fmt.Printf("  Active   : %s -> %s\n", oldLabel, newLabel)
			s.Expired = wantExpired
			changed = true
		}
	}

	switch {
	case clearPassword:
		if s.Password != "" {
			fmt.Printf("  Password : removed\n")
			s.Password = ""
			changed = true
		}
	case newPassword != "":
		hashed, err := hashSharePassword(newPassword)
		if err != nil {
			GoLog.Errorf("Failed to hash share password: %v", err)
			os.Exit(1)
		}
		action := "set"
		if s.Password != "" {
			action = "changed"
		}
		fmt.Printf("  Password : %s\n", action)
		s.Password = hashed
		changed = true
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

	fmt.Printf("%s*%s Share /%s updated.\n", colorGreen, colorReset, targetSubpath)
	GoLog.Infof("Share edited: /%s", targetSubpath)
}

func cmdEnable(subpath string) {
	if subpath == "" {
		helpEnable()
		os.Exit(1)
	}
	d := mustLoad()
	s, exists := d.Files[subpath]
	if !exists {
		GoLog.Errorf("Share /%s not found", subpath)
		os.Exit(1)
	}
	if !s.Expired {
		fmt.Printf("Share /%s is already active.\n", subpath)
		return
	}
	s.Expired = false
	d.Files[subpath] = s
	mustSave(d)
	fmt.Printf("%s*%s Share /%s enabled.\n", colorGreen, colorReset, subpath)
	GoLog.Infof("Share enabled: /%s", subpath)
}

func cmdDisable(subpath string) {
	if subpath == "" {
		helpEnable()
		os.Exit(1)
	}
	d := mustLoad()
	s, exists := d.Files[subpath]
	if !exists {
		GoLog.Errorf("Share /%s not found", subpath)
		os.Exit(1)
	}
	if s.Expired {
		fmt.Printf("Share /%s is already disabled.\n", subpath)
		return
	}
	s.Expired = true
	d.Files[subpath] = s
	mustSave(d)
	fmt.Printf("%s*%s Share /%s disabled.\n", colorGray, colorReset, subpath)
	GoLog.Infof("Share disabled: /%s", subpath)
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
		fmt.Printf("  /%s -> %s\n", k, d.Files[k].Path)
	}

	if !yes && !confirmPrompt("Delete all?") {
		fmt.Println("Aborted.")
		return
	}

	for _, k := range toDelete {
		delete(d.Files, k)
	}
	mustSave(d)

	fmt.Printf("%s-%s Deleted %d expired share(s).\n", colorRed, colorReset, len(toDelete))
	GoLog.Infof("Pruned %d expired share(s)", len(toDelete))
}

func cmdSetPassword(currentPassword, newPassword string) {
	d := mustLoad()

	if d.AdminPassword != "" && currentPassword == "" {
		currentPassword = promptLine("Current admin password: ")
	}
	if d.AdminPassword != "" {
		verifyAdminPassword(currentPassword, d)
	}

	if newPassword == "" {
		newPassword = promptLine("New admin password: ")
	}
	if newPassword == "" {
		GoLog.Error("Password cannot be empty")
		os.Exit(1)
	}

	hash, err := shared.HashPassword(newPassword)
	if err != nil {
		GoLog.Errorf("Hashing failed: %v", err)
		os.Exit(1)
	}

	d.AdminPassword = hash
	mustSave(d)

	fmt.Printf("%s*%s Admin password updated.\n", colorGreen, colorReset)
	GoLog.Info("Admin password updated")
}

func cmdSetUsername(currentPassword, newUsername string) {
	d := mustLoad()

	if d.AdminPassword != "" && currentPassword == "" {
		currentPassword = promptLine("Current admin password: ")
	}
	if d.AdminPassword != "" {
		verifyAdminPassword(currentPassword, d)
	}

	if newUsername == "" {
		newUsername = promptLine("New admin username: ")
	}
	if newUsername == "" {
		GoLog.Error("Username cannot be empty")
		os.Exit(1)
	}

	d.AdminUsername = newUsername
	mustSave(d)

	fmt.Printf("%s*%s Admin username updated.\n", colorGreen, colorReset)
	GoLog.Info("Admin username updated")
}

// ── Help texts ────────────────────────────────────────────────────────────────

func helpAdd() {
	fmt.Print(`
USAGE
  fileshare add [options]

OPTIONS
  -subpath, -s       URL subpath (omit for random)
  -file,    -f       File or folder path on the server  [required]
  -uses,    -u       Max downloads; -1 = unlimited  (default: -1)
  -expires, -e       Expiration: 24h, 7d, 2w, 3m, 1y, unix timestamp, or 0/never
  -upload            Allow uploads to this share
  -password, -pw     Protect the share with a password

EXAMPLES
  fileshare add -s docs -f /home/user/docs
  fileshare add -f /tmp/report.pdf -e 7d -u 10
  fileshare add -f /srv/uploads -upload
  fileshare add -f /tmp/secret.zip -pw hunter2

`)
}

func helpDelete() {
	fmt.Print(`
USAGE
  fileshare delete -s <subpath> [-y]

OPTIONS
  -subpath, -s   Subpath of the share to delete  [required]
  -y             Skip confirmation

`)
}

func helpEdit() {
	fmt.Print(`
USAGE
  fileshare edit -s <subpath> [options]

OPTIONS
  -subpath,       -s    Share to edit  [required]
  -new-subpath,   -n    Rename to a different subpath
  -file,          -f    Change the server file/folder path
  -uses,          -u    Change max uses (-1 = unlimited)
  -expires,       -e    Change expiration (duration, unix timestamp, or 0/never)
  -upload               Change upload permission (true/false/yes/no/on/off)
  -active               Enable or disable the share (true/false)
  -password,      -pw   Set or change the share password
  -clear-password       Remove the share password

EXAMPLES
  fileshare edit -s music -n music2024
  fileshare edit -s docs  -e 30d -u 50
  fileshare edit -s temp  -upload=false
  fileshare edit -s priv  -pw newpassword
  fileshare edit -s priv  -clear-password
  fileshare edit -s temp  -active=false

`)
}

func helpEnable() {
	fmt.Print(`
USAGE
  fileshare enable  -s <subpath>
  fileshare disable -s <subpath>

Enables or disables a share without deleting it.
Equivalent to: fileshare edit -s <subpath> -active true/false

OPTIONS
  -subpath, -s   Subpath of the share  [required]

`)
}

func helpSetUsername() {
	fmt.Print(`
USAGE
  fileshare setusername [options]

OPTIONS
  -username, -u    New admin username (prompted if omitted)
  -current,  -c    Current admin password for verification (prompted if omitted)

EXAMPLES
  fileshare setusername -u myname
  fileshare setusername

`)
}

func helpSetPassword() {
	fmt.Print(`
USAGE
  fileshare setpassword [options]

OPTIONS
  -password, -p   New admin password (prompted if omitted)
  -current,  -c   Current admin password for verification (prompted if omitted)

EXAMPLES
  fileshare setpassword -p mysecret
  fileshare setpassword

`)
}

func helpPrune() {
	fmt.Print(`
USAGE
  fileshare prune [-y]

Deletes all expired shares permanently.

OPTIONS
  -y   Skip confirmation

`)
}

func printHelp() {
	fmt.Print(`
Fileshare CLI -- manage shares from the command line

USAGE
  fileshare <command> [options]

COMMANDS
  list          Show all shares with status summary
  add           Create a new share
  delete        Delete a share
  edit          Edit an existing share
  enable        Re-enable a disabled share
  disable       Disable a share without deleting it
  prune         Delete all expired shares
  setpassword   Set the admin password
  setusername   Set the admin username
  help          Show this help or help for a specific command

GLOBAL FLAGS
  -data <path>   Path to data.json  (default: /opt/fileshare/data.json)

EXAMPLES
  fileshare list
  fileshare list --json
  fileshare add -f /home/user/music -s music -e 7d
  fileshare add -f /tmp/secret.zip -pw hunter2
  fileshare delete -s music
  fileshare edit -s music -e 30d -u 100
  fileshare edit -s priv -clear-password
  fileshare disable -s temp
  fileshare enable  -s temp
  fileshare prune -y
  fileshare setpassword
  fileshare setusername -u myname
  fileshare help add

`)
}

// ── Main ──────────────────────────────────────────────────────────────────────

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

	// ── list ─────────────────────────────────────────────────────────────────
	case "list", "l", "ls":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output raw JSON")
		_ = fs.Parse(args)
		cmdList(*jsonOut)

	// ── add ──────────────────────────────────────────────────────────────────
	case "add", "addrandom", "random", "add_random", "addr":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		filePath := fs.String("file", "", "")
		fs.StringVar(filePath, "f", "", "")
		uses := fs.Int("uses", -1, "")
		fs.IntVar(uses, "u", -1, "")
		expires := fs.String("expires", "", "")
		fs.StringVar(expires, "e", "", "")
		fs.StringVar(expires, "t", "", "") // legacy alias
		allowPost := fs.Bool("upload", false, "")
		fs.BoolVar(allowPost, "allow-post", false, "") // legacy alias
		fs.BoolVar(allowPost, "p", false, "")          // legacy alias
		password := fs.String("password", "", "")
		fs.StringVar(password, "pw", "", "")
		_ = fs.Parse(args)

		if cmd != "add" {
			*subpath = "" // legacy addrandom forces a random subpath
		}
		exp, err := shared.ParseExpiration(*expires)
		if err != nil {
			GoLog.Errorf("Invalid expiration: %v", err)
			os.Exit(1)
		}
		cmdAdd(*subpath, *filePath, *uses, exp, *allowPost, *password)

	// ── delete ───────────────────────────────────────────────────────────────
	case "delete", "del", "remove", "rm":
		fs := flag.NewFlagSet("delete", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdDelete(*subpath, *yes)

	// ── edit ─────────────────────────────────────────────────────────────────
	case "edit":
		fs := flag.NewFlagSet("edit", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		oldSubpath := fs.String("old_subpath", "", "") // legacy
		fs.StringVar(oldSubpath, "o", "", "")
		newSubpath := fs.String("new-subpath", "", "")
		fs.StringVar(newSubpath, "n", "", "")
		newFile := fs.String("file", "", "")
		fs.StringVar(newFile, "f", "", "")
		newUses := fs.String("uses", "", "")
		fs.StringVar(newUses, "u", "", "")
		newExpires := fs.String("expires", "", "")
		fs.StringVar(newExpires, "e", "", "")
		newUpload := fs.String("upload", "", "")
		fs.StringVar(newUpload, "allow-post", "", "") // legacy alias
		newActive := fs.String("active", "", "")
		newPassword := fs.String("password", "", "")
		fs.StringVar(newPassword, "pw", "", "")
		clearPassword := fs.Bool("clear-password", false, "")
		_ = fs.Parse(args)

		if *subpath == "" && *oldSubpath != "" {
			*subpath = *oldSubpath
		}
		cmdEdit(*subpath, *newSubpath, *newFile, *newUses, *newExpires, *newUpload, *newActive, *newPassword, *clearPassword)

	// ── enable / disable ─────────────────────────────────────────────────────
	case "enable":
		fs := flag.NewFlagSet("enable", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		_ = fs.Parse(args)
		cmdEnable(*subpath)

	case "disable":
		fs := flag.NewFlagSet("disable", flag.ExitOnError)
		subpath := fs.String("subpath", "", "")
		fs.StringVar(subpath, "s", "", "")
		_ = fs.Parse(args)
		cmdDisable(*subpath)

	// ── prune ────────────────────────────────────────────────────────────────
	case "prune", "cleanup", "clean":
		fs := flag.NewFlagSet("prune", flag.ExitOnError)
		yes := fs.Bool("y", false, "")
		fs.BoolVar(yes, "yes", false, "")
		_ = fs.Parse(args)
		cmdPrune(*yes)

	// ── setpassword ──────────────────────────────────────────────────────────
	case "setpassword", "setpass", "password":
		fs := flag.NewFlagSet("setpassword", flag.ExitOnError)
		current := fs.String("current", "", "")
		fs.StringVar(current, "c", "", "")
		newPw := fs.String("password", "", "")
		fs.StringVar(newPw, "p", "", "")
		_ = fs.Parse(args)
		cmdSetPassword(*current, *newPw)

	// ── setusername ──────────────────────────────────────────────────────────
	case "setusername", "setuser", "username":
		fs := flag.NewFlagSet("setusername", flag.ExitOnError)
		current := fs.String("current", "", "")
		fs.StringVar(current, "c", "", "")
		username := fs.String("username", "", "")
		fs.StringVar(username, "u", "", "")
		_ = fs.Parse(args)
		cmdSetUsername(*current, *username)

	// ── help ─────────────────────────────────────────────────────────────────
	case "help", "--help", "-h":
		if len(args) > 0 {
			switch args[0] {
			case "add", "addrandom":
				helpAdd()
			case "delete", "del", "remove", "rm":
				helpDelete()
			case "edit":
				helpEdit()
			case "enable", "disable":
				helpEnable()
			case "setusername", "setuser", "username":
				helpSetUsername()
			case "setpassword", "setpass", "password":
				helpSetPassword()
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
