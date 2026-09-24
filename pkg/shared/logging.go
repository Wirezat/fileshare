package shared

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
)

const maxLogEntries = 500

// LogEntry is a line in the admin log view. Request is set only for HTTP
// access log entries — Message stays a short summary, the full structured
// request data lives in Request instead of being embedded as JSON text.
type LogEntry struct {
	Level   string          `json:"level"`
	Time    string          `json:"time"`
	Message string          `json:"message"`
	Request json.RawMessage `json:"request,omitempty"`
}

type LogStore struct {
	mu      sync.RWMutex
	entries []LogEntry
	subs    []chan LogEntry
}

var Logger = &LogStore{
	entries: make([]LogEntry, 0, maxLogEntries),
}

// parseLine parses a log line of the form: [LEVEL] [timestamp] message
func parseLine(line string) (LogEntry, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || line[0] != '[' {
		return LogEntry{}, false
	}
	i := strings.IndexByte(line, ']')
	if i < 0 {
		return LogEntry{}, false
	}
	level, rest := line[1:i], strings.TrimSpace(line[i+1:])
	if len(rest) < 2 || rest[0] != '[' {
		return LogEntry{}, false
	}
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return LogEntry{}, false
	}
	msg := ""
	if j+2 < len(rest) {
		msg = rest[j+2:]
	}
	return LogEntry{Level: level, Time: rest[1:j], Message: msg}, true
}

// Add appends entry to the ring buffer and pushes it to every subscriber.
func (l *LogStore) Add(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= maxLogEntries {
		copy(l.entries, l.entries[1:])
		l.entries = l.entries[:len(l.entries)-1]
	}
	l.entries = append(l.entries, entry)
	for _, ch := range l.subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// Recent returns up to n of the most recent log entries.
func (l *LogStore) Recent(n int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	total := len(l.entries)
	if n <= 0 || total == 0 {
		return nil
	}
	if n > total {
		n = total
	}
	out := make([]LogEntry, n)
	copy(out, l.entries[total-n:])
	return out
}

// Subscribe returns a channel that receives all future log entries.
func (l *LogStore) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	l.mu.Lock()
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (l *LogStore) Unsubscribe(ch chan LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, sub := range l.subs {
		if sub == ch {
			l.subs = append(l.subs[:i], l.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Load reads a log file and populates the ring buffer. Each line is either
// a GoLog text line ("[LEVEL] [time] message") or, for a persisted request
// log entry, a JSON-encoded LogEntry — told apart by its first character.
func (l *LogStore) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "{") {
			var entry LogEntry
			if json.Unmarshal([]byte(line), &entry) == nil {
				l.Add(entry)
			}
			continue
		}
		if entry, ok := parseLine(line); ok {
			l.Add(entry)
		}
	}
	return s.Err()
}
