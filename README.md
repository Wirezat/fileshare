# fileshare
A simple go program that reads files from a json and hosts them under a link

This can be used as a systemd service. Data.json has to bi in the same directory and contains the port and the files to be hosted.

"example": "/path/to/file"
will host the file under http://localhost:port/example

this link will just lead directly to the file so any device can easily download your files.

Note: This is a fun project and my first go project so it may be a bit rough. But it works and it will get better over time... I hope
