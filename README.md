# fileshare
A simple go program that reads files from a json and hosts them under a link

This can be used as a systemd service. Data.json has to be in the same directory and contains the port and the files to be hosted.
The directory has now been set to "/opt/fileshare/" (you can change this in the json interface code)

"example": "/path/to/file"
will host the file under http://localhost:port/example

this link will just lead directly to the file so any device can easily download your files.

Note: This is a fun project and my first go project so it may be a bit rough. But it works and it will get better over time... I hope
***
## Update 1.1.0

The Json interface is now added, use it via
go run fileshare.go <functionality> <subdomain> <filepath> although not all parameters are needed all the time

functions:
 - list
 - del <subdomain>
 - add <subdomain> <path/to/file>
 - addrandom <path/to/file>
   - add random adds a random subdomain for more private data
