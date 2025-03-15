#!/bin/bash

# Author: [Damian Webster]

# Ziel und Pfad des Symlinks definieren
LINK="/usr/local/bin/fileshare"

# Funktion zum Entfernen des Symlinks
remove_symlink(){
    if [ -L "$LINK" ]; then
        echo "Removing symlink $LINK"
        rm "$LINK"
    else
        echo "Symlink $LINK does not exist."
    fi
}

# Systemd-Dienst entfernen
remove_systemd_service(){
    systemctl stop fileshare.service || { echo "Error stopping fileshare service"; }
    systemctl disable fileshare.service || { echo "Error disabling fileshare service"; }
    systemctl daemon-reload || { echo "Error reloading systemd"; }
}

# SELinux-Rücksetzungsbefehl (falls notwendig)
restorecon -v /etc/systemd/system/fileshare.service

# Entferne Symlink
remove_symlink

# Entferne das gesamte Verzeichnis /opt/fileshare
echo "Removing /opt/fileshare directory and its contents"
rm -rf /opt/fileshare || { echo "Error removing /opt/fileshare"; exit 1; }

# Entferne den Systemd-Dienst
remove_systemd_service

# Entferne die Systemd-Service-Datei
if [ -f "/etc/systemd/system/fileshare.service" ]; then
    rm -f "/etc/systemd/system/fileshare.service"
    echo "Removed /etc/systemd/system/fileshare.service"
fi

echo "Uninstallation completed."