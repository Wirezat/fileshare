#!/bin/bash

# Author: [Damian Webster]

make_symlink(){
    # Ziel und Pfad des Symlinks definieren
    TARGET="/opt/fileshare/fileshare-interface"
    LINK="/usr/local/bin/fileshare"
    # Überprüfen, ob der Symlink bereits existiert
    if [ -L "$LINK" ]; then
        echo "Symlink $LINK exists, removing it."
        rm "$LINK"
    fi

    # Symlink erstellen
    ln -s "$TARGET" "$LINK"

    # Überprüfen, ob der Symlink erfolgreich erstellt wurde
    if [ -L "$LINK" ]; then
        echo "Symlink successfully created: $LINK -> $TARGET"
    else
        echo "Failed to create symlink."
        exit 1
    fi
}

# Überprüfen, ob die notwendigen Dateien existieren
if [ ! -f "fileshare-backend" ] || [ ! -f "fileshare-interface" ] || [ ! -f "fileshare-run.sh" ] || [ ! -f "fileshare.service" ] || [ ! -f "data.json" ]; then
    echo "Error: One or more files are missing!"
    exit 1
fi

# Erstelle Verzeichnis und setze Berechtigungen
mkdir -p /opt/fileshare
chown -R $USER:$USER /opt/fileshare

# Dateien verschieben
cp fileshare-backend /opt/fileshare || { echo "Error moving fileshare-backend"; exit 1; }
cp fileshare-interface /opt/fileshare || { echo "Error moving fileshare-interface"; exit 1; }
cp data.json /opt/fileshare || { echo "Error moving data.json"; exit 1; }
cp fileshare-run.sh /opt/fileshare || { echo "Error moving fileshare-run.sh"; exit 1; }
cp fileshare.service /etc/systemd/system || { echo "Error moving fileshare.service"; exit 1; }
cp template.html /opt/fileshare/template.html || { echo "Error moving template.html"; exit 1; }

# Alle Dateien im Verzeichnis /opt/fileshare ausführbar machen
chmod +x /opt/fileshare/* || { echo "Error setting execute permissions on files"; exit 1; }

# magic to fix systemd SELinux stuff.
/sbin/restorecon -v /etc/systemd/system/fileshare.service
# Systemd neu laden und Dienst aktivieren
systemctl daemon-reload
systemctl enable --now fileshare.service

# Symlink erstellen
make_symlink
