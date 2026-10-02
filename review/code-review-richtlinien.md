# Cloud-Lauf: so wird diese Datei ausgeführt

Diese Datei ist die Aufgabe. Sie wird in einer Claude-Code-Cloud-Session mit „Führe `review/code-review-richtlinien.md` aus" gestartet. Für diesen Lauf gilt, und das hat Vorrang vor dem Rest:

- **Ausgabe nur unter `review/`.** `docs/` ist in diesem Repo gitignored und würde nie auf dem Branch landen. Tickets liegen deshalb in `review/issues/<name>/Issue.md` (Themen-Tickets und Detail-Tickets, siehe unten), Befunde und Index in `review/befunde/`. Kein GitHub-Issue anlegen, kein PR-Kommentar als Ticket.
- **Kein Code wird geändert.** Nur Dateien unter `review/` schreiben. Commit-Format `chore: <was neu ist> - <Grund>`, eine Zeile, klein geschrieben. Am Ende auf einen Branch `review/gesamt` pushen, keinen PR öffnen.
- **Es gibt kein `projekt.md`.** Angaben daraus gelten so: Ticket-Ordner `review/issues/<name>/`, Kategorien `feat|fix|chore|refactor`, Testbefehl `go test ./...`, Coverage `go test -cover ./...`. Scope: ganzes Repo **ohne** Submodul `assets/web/ui`, aber `assets/web/` (html, js, css, locales) gehört dazu.
- **Submodul:** `assets/web/ui` ist ein Submodul (wirezatui) mit SSH-URL, die in der Cloud nicht geht. Nur falls das UI gelesen werden muss: `git clone https://github.com/Wirezat/wirezatui.git /tmp/wirezatui` und den im Repo gepinnten Commit auschecken (`git ls-tree HEAD assets/web/ui`). Nie ins Repo einchecken.
- **Graphen:** Der Builder (`graphbuilder`) ist in der Cloud nicht installiert. In `review/graphs/` liegen bereits 18 validierte Spec-JSONs für die Workflows 1–15 (Ist plus Soll). Sie sind Vorlage und Stand. Fehlende Workflows (16 CLI Share-Verwaltung: list/add/edit/delete/enable/disable/prune, 17 CLI Admin-Zugang: setpassword/setusername, 18 Installer: install/update/uninstall/--remote) als Spec-JSON im selben Format ergänzen: gleiche Feldnamen wie in den vorhandenen Dateien, keine unbekannten Felder, jede Kante verweist auf vorhandene Knoten, genau ein `term`-Ende, Fehlerausgänge mit `error: true`, `datei:zeile` in `tech.where`. HTML wird später lokal erzeugt (`graphbuilder <spec>.json`), nicht in der Cloud.
- **Vorhandene Specs prüfen:** Stichprobe, ob `datei:zeile` noch zum Code passt (HEAD kann sich geändert haben). Abweichungen als Befund notieren.
- **Rückfragen:** Phase 7 (Ticketliste abstimmen) ist die einzige Stelle, an der auf den User gewartet wird. Davor entscheidet die Session selbst, nennt aber jede nicht belegte Annahme im Ticket als ❓. Wartet die Session dort, ist das so gewollt.
- **Zum Schluss:** Kurzer Bericht: Anzahl Themen-/Detail-Tickets, die drei wichtigsten Befunde, Pfad zu `review/befunde/Index.md`.

---

# Code-Review-Richtlinien: das Review erstellt Tickets (Entwurf)

Status: Entwurf zur Abstimmung. Eigener Skill, unabhängig von `designing-ticket-flows`. Dieser Skill **erstellt** Tickets, `designing-ticket-flows` **bearbeitet** sie später. Die Schnittstelle dazwischen ist die `Issue.md` im Ticket-Ordner. Zuerst angewandt auf `fileshare`.

## Überblick

Das Review analysiert eine Codebasis und liefert am Ende **fertig geschnittene Tickets**. Der Schwerpunkt ist Qualität: wo lässt sich der Code vereinfachen, welche Funktionen gehören (nicht) zusammen, wo ist ein Prozess suboptimal, wo muss er sich ändern, und wo würde eine kleine Änderung der Bedienung den Code deutlich einfacher machen.

Das Review ändert keinen Code und legt keinen Lösungsweg fest. Ein Ticket beschreibt Problem, Ziel und prüfbare Kriterien. Den Weg dahin (Soll-Graph, Plan, Umsetzung) erarbeitet später der Bearbeitungs-Skill zusammen mit dem User.

**Kernregeln**

1. **Das Ergebnis sind Tickets, kein Bericht.** Befunde sind ein Zwischenschritt.
2. **Qualität vor Checklistenvollständigkeit.** Wenige Tickets, die den Code spürbar einfacher machen, schlagen viele Kleinigkeiten.
3. **Belegen, nicht behaupten.** Jede Aussage hat `datei:zeile`. Was nur vermutet ist, steht als ❓ im Ticket.
4. **Nichts wird still entschieden.** Ungeklärtes wird dem User gefragt oder steht als offene Frage im Ticket.
5. **Das Konzept des Tools bleibt.** Die Bedienung darf sich ändern, wenn der Code dadurch spürbar einfacher wird. Ob, entscheidet der User.

**Konzeptgrenze** (Vorschlag aus dem README, der User bestätigt): selbst gehostet, Dateien und Ordner per Link teilen, keine Benutzerkonten und Instanzen, Verwaltung über Admin-UI und CLI. Eine Änderung, die diese Grenze verschiebt, ist keine UX-Änderung, sondern eine Konzeptänderung und kommt nur als Frage.

## Voraussetzungen

- `projekt.md` im Projektroot lesen. Fehlt sie oder fehlt eine Angabe, trägt der User ein: Ticket-Ordner, Branch-Schema, Commit-Kategorien, Testbefehle.
- Scope festlegen: ganzes Repo, nur `cmd/`, mit oder ohne Submodul wirezatUI.
- **Baseline** erfassen: `go build ./...`, `go vet ./...`, `go test -cover ./...`. Das Ergebnis ist Ausgangslage für die Tickets, kein Befund.

## Ablauf

**Phase 1: Inventur und Workflows**

1. Pakete, Dateien und Größen erfassen. Einstiegspunkte sammeln: HTTP-Routen, CLI-Befehle, Hintergrundprozesse, Installer.
2. Daraus die **Workflow-Liste** ableiten und mit dem User abgleichen (einzeln fragen).
3. Pro Workflow ein **Ist-Graph** (Skill `graph-builder`), eine Spec je Workflow. Knoten tragen `tech.where` mit `datei:zeile`, Fehlerausgänge `error: true`, Meldungen unter `tech.messages`.
4. **Vollständigkeitsprüfung:** Jede Route und jeder CLI-Befehl kommt in mindestens einem Graph vor, jeder Knoten zeigt auf echten Code. Lücken sind selbst ein Befund (undokumentierter Ablauf).

Die Ist-Graphen bleiben nach dem Review erhalten. Die Tickets verweisen auf sie, und die Ist-Analyse im Bearbeitungs-Skill kann an ihnen anknüpfen.

**Phase 2: Struktur und Kohäsion**

5. Pro Datei und Paket die Zuständigkeit in **einem** Satz formulieren. Braucht der Satz ein „und“, ist die Datei ein Kandidat.
6. Funktionen nach den Kriterien unten gruppieren: was gehört nicht zusammen und sollte getrennt werden, was gehört zusammen und liegt verstreut.

**Phase 3: Qualitätsanalyse (Schwerpunkt)**

7. Den Vereinfachungs-Katalog (unten) auf jeden Workflow und jedes Paket anwenden.
8. Jeder Vorschlag nennt, was entfällt, was sich verbindet und welche Tests das Verhalten absichern. Äquivalenz vorher und nachher ist ein **Soft-Check**: Abweichung melden, nie blockieren.

**Phase 4: Faktoren-Durchgang**

9. Die Faktoren-Tabelle abarbeiten. Befunde mit Schweregrad und Konfidenz erfassen.

**Phase 5: Prozess und Bedienung**

10. Pro Workflow ein **Soll-Graph** (Vereinfacht) als neue Version der Spec, wo der Prozess sich ändern soll. Alles Ungeklärte als ❓.
11. UX-Hebel (unten) getrennt auflisten, nie in den Code-Befunden verstecken.

**Phase 6: Ticket-Schnitt**

12. Befunde zu Tickets schneiden, nach den Regeln unten. Pro Ticket einen Namen, eine Kategorie und die zugehörigen Befund-IDs festlegen.

**Phase 7: Ticketliste abstimmen**

13. Dem User zuerst die **Liste** vorlegen: die Themen-Tickets, darunter ihre Detail-Tickets (Name, Kategorie, Schweregrad, Aufwand, ein Satz, Abhängigkeiten, Reihenfolge), noch ohne ausgeschriebene Tickets. Der User streicht, bündelt oder teilt. Hier dürfen mehrere Fragen pro Runde gestellt werden.

**Phase 8: Tickets schreiben**

14. Pro freigegebenem Detail-Ticket einen Ordner `review/issues/<name>/` mit `Issue.md` nach dem Format unten anlegen. Pro Themen-Ticket zusätzlich eine kurze `Issue.md` mit Ziel, Liste der Detail-Tickets und Reihenfolge. Tickets liegen nur im Ordner, es gibt keine GitHub-Issues.
15. `Index.md` im Review-Ordner mit der endgültigen Reihenfolge und den Abhängigkeiten schreiben.

## Ticket-Schnitt

- **Zwei Ebenen.** Ein **Themen-Ticket** (grob) bündelt ein Thema oder Paket, zum Beispiel „Upload vereinfachen“. Darunter hängen **Detail-Tickets** (fein). Ein Detail-Ticket hat **einen** Änderungsgrund, ist in einem Branch lieferbar und unabhängig testbar.
- Befunde mit derselben Ursache gehören in ein Ticket, Befunde mit derselben Datei aber verschiedener Ursache nicht.
- **Refactoring** (Verhalten bleibt) und **Verhaltensänderung** sind getrennte Tickets. Refactorings kommen nach den Tickets, die sie mit Tests absichern.
- Sicherheitsbefunde mit hohem Schweregrad bekommen je ein eigenes Ticket.
- Jeder **UX-Hebel** ist ein eigenes Ticket, das ohne ihn lieferbare Vereinfachungen nicht blockiert.
- Ein **Prozessticket** nennt Workflow und Knoten, die sich ändern, und verlinkt Ist- und Soll-Graph.
- Abhängigkeiten ausdrücklich nennen (blockiert durch, blockiert).
- Kategorie passend zum Commit-Format: `fix`, `feat`, `chore` oder `refactor`.

## Ticket-Format (`Issue.md`)

```
# <Titel: was sich für den Nutzer oder die Codebasis ändert>

Kategorie: fix | feat | chore | refactor
Schweregrad: kritisch | hoch | mittel | niedrig
Aufwand: S | M | L
Befunde: F-003, F-007
Abhängigkeiten: blockiert durch <ticket>, blockiert <ticket>

## Problem
Was heute ist, mit `datei:zeile` und Beleg. Betroffene Workflows mit Link auf den Ist-Graph und die Knoten-IDs.

## Ziel
Was danach gelten soll, je ein Satz fachlich, für den Nutzer und technisch.

## Nicht-Ziel
Was dieses Ticket bewusst nicht ändert.

## Richtung (Idee, kein Plan)
Ein bis drei Sätze, wie es gehen könnte, plus Alternativen. Den Weg legt die Bearbeitung fest.

## Akzeptanzkriterien
- jedes Kriterium ist mit einem Test oder einer Beobachtung prüfbar

## Risiko
Regression, Abwärtskompatibilität (Links, `data.json`, CLI-Aufrufe), Migration.

## Offene Fragen
❓ ...
```

**UX-Hebel-Tickets** enthalten zusätzlich: Änderung für den Nutzer in einem Satz, Code-Gewinn mit `datei:zeile` (was entfällt oder vereinfacht sich), Alternative ohne UX-Änderung und was sie kostet, Auswirkung auf den Bestand (Links, gespeicherte Daten, CLI-Aufrufe, Migrationsweg oder „keine“) und eine Empfehlung. Ein UX-Hebel ist immer ein ❓ und wird nie still in ein anderes Ticket eingebaut.

## Faktoren

| Faktor | Leitfrage |
|---|---|
| Korrektheit | Randfälle, Fehlerpfade, Zustandsübergänge (Ablauf und Limits eines Shares), Zeit und Zeitzonen |
| Sicherheit | Authentifizierung und Sessions, Pfad-Traversal, Upload, XSS und Header, CSRF, Rate-Limit, Secrets und Krypto, Setup-Code, Office-Callback |
| Fehlerbehandlung | geschluckte Fehler, Timeouts, Ressourcen-Lecks, kaputte oder riesige Eingaben |
| Nebenläufigkeit | Races, Locking, TTL-Stores, parallele Uploads und Konfigurationsänderungen |
| Daten und Konfiguration | atomares Schreiben, Validierung, Defaults, Migration bei Formatänderung |
| Schnittstellen | Statuscodes und Fehlerformate, Admin-API gegen CLI, Abwärtskompatibilität bestehender Links |
| Tests | kritische Pfade abgedeckt, Assertions aussagekräftig, keine Flakiness |
| Performance | Streaming statt Vollladen, ZIP, Thumbnails, Speicherspitzen |
| Frontend | XSS in der DOM-Erzeugung, Tastatur und Escape, Barrierefreiheit, DE/EN-Parität |
| Wartbarkeit | Dateigröße, Duplikation, Benennung, Kommentare |
| Build und Betrieb | Installer, systemd, Logging, Submodul-Pinning, Abhängigkeiten, Lizenzen |
| Doku gegen Verhalten | jede README-Aussage gegen den Code prüfen |

**Gewichtung für fileshare:** Der Server ist aus dem Internet erreichbar. Sicherheit und Korrektheit werden vor allem anderen geprüft, der Qualitätsfokus (Vereinfachung, Kohäsion, Prozess) bestimmt aber den Schwerpunkt der Tickets.

## Kohäsion: gehört es zusammen?

| Gehört zusammen, wenn | Gehört nicht zusammen, wenn |
|---|---|
| es sich aus demselben Grund ändert | die Datei Ebenen mischt: HTTP-Parsing, Fachlogik, Speicherung, Darstellung |
| es auf denselben Daten arbeitet | `utils`, `types` oder `helpers` als Sammelbecken dienen |
| es fast nur gemeinsam aufgerufen wird | eine Funktion Parameter braucht, die nur für ein Nebenziel da sind |
| es dieselbe Abstraktionsebene hat | zwei Aufrufer disjunkte Teile derselben Datei nutzen |
| es dieselbe Fehlerbehandlung und Invarianten teilt | Abhängigkeiten in die falsche Richtung zeigen (zum Beispiel `pkg` auf `cmd`) |
| | dieselbe Logik in Admin-Handler und CLI doppelt steht |

Startpunkte zum Prüfen, **noch nicht gelesen und kein Befund**: `cmd/cli/main.go` (995 Zeilen), `cmd/server/utils.go`, `pkg/shared/utils.go`, `cmd/server/types.go`, `handler.go` und `admin_handler.go`.

## Vereinfachungs-Katalog

- Was kann **ersatzlos** weg: toter Code, nie gesetzte Optionen, nie erreichte Zweige?
- Welche zwei Pfade tun fast dasselbe und lassen sich zu einem vereinen?
- Welche Abstraktion, Schicht oder Schnittstelle hat genau einen Nutzer?
- Welcher Zustand ist ableitbar und wird trotzdem gespeichert oder synchronisiert?
- Welche Sonderfälle verschwinden, wenn eine Regel einheitlich gilt?
- Wo wird dieselbe Prüfung mehrfach gemacht, oder zu spät?
- Welche Einstellung wird in der Praxis nie vom Default abweichend benutzt?
- Wo ersetzt die Standardbibliothek eigenen Code?

## Prozess: wo ist er suboptimal, wo muss er sich ändern?

Der Ist-Graph macht es sichtbar. Typische Zeichen:

- Schleifen und viele Fehlerausgänge, die nur durch frühere unsaubere Schritte entstehen
- Fehler werden erst am Ende des Ablaufs erkannt, obwohl die Eingabe früh prüfbar wäre
- derselbe Ablauf läuft über zwei Einstiegspunkte (Admin-UI und CLI) unterschiedlich
- Zustände werden von Hand nachgezogen statt aus einer Quelle abgeleitet
- Schritte, die nur existieren, um einen anderen Schritt zu korrigieren

## Befund-Format (Zwischenschritt)

Befunde stehen in `Befunde.md` im Review-Ordner und sind die Grundlage der Tickets.

| Feld | Inhalt |
|---|---|
| ID | `F-001` |
| Faktor | aus der Faktoren-Tabelle, oder `Vereinfachung`, `Kohäsion`, `Prozess`, `UX-Hebel` |
| Schweregrad | kritisch, hoch, mittel, niedrig, Hinweis |
| Konfidenz | **belegt** (Test, Aufruf, eindeutige Codestelle) oder **vermutet** (steht zusätzlich als ❓) |
| Ort | `datei:zeile`, bei Prozessen Workflow und Knoten |
| Beobachtung | was ist, ohne Wertung |
| Beleg | Reproduktion, Testausgabe oder Zitat der Stelle |
| Wirkung | Nutzen (weniger Code, weniger Risiko), Aufwand (S, M, L), Regressionsrisiko |

## Ordner

- Review-Ordner (Vorschlag `review/befunde/`): `Befunde.md`, `Index.md`, `graphs/` mit je Workflow einer `spec.json` und dem HTML
- Je Ticket `review/issues/<name>/Issue.md`, so wie der Bearbeitungs-Skill es als Start erwartet

**Priorisierung in `Index.md`:** erst, was Absicherung schafft (fehlende Tests für kritische Pfade), dann Sicherheit und Korrektheit, dann Vereinfachungen mit dem größten Code-Gewinn, zuletzt UX-Hebel.

## Fragen

- In Phase 1: **eine Frage pro Runde**. In Phase 7: mehrere pro Runde erlaubt.
- Neue Fragen sofort stellen, nicht bis zur Ticketliste sammeln.
- Ist die Antwort eine Wahl: als Auswahl mit Empfehlung (AskUserQuestion).

## Red Flags: STOP

| Gedanke | Stattdessen |
|---|---|
| „Das sieht unschön aus, also Befund“ | Schaden belegen: was geht kaputt, wird teurer oder unsicherer |
| „Ich bin sicher, das crasht“ | erst reproduzieren |
| „Viele kleine Funde füllen die Ticketliste“ | Kleinkram bündeln, Qualitätstickets nach vorn |
| „Das Ticket enthält gleich die komplette Lösung“ | Problem, Ziel, Kriterien. Den Weg legt die Bearbeitung fest |
| „Ein Ticket mit zehn Anliegen“ | ein Änderungsgrund pro Ticket |
| „Kriterium: Code ist sauberer“ | prüfbar machen, sonst streichen |
| „Mit anderer Bedienung wird der Code schöner“ | UX-Hebel-Ticket mit ❓, der User entscheidet |
| „Den Fix schreibe ich gleich mit“ | Das Review ändert keinen Code |
| „Ich schreibe alle Tickets aus, dann sieht der User weiter“ | erst die Liste abstimmen |
| „Der Graph ist halb fertig, das reicht“ | Vollständigkeitsprüfung gegen Routen und CLI-Befehle |
| „Die Datei ist groß, also schlecht“ | Zuständigkeit in einem Satz formulieren, dann entscheiden |

## Offen im Entwurf

- Konzeptgrenze bestätigen oder anpassen
- Tiefe der Workflow-Graphen: alle Workflows oder zuerst die sicherheitsrelevanten
- Soll das Submodul wirezatUI Teil des Reviews sein
- Name und Ort des Skills
