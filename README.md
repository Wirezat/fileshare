# fileshare

A self-hosted file sharing server written in Go. Share files and folders from your server by link, with optional passwords, uploads and expiry, all managed in a web admin UI.

## Features

- **Web admin UI** to create, edit, disable and delete shares
- **Passwords** and **expiry** per share
- **Uploads** into a share: chunked and resumable
- **Directory listing** with previews for media, PDF, text and Markdown, and ZIP download
- **Office documents** viewed or edited through an ONLYOFFICE-compatible document server
- **Link previews** in messengers, a fair **download limit** per visitor, **statistics**, live log viewer, dark mode

## Install

### Docker / Podman

Save this as `compose.yaml`:

```yaml
services:
  fileshare:
    image: ghcr.io/wirezat/fileshare:latest
    # user: "1000:1000"
    restart: unless-stopped
    ports:
      - "27182:27182"
    volumes:
      - ./data:/data:Z
      - ./share:/share:Z
```

```sh
docker compose up -d    # or: podman compose up -d
```

fileshare runs with the rights of whoever starts it. `./share` appears as `/share`; mount other folders at the same path as on the host, e.g. `/srv/share:/srv/share:Z`.

### Linux service

Download the tarball for your machine (`-amd64` for most servers, `-arm64` for Raspberry Pi and other 64-bit ARM boards) and `SHA256SUMS` from the [latest release](https://github.com/Wirezat/fileshare/releases/latest), then:

```sh
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf fileshare-v*-linux-*.tar.gz && cd fileshare-v*-linux-*/
sudo bash scripts/fileshare-installer.sh    # system service with its own user
bash scripts/fileshare-installer.sh         # or: just for your user, without sudo
```

The installer asks for port and folders, and also updates and uninstalls. fileshare never runs as root; ports below 1024 need a reverse proxy in front.

## First login

Open `/admin`. It asks for a **setup code**, which the installer prints and the server writes to its log (`docker compose logs`).

Forgot the password? Set `"admin_password": ""` in `data.json`; a new setup code appears in the log within two seconds.

## Before you share anything

- **Symlinks are followed** and **dot-files are shared.** Keep `.env`, `.git` and the like out of shared folders, and don't let untrusted people write into them.
- **Installed with `sudo`**, the service user can read and write nearly every file on the system through fileshare. Don't make your login user the service user.
- **Behind a reverse proxy**, pass the client address in `X-Forwarded-For`, or all visitors share one download limit.
- **`edit` shares** let anyone with the link overwrite the documents in them.

## Updates and backups

- **Back up the data folder** (`./data`, `/opt/fileshare` with `sudo`, `~/.local/share/fileshare` without). `data.json` holds all settings and shares, `stats.json` the statistics. It contains password hashes and the document server secret, so keep the backup private.
- **Update:** `docker compose pull && docker compose up -d`, or run the installer of the new release and pick Update.
- **Newer versions read the data of older ones. Going back does not work cleanly**: an older version drops settings it does not know. Back up before updating and restore the backup to go back.
- Changes, especially breaking ones, are in the [CHANGELOG](CHANGELOG.md). To report a security issue, see [SECURITY.md](SECURITY.md).

## Configuration

Everything is set in the admin UI. Port, listen address, data and log folder can also be set with `FS_PORT`, `FS_BIND`, `FS_DATA_DIR` and `FS_LOG_DIR`. `data.json` can be edited by hand; changes apply within two seconds.
