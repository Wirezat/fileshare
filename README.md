# FILESHARE
A simple go program that reads files from a json and hosts them under a link
***
## Installation:
To install fileshare, just use the installer by downloading the release folder and executing it [`sudo bash install`]
To uninstall fileshare, you can use the uninstaller with [`sudo bash uninstall`]

***
## Usage:
use the program as 
fileshare <functionality> <subdomain> <filepath> although not all parameters are needed all the time

functions:
 - list
 - del <subdomain>
   - deletes a given path from the list
 - add <subdomain> <path/to/file>
   - adds a new file under a given subdomain
 - addrandom <path/to/file>
   - adds a file under a random subdomain for more private data

For example "sharefile path/to/file example"  
will host the file under http://localhost:port/example

this link will just lead directly to the file so any device can easily download your files.
