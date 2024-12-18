#!/bin/bash

# Hardcodierte Go-Datei (ohne Escape-Zeichen für Leerzeichen)
GO_FILE="/opt/fileshare/fileshare-interface"


# Überprüfen, ob die Go-Datei existiert
if [ ! -f "$GO_FILE" ]; then
    echo "Error: $GO_FILE does not exist."
    exit 1
fi

# Ausführen der Go-Datei mit den übergebenen Argumenten
sudo "$GO_FILE" "$@"
