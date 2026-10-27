# Changelog

Versions follow [Semantic Versioning](https://semver.org) from 5.1.0 on:

- **Major** (6.0.0): something you have to act on when updating, like a setting that changes meaning, a removed feature or a new way to install.
- **Minor** (5.2.0): new features. Your data and setup keep working without changes.
- **Patch** (5.1.1): fixes only.

Every version reads the `data.json` of all earlier versions. Going back to an older version is not supported; restore a backup instead.

Release tags are `v<version>.<codename>`, e.g. `v5.1.0.exodus`. Earlier versions are described on the [releases page](https://github.com/Wirezat/fileshare/releases).

## Unreleased

### Added

- The Docker/Podman image also runs on arm64 (Raspberry Pi 4/5 and other 64-bit ARM boards).

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
