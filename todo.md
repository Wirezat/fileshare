# TODO

## Empfängerseite

- [x] **Filter in der Listing-Seite** — Eingabefeld, das Karten/Zeilen clientseitig nach Name ausblendet.
- [x] **Auswahl-ZIP** — mehrere Dateien/Ordner anhaken, nur die als ZIP laden. ZIP-Streamer bekommt eine Pfadliste als Parameter.
- [ ] **Text-/Code-Preview** — `.txt`, `.md`, `.json`, `.log`, Quellcode inline anzeigen statt Download; Markdown gerendert.
- [ ] **Open-Graph-Tags** auf der Share-Seite — Messenger-Vorschau mit Dateiname, Größe, ggf. Thumbnail.

## Admin

- [ ] **Share-Statistik** — Besuche, letzter Zugriff, Downloads pro Datei; Zeitstempel zur bestehenden Visit-Zählung, Anzeige im Admin.
- [ ] **Webhook-Benachrichtigung** — POST an ntfy/Gotify/Discord bei Upload und Ablauf, konfigurierbar unter Settings.
- [ ] **Share duplizieren** im Admin.
- [ ] **QR-Code** für den Share-Link im Admin.
- [ ] **Config-Reload per SIGHUP** — CLI-Änderungen ohne Restart übernehmen.

## Sicherheit / Robustheit

- [ ] **Rate-Limit** auf Password-Gate und Admin-Login, pro IP.
- [ ] **Bandbreiten-Limit pro Share**.
