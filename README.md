# fileshare

A self-hosted file sharing server written in Go. Serve files and folders over HTTP with optional password protection, upload support, use limits, and expiration — managed through a web-based admin interface or a command-line tool.
This tool was developed because I often wanted to send files to friends, only for my messengers to block them due to file size, since they were on my NAS anyway, so why not give them the direct link to a file or folder?
Because there were no proper tools that were able to do this in a simple way without being massively overcomplicated, having user instances, etc., etc.
---

## Features

- **Web admin UI** — manage all shares from a browser, no terminal required
- **Password-protected shares** — per-share passwords with token-based sessions
- **Upload support** — allow others to upload files into a share via chunked upload
- **Expiration** — time-based or use-count-based share limits
- **Directory listing** — browse folders and download as ZIP
- **Live log viewer** — stream server logs in real time from the admin UI
- **Dark mode** — persisted per browser
- **CLI tool** — full share management from the command line for scripting and remote access

---

## Getting Started

### 1. Install

Just use the installer provided in the release. If you want to build it yourself, the install script is the same I've used in the development process, you can find it in the same place in the code itself.

### 3. First-time setup

Open `http://localhost:<port>/setup` or `http://localhost:<port>/admin` in your browser. You will be prompted to set an admin username and password. After that, `/setup` is permanently disabled, and you are redirected to the admin panel.

---

## Admin UI

The admin interface is available at `/admin`. It is split into three tabs.

### Shares

Create and manage all shares from the shares tab. Each share maps a public URL subpath to a file or folder on the server.

| Field | Description |
|---|---|
| Subpath | The URL path, e.g., `docs` → `http://host/docs`. Leave empty for a random value. |
| Path | Absolute path to the file or folder on the server. |
| Max uses | How many times the share can be accessed. `-1` for unlimited. |
| Expires | Optional expiration date and time. |
| Allow uploads | Let visitors upload files into this share's directory. |
| Password | Optionally protect the share with a password. |

Shares can be edited, disabled, re-enabled, and deleted inline from the table. A disabled share remains in the list but is inaccessible until re-enabled.

### Logs

Live server log stream with INFO / WARN / ERROR filtering. The log viewer connects via SSE and updates in real time. Clearing the view does not affect the log file on disk.

### Settings

| Setting | Description |
|---|---|
| Change username | Updates the admin username. Requires the current password. |
| Change password | Updates the admin password (stored as a bcrypt hash). Requires the current password. |
| Delete expired shares | Permanently removes all expired shares from `data.json`. |

---

## Share Behavior

### Accessing a share

- `http://host/<subpath>` — serves the file directly or shows a directory listing.
- Directories can be downloaded as a ZIP via the `?download=zip` query parameter.
- If the share has a password, visitors are shown a password gate before accessing the content.

### Password-protected shares

Entering the correct password sets a session cookie scoped to that subpath. The session is valid for 24 hours. Each share's password is stored as a bcrypt hash.

### Uploads

When a share has uploads enabled, visitors can drag and drop files onto the listing page. Uploads use a chunked protocol with crash-safe resume support.

---

## CLI

The CLI tool provides full share management for use in scripts or over SSH. It reads and writes `data.json` directly.
Note: This was the original interface for the program, so I wanted to keep it as a legacy option. Since I've made the WebUI,
its updates are entirely Vibe Coded, but it should work without problems. I guess. I haven't put the most of work into it

```
fileshare <command> [options]
```

### Commands

| Command | Description |
|---|---|
| `list` | Show all shares with status, expiration, upload flag, and password indicator. |
| `add` | Create a new share. |
| `delete` | Delete a share. |
| `edit` | Edit an existing share (path, subpath, uses, expiration, upload, active state, password). |
| `enable` | Re-enable a disabled share. |
| `disable` | Disable a share without deleting it. |
| `prune` | Delete all expired shares permanently. |
| `setpassword` | Update the admin password. Prompts for the current password if one is set. |
| `setusername` | Update the admin username. Prompts for the current password if one is set. |
| `help <command>` | Show detailed help for any command. |

### Quick reference

```sh
# List all shares
fileshare list
fileshare list --json

# Add a share
fileshare add -f /srv/files/report.pdf -s report -e 7d -u 10
fileshare add -f /srv/uploads -upload           # random subpath, uploads enabled
fileshare add -f /srv/secret.zip -pw hunter2   # password-protected

# Edit a share
fileshare edit -s report -e 30d -u 50
fileshare edit -s report -pw newpassword
fileshare edit -s report -clear-password
fileshare edit -s report -active=false         # disable without deleting

# Enable / disable
fileshare disable -s report
fileshare enable  -s report

# Delete
fileshare delete -s report

# Clean up expired shares
fileshare prune -y

# Update admin credentials
fileshare setpassword
fileshare setusername -u newname
```
