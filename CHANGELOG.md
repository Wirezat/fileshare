# Changelog

Versions follow [Semantic Versioning](https://semver.org) from 5.1.0 on:

- **Major** (6.0.0): something you have to act on when updating, like a setting that changes meaning, a removed feature or a new way to install.
- **Minor** (5.2.0): new features. Your data and setup keep working without changes.
- **Patch** (5.1.1): fixes only.

Every version reads the `data.json` of all earlier versions. Going back to an older version is not supported; restore a backup instead. From the first version with a `schema` number on, an older one refuses to start on newer data.

Release tags are `v<version>.<codename>`, e.g. `v5.1.0.exodus`. Earlier versions are described on the [releases page](https://github.com/Wirezat/fileshare/releases).

## Unreleased

### Added

- The Docker/Podman image and the release tarball also run on arm64 (Raspberry Pi 4/5 and other 64-bit ARM boards).
- `data.json` carries a `schema` number. A version that finds a higher number refuses to start instead of dropping settings it does not know. Versions before this one do not check it, so going back from here to 5.1.x still loses settings.

- Settings → Upload notification: a message to an ntfy topic for every finished upload (server, topic and optional access token), with file name, size and the share it went into. Optional access token for protected topics. Off while the address is empty.

- `fileshare-backend version` prints the running version.

- Users can show dotfiles in a share with a toggle next to the view picker. They stay hidden by default and the choice is remembered.

- Every file opens in a lightbox and has an icon. Where possible it also has a card preview: images, the cover of mp3, flac and ogg files, the first lines of text, video and audio players, and Markdown, HTML, source code, PDF and Office documents in a matching view. Office files can be edited from the lightbox in shares with edit access.

- Photos get card previews from phones and cameras too: the limit counts pixels (16.8 MP) instead of edge length, bigger photos show their embedded Exif thumbnail, rotated photos stand upright, and HEIC and AVIF photos work as well (the lightbox shows HEIC as a JPEG of up to 2400 px). HEIC and AVIF photos use their embedded thumbnail for cards when it is large enough, and always when the photo is over the limit. JPEG, WebP and HEIC photos do the same with their Exif thumbnail.

- Shares with tens of thousands of entries open fast and scroll smoothly. The page carries the listing as compressed data and only the rows around the viewport are in the page. Previews start once a card stays in view. Filter, selection, ZIP download and the lightbox work on the whole listing. A share with 21,746 entries went from 35 MB to 320 KB and from several seconds to 1.6 s. The listing needs JavaScript.

- Text and source files are shown with syntax colors in the lightbox, in light and dark mode. The colors are computed in the browser, so large files do not load the server, and the lightbox reads up to 2 MiB of a text file.

- m4a, m4b and opus files play as audio, m4v as video, and SVG files show as images, also on cards. Audio and video files show their embedded cover (m4a, m4b, aac, mp4, mov, mkv, webm), videos as the picture before playback. Files without an extension are recognized by their content (images, videos, audio, PDF) and open and preview like the format they are.

- A toggle shows card previews in their own aspect ratio instead of the fixed crop. The cards have their size from the start, also in long folders.

- Camera raw files (dng, cr2, cr3, nef, nrw, arw, orf, rw2, raf, pef, srw) show their embedded preview on cards and in the lightbox, turned upright.

- Photoshop files (psd) show their embedded thumbnail on cards and in the lightbox.

- Office documents in shares with edit access are edited inside the lightbox, which widens to almost the whole window (the whole screen on phones). Closing it after changes says that they are being saved.

- Pressing Ctrl+F in a share focuses the filter field.

### Changed

- Release tarballs are named `fileshare-v<version>-linux-amd64.tar.gz` and `fileshare-v<version>-linux-arm64.tar.gz`. The installer stops with a clear message if the binary does not fit the machine.

### Fixed

- Links to files and folders with `#`, `?` or `%` in their name work. The same goes for the Office viewer and link previews.
- Long file names on cards scroll on hover.
- A locked upload toggle in the admin says why in its tooltip and looks disabled instead of answering a click with an error toast. Locked toggles dim as rounded pills.
- Browsers revalidate the shared UI files and admin scripts on every use, so an update shows up at once.
- Built with Go 1.27.4. Go 1.25 gets no more security fixes, and the older build called 14 known-vulnerable standard-library paths (`net/http`, `crypto/tls`, `crypto/x509`, `html/template`).
- Preview images: `golang.org/x/image` moves to v0.45.0, which closes a memory exhaustion when decoding crafted WebP files (GO-2026-6222).

## 5.1.0 (exodus)

### Breaking

- fileshare no longer runs as root. A v5.0 system install stops at Update: stop the service, move `/etc/systemd/system/fileshare.service` away and pick Install. `data.json` stays.
- Shares can no longer be named `admin`, `static`, `setup` or `healthz`. An existing share with one of these names stays hidden behind the route of the same name; rename it.

### Added

- Install for a single user without `sudo`, or as a system service with its own user.
- Docker/Podman image `ghcr.io/wirezat/fileshare`, with a health check and a ready-to-use `compose.yaml`.
- `SHA256SUMS` for the release tarball.
- `/healthz` and `fileshare-backend healthcheck`.
- Port, listen address, data and log folder via `FS_PORT`, `FS_BIND`, `FS_DATA_DIR`, `FS_LOG_DIR`.
- The admin UI greys out folders and upload options the service user cannot use, and explains why.
