# fileshare

A self-hosted file sharing server written in Go. Share files and folders from your server by link, with optional passwords, uploads and expiry, all managed in a web admin UI.

I built it because messengers kept rejecting large files that were already on my NAS. Sending a direct link was simpler, and the existing tools were far more than I needed.

## Features

- **Web admin UI** to create, edit, disable and delete shares
- **Passwords** per share
- **Uploads** into a share: chunked and resumable
- **Expiry** date per share
- **Directory listing** with name filter, previews for media, PDF, text and Markdown, and ZIP download of a folder or a selection
- **Office documents** viewed or edited through an ONLYOFFICE-compatible document server
- **Link previews** in messengers
- **Download limit** shared fairly between visitors
- **Live log viewer**, dark mode

## Getting Started

### 1. Install

```sh
git clone --recursive https://github.com/Wirezat/fileshare
sudo bash scripts/fileshare-installer.sh               # menu: install / update / uninstall
bash scripts/fileshare-installer.sh --remote user@host # same, on another machine over ssh
```

Install builds the server, deploys it to `/opt/fileshare` and sets up a systemd unit. Update redeploys without touching `data.json`. Uninstall offers to keep a copy of `data.json`.

The web UI is the [wirezatUI](https://github.com/Wirezat/wirezatui) submodule. If you cloned without `--recursive`, run `git submodule update --init`, or every page renders unstyled.

### 2. First-time setup

Open `/admin`. It redirects to `/setup`, which asks for a username, a password and a **setup code**. The server writes the code to its log on every start that finds no admin password:

```
[INFO] no admin password set — open /setup and enter this setup code: 3f1c…
```

Only someone who can read the log can create the account. After setup, `/setup` is disabled.

**Forgotten password:** set `"admin_password": ""` in `/opt/fileshare/data.json`. Within two seconds every admin session ends and a new setup code appears in the log. Shares are not touched.

## Admin UI

### Shares

Each share maps a URL path to a file or folder on the server.

| Field | Description |
|---|---|
| Subpath | URL path, e.g. `docs` → `http://host/docs`. Empty for a random one. |
| Path | Absolute path on the server. |
| Expires | Optional date and time. |
| Allow uploads | Visitors may upload into the folder. |
| ZIP download | Offer the folder as ZIP. On by default. |
| Office | `off`, `view` or `edit`. Available once a document server is set up. |
| Password | Optional. |

### Settings

| Setting | Description |
|---|---|
| Username, password | Need the current password and end every other admin session. |
| Office integration | Document server address and its `JWT_SECRET`. |
| Bandwidth | Download limit on/off and a rate in Mbit/s. Applies to everything visitors download, including pages and previews. It does not apply to admin pages, static assets or uploads. Changes reach running downloads at once. Visitors downloading at the same time split the rate evenly. |
| Delete expired shares | Removes them from `data.json`. |

The log page streams the server log live, with level filters and search.

### Editing `data.json` by hand

Changes take effect within two seconds, except `port`, which needs a restart. Leave `"revision"` as it is; the server counts it up on every save.

If the file is invalid, or older than the server's last save, the server keeps the last valid state and shows a banner in the admin UI. The next change in the admin UI moves the rejected file to `data.json.rejected-<time>`. To restore a backup while the server runs, set its `"revision"` to at least the value named in the log, or restart.

## Sharing

- `http://host/<subpath>` serves the file or a folder listing.
- `?dl=1` always returns the raw file.
- `?download=zip` returns the folder as ZIP. Selected entries add `f=<name>` per entry.
- Dot-files and dot-folders are listed, zipped and served like any other entry. Keep things like `.env` or `.git` out of shared folders.
- A correct password unlocks the share for 24 hours in that browser.

### Uploads

Visitors drag files onto the listing. Uploads are sent in chunks and written straight into a hidden `.fileshare-upload-….part` file in the share's folder, which is renamed once complete. Choosing the same file again resumes an interrupted upload, also after a server restart.

Unfinished uploads are removed after the chunk inactivity timeout. This happens at the next server start or the next upload. A password-protected share accepts uploads only after it is unlocked. One chunk may not exceed `maxPostSize`.

`MAX_UPLOAD_RATE` at the top of `web/js/share.js` caps the upload rate per browser tab in bytes per second. `0` means no cap.

### Office documents

fileshare embeds the editor of an ONLYOFFICE-compatible document server (ONLYOFFICE Docs, EuroOffice and similar). Under Settings, enter the server's public address and its `JWT_SECRET`, then choose `view` or `edit` per share.

Anyone who can open an `edit` share can overwrite its documents. Password-protected shares work as usual.

The document server must reach fileshare's public address. Behind a router without NAT hairpinning, give its container an `extra_hosts` entry for fileshare's hostname pointing at the host gateway, and set `ALLOW_PRIVATE_IP_ADDRESS=true`.

### Behind a proxy

Rate limits and the download limit tell visitors apart by IP address, IPv6 by its /64 network. Behind a reverse proxy, fileshare uses the rightmost public address in `X-Forwarded-For`. The proxy must pass the real client address on. If it does not, all visitors count as one.
