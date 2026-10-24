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
- **Statistics** on downloads, uploads and unique visitors, all time and the last 30 days, kept in `stats.json`
- **Live log viewer**, dark mode

## Getting Started

### 1. Install

```sh
git clone --recursive https://github.com/Wirezat/fileshare && cd fileshare
sudo bash scripts/fileshare-installer.sh               # as a system service
bash scripts/fileshare-installer.sh                    # without sudo, for your user only
```

The installer asks for port, folders and whether a reverse proxy sits in front, each with a default. Flags skip the questions, e.g. `--install --port 8080 --proxy --yes`. It builds from source if Go is installed, otherwise it uses the release binary. The same script updates and uninstalls.

| | with `sudo` | without `sudo` |
|---|---|---|
| Runs as | system user `fileshare`, or one you name | you |
| Can read and write | reads everything except `/etc/shadow`, `/etc/gshadow`, `/root/.ssh`; writes everything except `/etc`, `/usr`, `/boot` | what you may |
| Installed to | `/opt/fileshare` | `~/.local/share/fileshare` |

fileshare never runs as root. Ports below 1024 are refused: put a reverse proxy in front, or open the port in your firewall.

**Before you share anything:**

- Symlinks are always followed. Whoever can write into a shared folder can reach any file fileshare can read or write.
- With `sudo`, the service user can read and write every file through fileshare. Don't make your login user the service user.

Port, listen address, data and log folder can also be set with `FS_PORT`, `FS_BIND`, `FS_DATA_DIR` and `FS_LOG_DIR`. Everything else lives in the admin UI.

The web UI is the [wirezatUI](https://github.com/Wirezat/wirezatui) submodule. If you cloned without `--recursive`, run `git submodule update --init`, or every page renders unstyled.

### 2. First-time setup

Open `/admin` and create the admin account. It asks for a **setup code**, which the server writes to its log on start:

```
[INFO] no admin password set — open /setup and enter this setup code: 3f1c…
```

**Forgotten password:** set `"admin_password": ""` in `data.json` in the data folder. A new setup code appears in the log within two seconds; shares are kept.

## Good to know

- **Dot-files are shared too.** Keep `.env`, `.git` and the like out of shared folders.
- **Reverse proxy:** it must pass the client address in `X-Forwarded-For`, or all visitors share one download limit.
- **Office documents:** under Settings, enter an ONLYOFFICE-compatible document server and its `JWT_SECRET`, then pick `view` or `edit` per share. Anyone who can open an `edit` share can overwrite its documents. The document server must reach fileshare's public address.
- **`data.json`** can be edited by hand; changes apply within two seconds. An invalid file is ignored and the admin UI shows a banner.

## Docker

```sh
docker compose up -d --build    # or: podman compose up -d --build
```

- fileshare runs as whoever starts it and has exactly their rights: rootful as root, rootless as you. Rootful, uncomment `user:` with a host user's UID and GID to run as that user; create `./data` and `./share` as that user first.
- `./share` appears as `/share`. Mount other folders at the same path as on the host, e.g. `/srv/share:/srv/share:Z`.
- SELinux: for host folders other services use too, add `security_opt: [label=disable]` instead of relabelling them with `:Z`.
- The setup code is in `docker compose logs`.
