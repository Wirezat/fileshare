# fileshare

A self-hosted file sharing server written in Go. Serve files and folders over HTTP with optional password protection, upload support, use limits, and expiration — managed through a web-based admin interface.
This tool was developed because I often wanted to send files to friends, only for my messengers to block them due to file size, since they were on my NAS anyway, so why not give them the direct link to a file or folder?
Because there were no proper tools that were able to do this in a simple way without being massively overcomplicated, having user instances, etc., etc.
---

## Features

- **Web admin UI** — manage all shares from a browser, no terminal required
- **Password-protected shares** — per-share passwords with token-based sessions
- **Upload support** — allow others to upload files into a share via chunked upload
- **Office documents** — view or edit Word, Excel and PowerPoint files in the browser through an ONLYOFFICE-compatible document server
- **Expiration** — time-based share limits
- **Directory listing** — browse folders, filter by name, preview media, PDFs, text and Markdown, download the folder or a selection as ZIP (per share switchable)
- **Link previews** — pasting a share link into a messenger shows a proper preview instead of a bare URL
- **Live log viewer** — stream server logs in real time from the admin UI
- **Dark mode** — persisted per browser

---

## Getting Started

### 1. Install

Just use the installer provided in the release. If you want to build it yourself, the install script is the same I've used in the development process, you can find it in the same place in the code itself.

```sh
sudo bash scripts/fileshare-installer.sh              # menu: install / update / uninstall
bash scripts/fileshare-installer.sh --remote user@host # same, but on another machine over ssh
```

Install builds the server, deploys to `/opt/fileshare`, sets up the systemd unit and prints the setup code you need next. Update rebuilds and redeploys without touching `data.json`. Uninstall offers to keep a copy of `data.json` before removing everything.

The web UI lives in a submodule ([wirezatUI](https://github.com/Wirezat/wirezatui)), so clone with it:

```sh
git clone --recursive https://github.com/Wirezat/fileshare
# already cloned without it:
git submodule update --init
```

Without the submodule `assets/web/ui` stays empty and every page renders unstyled. `go.sum` is not tracked, so run `go mod tidy` before the first build.

### 2. First-time setup

Open `http://localhost:<port>/setup` or `http://localhost:<port>/admin` in your browser — `/admin` redirects there while no password is set.

The page asks for a **setup code** on top of the username and password. It is generated on every start that finds no admin password and written to the server log:

```
[INFO] no admin password set — open /setup and enter this setup code: 3f1c…
```

Without it the endpoint answers 403. This keeps a fresh instance from being claimed by whoever reaches it first — only someone who can read the server's own log can create the account. After setup, `/setup` is permanently disabled and you are redirected to the admin panel.

---

## Admin UI

The admin interface is available at `/admin` and is split across three pages: `/admin` (shares), `/admin/logs` and `/admin/settings`.

### Shares

Create and manage all shares from the shares tab. Each share maps a public URL subpath to a file or folder on the server.

| Field | Description |
|---|---|
| Subpath | The URL path, e.g., `docs` → `http://host/docs`. Leave empty for a random value. |
| Path | Absolute path to the file or folder on the server. |
| Expires | Optional expiration date and time. |
| Allow uploads | Let visitors upload files into this share's directory. |
| ZIP download | Offer the folder as a single ZIP. On by default; switch it off for very large folders. |
| Office | `off`, `view` or `edit` — how office documents in this share open. Greyed out until a document server is configured under Settings. |
| Password | Optionally protect the share with a password. |

Shares can be edited, disabled, re-enabled, and deleted inline from the table. A disabled share remains in the list but is inaccessible until re-enabled.

### Logs

Live server log stream at `/admin/logs`, with DEBUG / INFO / WARN / ERROR filters and a full-text search over the visible lines. Request log lines expand to their full JSON on click. The viewer loads the server's recent buffer over REST and then follows along via SSE. Clearing the view does not affect the log file on disk.

### Settings

| Setting | Description |
|---|---|
| Change username | Updates the admin username. Requires the current password, and ends every other session. |
| Change password | Updates the admin password (stored as an Argon2id hash). Requires the current password, and ends every other session. |
| Office integration | Address of your document server and the shared secret it signs requests with. See [Office documents](#office-documents). |
| Delete expired shares | Permanently removes all expired shares from `data.json`. |

### Editing `data.json` by hand

Changes to `/opt/fileshare/data.json` take effect within two seconds, without a restart. Editing the admin username or password there ends every admin session; editing a share's password or path, or removing the share, ends its unlocks — the same as changing them in the admin UI. The server counts `"revision"` up on every save of its own (admin UI changes, upgrades of old password hashes); leave it as it is. If the file is not valid JSON, or carries a lower revision than the server's last save (for example an editor that kept an old copy open and saved it), the server keeps serving the last valid state, logs the reason once and shows it as a banner on every admin page. The next change in the admin UI still goes through (saving an unchanged value does not count): it moves the rejected file to `data.json.rejected-<time>` next to it and writes the server's state.

To restore an older backup while the server runs, set its `"revision"` to at least the value the log names, or restart the service. On a restart the server always takes the file on disk as it is.

---

## Share Behavior

### Accessing a share

- `http://host/<subpath>` — serves the file directly or shows a directory listing.
- Images, video and audio preview in place; PDFs open in the browser's own viewer. Text files (`.txt`, `.log`, `.json`, `.csv`, config and source files) open in an overlay, Markdown rendered; `?view=text` on a file URL returns that rendering as an HTML fragment for files up to 1 MB. Every card and table row has a download control, and `?dl=1` on any file URL always returns the raw file.
- Directories can be downloaded as a ZIP via the `?download=zip` query parameter, unless the share has ZIP switched off. Hidden entries (dot-files and dot-folders) stay out of the archive, as they do out of the listing. Tick entries in the listing and the ZIP button downloads only those; on the URL that is a repeated `f=<name>` parameter per entry, folders included recursively.
- A filter field above the listing narrows it by name; Escape clears it. The filter hides entries but keeps their selection.
- If the share has a password, visitors are shown a password gate before accessing the content.

### Password-protected shares

Entering the correct password sets a session cookie scoped to that subpath. The session is valid for 24 hours. Each share's password is stored as an Argon2id hash; bcrypt hashes written by older versions are still accepted and upgraded on the next successful unlock.

### Uploads

When a share has uploads enabled, visitors can drag and drop files onto the listing page. Uploads use a chunked protocol with crash-safe resume support.

### Office documents

fileshare can hand `.docx`, `.xlsx`, `.pptx` (and their `.doc`/`.xls`/`.ppt` and OpenDocument cousins) to an ONLYOFFICE-compatible document server — ONLYOFFICE Docs, EuroOffice or any fork that keeps the API — and embed the editor in its own page. Nothing is installed on the fileshare side; you point it at a server you already run.

**Setup.** Under `/admin/settings` enter the server's public address (the one browsers can reach) and the secret it signs requests with — the `JWT_SECRET` of the document server. Both must be set before the Office switch on a share does anything.

**Per share.** `view` opens documents read-only, `edit` lets visitors change and save them. `off` downloads them like any other file. A share with a password works the same way: visitors unlock it as usual, and the document server authenticates itself with its own signed token, so it needs no password.

**How saving works.** The document server fetches the file from fileshare, and when an editor closes with changes it posts a callback. fileshare checks the signature, only ever downloads the saved version from the configured server address, and swaps it in atomically. Anyone who can reach an `edit` share can overwrite its documents — that is the point of `edit`, so hand those links out accordingly. If a file is replaced by upload while someone has it open, the last save wins.

**Networking.** The document server must be able to reach fileshare's public address from inside its own network. Behind a NAT router that does not hairpin, a container typically cannot — for a Podman or Docker setup, add fileshare's hostname to the container's `extra_hosts` pointing at the host gateway, the same way you would for Nextcloud. `ALLOW_PRIVATE_IP_ADDRESS=true` on the document server is needed for that route.

### Link previews

Pasting a share link into a messenger (Telegram, Discord, WhatsApp, Slack, Signal, Threema, iMessage) shows a proper preview instead of a bare URL. A file share shows its name, type, size and expiry, with an inline image or video where the file is one; a folder share shows how many files and folders it holds and their total size; a password-protected share shows only that it is protected, nothing about its contents. This only changes what the crawler that fetches the link sees — visitors still get the normal page or file.

---

## Forgotten admin password

Set `"admin_password": ""` in `/opt/fileshare/data.json`. Within two seconds the server ends every admin session and prints a new setup code to its log; `/setup` then works as on a fresh install (see [First-time setup](#2-first-time-setup)). No restart is needed. Shares are not touched.
