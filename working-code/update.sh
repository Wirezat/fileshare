#!/bin/bash
# Ins richtige Verzeichnis wechseln
cd "$(dirname "$0")"

# Go-Code kompilieren
GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build -o fileshare-backend ./internal/fileshare_internal.go
GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build -o fileshare-interface ./json_interface/fileshare.go
echo "done compile"

# Alte Dateien entfernen
sudo rm /opt/fileshare/fileshare-backend
sudo rm /opt/fileshare/fileshare-interface
sudo rm /opt/fileshare/template.html
echo "done rm"

# Neue Dateien verschieben
sudo mv fileshare-backend /opt/fileshare/fileshare-backend
sudo mv fileshare-interface /opt/fileshare/fileshare-interface
sudo cp internal/template.html /opt/fileshare/template.html
echo "done mv"

# SELinux-Kontext wiederherstellen
sudo restorecon -v /opt/fileshare/fileshare-backend
sudo restorecon -v /opt/fileshare/fileshare-interface
echo "done sel"

# Dienst neu starten
sudo systemctl restart fileshare.service
echo "done restart"
