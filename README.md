# fileshare
A simple go program that reads files from a json and hosts them under a link

This can be used as a systemd service. Data.json has to be in the same directory and contains the port and the files to be hosted.
The directory has now been set to "/opt/fileshare/" (you can change this in the json interface code)

"example": "/path/to/file"
will host the file under http://localhost:port/example

this link will just lead directly to the file so any device can easily download your files.

Note: This is a fun project and my first go project so it may be a bit rough. But it works and it will get better over time... I hope

# Available Commands

## LIST COMMAND
### `list`, `l`
Displays all shared files along with their assigned subpaths.

---

## ADD COMMAND
### `add -subpath=<subpath> -file=<file> [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]`
Creates a new share for the specified file under the given subpath.
- `subpath` = desired share name
- `file` = path to the file on the system
- `use-expiration` = max uses
- `time-expiration` = time limit

---

## ADD RANDOM COMMAND
### `addrandom`, `random`, `add_random`, `addr -file=<file> [-use-expiration=<num>] [-time-expiration=<xxh/d/w/m/y>]`
Creates a new share for the specified file with a randomly generated subpath.
- `file` = path to the file on the system
- `use-expiration` = max uses
- `time-expiration` = time limit

---

## DELETE COMMAND
### `delete`, `del`, `remove`, `rm -subpath=<subpath>`
Removes an existing share.
- `subpath` = existing share name

---

## EDIT COMMAND
### `edit -subpath=<old_subpath> -file=<new_subpath>`
Changes the subpath of an existing share.
- `old_subpath` = current share name
- `new_subpath` = new share name
